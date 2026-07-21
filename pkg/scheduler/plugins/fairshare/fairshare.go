/*
Copyright 2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package fairshare implements a Volcano scheduler plugin that provides
// per-namespace fair share scheduling within target queues using decayed
// cumulative usage tracking. It prevents any single namespace from
// monopolizing resources when multiple namespaces have pending work, and
// remembers past usage across scheduling cycles so that heavy consumers are
// deprioritized even after their jobs finish.
//
// Tenant identity is the job's namespace — namespace is used directly rather
// than a separate "user" concept, since not every cluster maps users to
// namespaces one-to-one (e.g. a namespace may itself represent a team or
// project shared by several users).
// The tracked resource type is configurable per queue (e.g., nvidia.com/gpu for
// GPU queues, cpu for CPU queues).
//
// Historical usage decays exponentially with a configurable half-life (default
// 4 hours). This means a namespace that consumed 10 GPU-hours will see its
// usage penalty halve every 4 hours, naturally converging back to equal
// priority.
package fairshare

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// Package-level persistent state. Volcano calls New() every scheduling cycle,
// so state stored on the plugin instance is lost between cycles. These globals
// survive across the scheduler process lifetime.
var (
	globalMu        sync.Mutex
	globalUsage     = make(map[string]map[string]float64) // [queue][namespace] → resource-seconds
	globalLastCycle time.Time
)

const (
	// PluginName is the name used to register this plugin with the framework.
	PluginName = "fairshare"

	defaultResourceKey      = "nvidia.com/gpu"
	defaultUnknownNamespace = "_unknown"
	defaultHalfLifeMinutes  = 240 // 4 hours
	usageEpsilon            = 1.0 // 1 resource-second: treat as equal
	usageCleanupThreshold   = 0.01
)

// queueState holds per-queue fair share tracking for one scheduling cycle.
type queueState struct {
	resourceKey      v1.ResourceName
	totalResource    float64
	namespaceRunning map[string]float64
	namespaceDemand  map[string]float64
	fairShares       map[string]float64
}

type fairSharePlugin struct {
	pluginArguments framework.Arguments

	defaultResource   string
	enableEnqueueGate bool
	queueResourceKeys map[string]string
	targetQueueNames  map[string]struct{}
	// targetAllQueues is true when fairshare.targetQueues is unset, meaning
	// the plugin applies to every queue instead of a fixed allowlist.
	targetAllQueues bool
	halfLife        time.Duration
	persistCfg      persistConfig

	queues map[string]*queueState

	// sessionUsage is a per-session snapshot of globalUsage taken under lock.
	// All reads during the scheduling cycle use this snapshot to avoid races.
	sessionUsage map[string]map[string]float64
}

// New creates a new fairSharePlugin instance with the given plugin arguments.
//
// Supported arguments:
//
//	fairshare.targetQueues          - comma-separated queue names (default: all queues)
//	fairshare.resourceKey           - default resource to track (default: "nvidia.com/gpu")
//	fairshare.resourceKey.<queue>   - per-queue resource override (e.g., "cpu" for a CPU queue)
//	fairshare.enableEnqueueGate     - "true" to enable enqueue gating (default: "false", ordering only)
//	fairshare.halfLifeMinutes       - half-life for usage decay in minutes (default: 240 = 4 hours)
//	fairshare.persistState          - "true" to persist usage to a ConfigMap across restarts (default: "false")
//	fairshare.stateNamespace        - namespace for the state ConfigMap (default: "volcano-system")
//	fairshare.stateConfigMap        - name of the state ConfigMap (default: "fairshare-usage-state")
//	fairshare.flushIntervalSeconds  - how often to flush state in seconds (default: 30)
func New(arguments framework.Arguments) framework.Plugin {
	fsp := &fairSharePlugin{
		pluginArguments:   arguments,
		defaultResource:   defaultResourceKey,
		queueResourceKeys: make(map[string]string),
		targetQueueNames:  make(map[string]struct{}),
	}

	var rk string
	arguments.GetString(&rk, "fairshare.resourceKey")
	if rk != "" {
		fsp.defaultResource = rk
	}

	var queuesArg string
	arguments.GetString(&queuesArg, "fairshare.targetQueues")
	if queuesArg != "" {
		for _, q := range strings.Split(queuesArg, ",") {
			q = strings.TrimSpace(q)
			if q != "" {
				fsp.targetQueueNames[q] = struct{}{}
			}
		}
	} else {
		// No allowlist configured: apply fair share to every queue rather
		// than silently doing nothing.
		fsp.targetAllQueues = true
	}

	for queueName := range fsp.targetQueueNames {
		var queueRK string
		arguments.GetString(&queueRK, "fairshare.resourceKey."+queueName)
		if queueRK != "" {
			fsp.queueResourceKeys[queueName] = queueRK
		}
	}

	var gateStr string
	arguments.GetString(&gateStr, "fairshare.enableEnqueueGate")
	fsp.enableEnqueueGate = strings.EqualFold(strings.TrimSpace(gateStr), "true")

	fsp.halfLife = defaultHalfLifeMinutes * time.Minute
	var halfLifeStr string
	arguments.GetString(&halfLifeStr, "fairshare.halfLifeMinutes")
	if halfLifeStr != "" {
		if mins, err := strconv.Atoi(strings.TrimSpace(halfLifeStr)); err == nil && mins > 0 {
			fsp.halfLife = time.Duration(mins) * time.Minute
		}
	}

	fsp.persistCfg = persistConfig{
		namespace:     defaultStateNamespace,
		configMapName: defaultConfigMapName,
		flushInterval: defaultFlushInterval,
	}
	var persistStr string
	arguments.GetString(&persistStr, "fairshare.persistState")
	fsp.persistCfg.enabled = strings.EqualFold(strings.TrimSpace(persistStr), "true")

	var stateNS string
	arguments.GetString(&stateNS, "fairshare.stateNamespace")
	if stateNS != "" {
		fsp.persistCfg.namespace = strings.TrimSpace(stateNS)
	}

	var stateCM string
	arguments.GetString(&stateCM, "fairshare.stateConfigMap")
	if stateCM != "" {
		fsp.persistCfg.configMapName = strings.TrimSpace(stateCM)
	}

	var flushStr string
	arguments.GetString(&flushStr, "fairshare.flushIntervalSeconds")
	if flushStr != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(flushStr)); err == nil && secs > 0 {
			fsp.persistCfg.flushInterval = time.Duration(secs) * time.Second
		}
	}

	queuesLog := interface{}("all")
	if !fsp.targetAllQueues {
		queuesLog = fsp.targetQueueNames
	}
	klog.V(2).Infof("fairshare: plugin created — queues=%v resource=%s halfLife=%s enqueueGate=%v persist=%v",
		queuesLog, fsp.defaultResource, fsp.halfLife, fsp.enableEnqueueGate, fsp.persistCfg.enabled)

	return fsp
}

func (fsp *fairSharePlugin) Name() string {
	return PluginName
}

// OnSessionOpen is called at the beginning of each scheduling cycle. It:
//  1. Decays historical usage and accumulates running usage for the elapsed period
//  2. Scans all jobs in target queues, computes per-namespace resource demand and running counts
//  3. Runs the max-min fairness algorithm per queue
//  4. Registers JobOrderFn (usage-based ordering), optional JobEnqueueableFn, and EventHandler
func (fsp *fairSharePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("fairshare: OnSessionOpen enter")

	fsp.queues = make(map[string]*queueState)

	if fsp.targetAllQueues {
		for _, queueInfo := range ssn.Queues {
			fsp.initQueueState(ssn, queueInfo.Name)
		}
	} else {
		for queueName := range fsp.targetQueueNames {
			fsp.initQueueState(ssn, queueName)
		}
	}

	// On the first cycle, load persisted state and start the flush goroutine.
	// Must happen before acquiring globalMu (loadState takes the lock internally).
	initPersistence(ssn.KubeClient(), fsp.persistCfg)

	// Hold globalMu for the entire decay + accumulation phase, then snapshot.
	globalMu.Lock()
	now := time.Now()
	var elapsed time.Duration
	if !globalLastCycle.IsZero() {
		elapsed = now.Sub(globalLastCycle)
		decayAllUsage(elapsed, fsp.halfLife)
		klog.V(3).Infof("fairshare: decay applied — elapsed=%s factor=%.6f halfLife=%s",
			elapsed.Round(time.Millisecond), DecayFactor(elapsed, fsp.halfLife), fsp.halfLife)
	}
	globalLastCycle = now

	// Accumulate running usage under the lock.
	for _, job := range ssn.Jobs {
		queueName, targeted := fsp.getQueueName(ssn, job)
		if !targeted {
			continue
		}

		qs := fsp.queues[queueName]
		namespace := fsp.getNamespaceFromJob(job)

		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, task := range tasks {
					res := taskResource(task, qs.resourceKey)
					qs.namespaceRunning[namespace] += res

					if elapsed > 0 {
						ensureGlobalQueueUsage(queueName)[namespace] += res * elapsed.Seconds()
					}
				}
			}
		}

		if pendingTasks, ok := job.TaskStatusIndex[api.Pending]; ok {
			for _, task := range pendingTasks {
				qs.namespaceDemand[namespace] += taskResource(task, qs.resourceKey)
			}
		}
	}

	// Snapshot globalUsage so callbacks can read without holding the lock.
	fsp.sessionUsage = snapshotUsage()
	globalMu.Unlock()

	for queueName, qs := range fsp.queues {
		totalDemand := make(map[string]float64)
		for namespace := range qs.namespaceRunning {
			totalDemand[namespace] = qs.namespaceRunning[namespace] + qs.namespaceDemand[namespace]
		}
		for namespace := range qs.namespaceDemand {
			if _, ok := totalDemand[namespace]; !ok {
				totalDemand[namespace] = qs.namespaceDemand[namespace]
			}
		}

		qs.fairShares = CalculateFairShares(totalDemand, qs.totalResource)

		usage := fsp.sessionUsage[queueName]
		klog.V(2).Infof("fairshare: queue=%s namespaces=%d totalResource=%.0f running=%v demand=%v",
			queueName, len(totalDemand), qs.totalResource, qs.namespaceRunning, qs.namespaceDemand)
		klog.V(3).Infof("fairshare: queue=%s shares=%v usage=%v halfLife=%s",
			queueName, qs.fairShares, formatUsage(usage), fsp.halfLife)
	}

	ssn.AddJobOrderFn(fsp.Name(), func(l interface{}, r interface{}) int {
		lJob := l.(*api.JobInfo)
		rJob := r.(*api.JobInfo)

		lQueue, lTarget := fsp.getQueueName(ssn, lJob)
		rQueue, rTarget := fsp.getQueueName(ssn, rJob)

		if !lTarget && !rTarget {
			return 0
		}
		if !lTarget {
			return -1
		}
		if !rTarget {
			return 1
		}
		if lQueue != rQueue {
			return 0
		}

		qs := fsp.queues[lQueue]
		lNamespace := fsp.getNamespaceFromJob(lJob)
		rNamespace := fsp.getNamespaceFromJob(rJob)

		queueUsage := fsp.sessionUsage[lQueue]
		lUsage := queueUsage[lNamespace]
		rUsage := queueUsage[rNamespace]

		klog.V(5).Infof("fairshare: JobOrderFn: <%s/%s> namespace=%s usage=%.1f running=%.0f, <%s/%s> namespace=%s usage=%.1f running=%.0f",
			lJob.Namespace, lJob.Name, lNamespace, lUsage, qs.namespaceRunning[lNamespace],
			rJob.Namespace, rJob.Name, rNamespace, rUsage, qs.namespaceRunning[rNamespace])

		if lUsage < rUsage-usageEpsilon {
			klog.V(3).Infof("fairshare: JobOrderFn: %s/%s WINS over %s/%s (usage %.1f < %.1f)",
				lJob.Namespace, lJob.Name, rJob.Namespace, rJob.Name, lUsage, rUsage)
			return -1
		}
		if lUsage > rUsage+usageEpsilon {
			klog.V(3).Infof("fairshare: JobOrderFn: %s/%s WINS over %s/%s (usage %.1f < %.1f)",
				rJob.Namespace, rJob.Name, lJob.Namespace, lJob.Name, rUsage, lUsage)
			return 1
		}

		lRunning := qs.namespaceRunning[lNamespace]
		rRunning := qs.namespaceRunning[rNamespace]
		if lRunning < rRunning {
			return -1
		}
		if lRunning > rRunning {
			return 1
		}

		return 0
	})

	if fsp.enableEnqueueGate {
		ssn.AddJobEnqueueableFn(fsp.Name(), func(obj interface{}) int {
			job := obj.(*api.JobInfo)

			queueName, targeted := fsp.getQueueName(ssn, job)
			if !targeted {
				return util.Abstain
			}

			qs := fsp.queues[queueName]
			namespace := fsp.getNamespaceFromJob(job)
			share, ok := qs.fairShares[namespace]
			if !ok {
				return util.Abstain
			}

			running := qs.namespaceRunning[namespace]
			jobRes := jobTotalResource(job, qs.resourceKey)

			klog.V(5).Infof("fairshare: JobEnqueueableFn: job=<%s/%s> namespace=%s running=%.0f jobRes=%.0f share=%.0f",
				job.Namespace, job.Name, namespace, running, jobRes, share)

			if running >= share && jobRes > 0 {
				klog.V(3).Infof("fairshare: REJECT enqueue for <%s/%s>: namespace %s at %.0f (share=%.0f)",
					job.Namespace, job.Name, namespace, running, share)
				return util.Reject
			}

			return util.Abstain
		})
	}

	ssn.AddEventHandler(&framework.EventHandler{
		// AllocateFunc/DeallocateFunc only update qs.namespaceRunning, the
		// session-local live count used by JobOrderFn/JobEnqueueableFn for
		// the remainder of *this* cycle. They deliberately do not touch
		// globalUsage (the persisted, decayed historical total): globalUsage
		// grows by time-integrating running resource (res * elapsed.Seconds())
		// once per cycle in OnSessionOpen, using the elapsed wall-clock time
		// since the previous cycle. A mid-cycle allocation has no elapsed
		// time associated with it yet — that only becomes known at the start
		// of the *next* cycle, when this task shows up as already-allocated
		// in job.TaskStatusIndex and its running time since the last cycle
		// boundary gets integrated in. So a newly allocated task is missing
		// from globalUsage for at most one scheduling cycle (~1s), which is
		// negligible against the default 4h half-life.
		AllocateFunc: func(event *framework.Event) {
			task := event.Task
			job, ok := ssn.Jobs[task.Job]
			if !ok {
				return
			}
			queueName, targeted := fsp.getQueueName(ssn, job)
			if !targeted {
				return
			}
			qs := fsp.queues[queueName]
			namespace := fsp.getNamespaceFromJob(job)
			res := taskResource(task, qs.resourceKey)
			qs.namespaceRunning[namespace] += res

			klog.V(4).Infof("fairshare: AllocateFunc: task=<%s/%s> namespace=%s res=%.0f newRunning=%.0f usage=%.1f share=%.0f",
				task.Namespace, task.Name, namespace, res, qs.namespaceRunning[namespace],
				fsp.sessionUsage[queueName][namespace], qs.fairShares[namespace])
		},
		DeallocateFunc: func(event *framework.Event) {
			task := event.Task
			job, ok := ssn.Jobs[task.Job]
			if !ok {
				return
			}
			queueName, targeted := fsp.getQueueName(ssn, job)
			if !targeted {
				return
			}
			qs := fsp.queues[queueName]
			namespace := fsp.getNamespaceFromJob(job)
			res := taskResource(task, qs.resourceKey)
			qs.namespaceRunning[namespace] -= res
			if qs.namespaceRunning[namespace] < 0 {
				qs.namespaceRunning[namespace] = 0
			}

			klog.V(4).Infof("fairshare: DeallocateFunc: task=<%s/%s> namespace=%s res=%.0f newRunning=%.0f usage=%.1f share=%.0f",
				task.Namespace, task.Name, namespace, res, qs.namespaceRunning[namespace],
				fsp.sessionUsage[queueName][namespace], qs.fairShares[namespace])
		},
	})
}

func (fsp *fairSharePlugin) OnSessionClose(ssn *framework.Session) {
	klog.V(4).Infof("fairshare: OnSessionClose")
	// Rate-limited to fairshare.flushIntervalSeconds inside maybeFlush; see
	// its doc comment in persist.go for why this replaced a background
	// goroutine+ticker.
	maybeFlush(fsp.persistCfg)
}

// decayAllUsage applies exponential decay to all historical usage:
// usage *= 2^(-elapsed/halfLife). Caller must hold globalMu.
func decayAllUsage(elapsed, halfLife time.Duration) {
	if elapsed <= 0 || halfLife <= 0 {
		return
	}
	factor := math.Pow(2.0, -elapsed.Seconds()/halfLife.Seconds())

	for _, namespaces := range globalUsage {
		for namespace, usage := range namespaces {
			decayed := usage * factor
			if decayed < usageCleanupThreshold {
				delete(namespaces, namespace)
			} else {
				namespaces[namespace] = decayed
			}
		}
	}
}

// ensureGlobalQueueUsage returns the usage map for a queue, creating it if needed.
// Caller must hold globalMu.
func ensureGlobalQueueUsage(queueName string) map[string]float64 {
	if _, ok := globalUsage[queueName]; !ok {
		globalUsage[queueName] = make(map[string]float64)
	}
	return globalUsage[queueName]
}

// snapshotUsage returns a deep copy of globalUsage for lock-free reads.
// Caller must hold globalMu.
func snapshotUsage() map[string]map[string]float64 {
	snap := make(map[string]map[string]float64, len(globalUsage))
	for queue, namespaces := range globalUsage {
		namespaceSnap := make(map[string]float64, len(namespaces))
		for namespace, val := range namespaces {
			namespaceSnap[namespace] = val
		}
		snap[queue] = namespaceSnap
	}
	return snap
}

// DecayFactor computes 2^(-elapsed/halfLife), exported for testing.
func DecayFactor(elapsed, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return 1.0
	}
	return math.Pow(2.0, -elapsed.Seconds()/halfLife.Seconds())
}

// initQueueState creates the per-cycle queueState entry for queueName.
func (fsp *fairSharePlugin) initQueueState(ssn *framework.Session, queueName string) {
	resourceKey := fsp.getResourceKey(queueName)
	totalResource := fsp.getQueueTotalResource(ssn, queueName, resourceKey)

	fsp.queues[queueName] = &queueState{
		resourceKey:      resourceKey,
		totalResource:    totalResource,
		namespaceRunning: make(map[string]float64),
		namespaceDemand:  make(map[string]float64),
	}
}

func (fsp *fairSharePlugin) getQueueName(ssn *framework.Session, job *api.JobInfo) (string, bool) {
	queue, ok := ssn.Queues[job.Queue]
	if !ok {
		return "", false
	}
	if fsp.targetAllQueues {
		return queue.Name, true
	}
	_, targeted := fsp.targetQueueNames[queue.Name]
	return queue.Name, targeted
}

func (fsp *fairSharePlugin) getResourceKey(queueName string) v1.ResourceName {
	if rk, ok := fsp.queueResourceKeys[queueName]; ok {
		return v1.ResourceName(rk)
	}
	return v1.ResourceName(fsp.defaultResource)
}

func (fsp *fairSharePlugin) getQueueTotalResource(ssn *framework.Session, queueName string, resourceKey v1.ResourceName) float64 {
	if queueInfo, ok := ssn.Queues[api.QueueID(queueName)]; ok && queueInfo.Queue != nil {
		cap := queueInfo.Queue.Spec.Capability
		if cap != nil {
			capResource := api.NewResource(cap)
			if total := capResource.Get(resourceKey); total > 0 {
				return total
			}
		}
	}
	return ssn.TotalResource.Get(resourceKey)
}

func (fsp *fairSharePlugin) getNamespaceFromJob(job *api.JobInfo) string {
	if job.Namespace != "" {
		return job.Namespace
	}
	return defaultUnknownNamespace
}

func taskResource(task *api.TaskInfo, resourceKey v1.ResourceName) float64 {
	if task.Resreq == nil {
		return 0
	}
	return task.Resreq.Get(resourceKey)
}

func jobTotalResource(job *api.JobInfo, resourceKey v1.ResourceName) float64 {
	total := 0.0
	for _, task := range job.Tasks {
		total += taskResource(task, resourceKey)
	}
	return total
}

// CalculateFairShares implements the max-min fairness algorithm.
// Given a map of namespace -> total resource demand and the total available resources,
// it returns a map of namespace -> fair share allocation.
func CalculateFairShares(namespaceDemand map[string]float64, totalResource float64) map[string]float64 {
	shares := make(map[string]float64, len(namespaceDemand))

	if len(namespaceDemand) == 0 || totalResource <= 0 {
		return shares
	}

	remaining := make(map[string]float64, len(namespaceDemand))
	for namespace, demand := range namespaceDemand {
		if demand > 0 {
			remaining[namespace] = demand
		}
	}

	if len(remaining) == 0 {
		return shares
	}

	available := totalResource

	for len(remaining) > 0 {
		equalShare := available / float64(len(remaining))

		var satisfied []string
		for namespace, demand := range remaining {
			if demand <= equalShare {
				shares[namespace] = demand
				available -= demand
				satisfied = append(satisfied, namespace)
			}
		}

		for _, namespace := range satisfied {
			delete(remaining, namespace)
		}

		if len(satisfied) == 0 {
			for namespace := range remaining {
				shares[namespace] = equalShare
			}
			break
		}
	}

	return shares
}

// FormatShares returns a human-readable string of fair share allocations.
func FormatShares(shares map[string]float64) string {
	parts := make([]string, 0, len(shares))
	for namespace, share := range shares {
		parts = append(parts, fmt.Sprintf("%s=%.1f", namespace, share))
	}
	return strings.Join(parts, ", ")
}

func formatUsage(usage map[string]float64) string {
	parts := make([]string, 0, len(usage))
	for namespace, u := range usage {
		parts = append(parts, fmt.Sprintf("%s=%.1f", namespace, u))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return strings.Join(parts, ", ")
}
