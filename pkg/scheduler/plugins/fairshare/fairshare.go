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
// per-user fair share scheduling within target queues using decayed cumulative
// usage tracking. It prevents any single user from monopolizing resources when
// multiple users have pending work, and remembers past usage across scheduling
// cycles so that heavy consumers are deprioritized even after their jobs finish.
//
// User identity is derived from the job's namespace (namespace-per-user pattern).
// The tracked resource type is configurable per queue (e.g., nvidia.com/gpu for
// GPU queues, cpu for CPU queues).
//
// Historical usage decays exponentially with a configurable half-life (default
// 4 hours). This means a user who consumed 10 GPU-hours will see their usage
// penalty halve every 4 hours, naturally converging back to equal priority.
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
	globalUsage     = make(map[string]map[string]float64) // [queue][user] → resource-seconds
	globalLastCycle time.Time
)

const (
	// PluginName is the name used to register this plugin with the framework.
	PluginName = "fairshare"

	defaultResourceKey     = "nvidia.com/gpu"
	defaultUnknownUser     = "_unknown"
	defaultHalfLifeMinutes = 240 // 4 hours
	usageEpsilon           = 1.0 // 1 resource-second: treat as equal
	usageCleanupThreshold  = 0.01
)

// queueState holds per-queue fair share tracking for one scheduling cycle.
type queueState struct {
	resourceKey   v1.ResourceName
	totalResource float64
	userRunning   map[string]float64
	userDemand    map[string]float64
	fairShares    map[string]float64
}

type fairSharePlugin struct {
	pluginArguments framework.Arguments

	defaultResource   string
	enableEnqueueGate bool
	queueResourceKeys map[string]string
	targetQueueNames  map[string]struct{}
	halfLife          time.Duration
	persistCfg        persistConfig

	queues map[string]*queueState

	// sessionUsage is a per-session snapshot of globalUsage taken under lock.
	// All reads during the scheduling cycle use this snapshot to avoid races.
	sessionUsage map[string]map[string]float64
}

// New creates a new fairSharePlugin instance with the given plugin arguments.
//
// Supported arguments:
//
//	fairshare.targetQueues          - comma-separated queue names (required)
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

	klog.V(2).Infof("fairshare: plugin created — queues=%v resource=%s halfLife=%s enqueueGate=%v persist=%v",
		fsp.targetQueueNames, fsp.defaultResource, fsp.halfLife, fsp.enableEnqueueGate, fsp.persistCfg.enabled)

	return fsp
}

func (fsp *fairSharePlugin) Name() string {
	return PluginName
}

// OnSessionOpen is called at the beginning of each scheduling cycle. It:
//  1. Decays historical usage and accumulates running usage for the elapsed period
//  2. Scans all jobs in target queues, computes per-user resource demand and running counts
//  3. Runs the max-min fairness algorithm per queue
//  4. Registers JobOrderFn (usage-based ordering), optional JobEnqueueableFn, and EventHandler
func (fsp *fairSharePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("fairshare: OnSessionOpen enter")

	fsp.queues = make(map[string]*queueState)

	for queueName := range fsp.targetQueueNames {
		resourceKey := fsp.getResourceKey(queueName)
		totalResource := fsp.getQueueTotalResource(ssn, queueName, resourceKey)

		fsp.queues[queueName] = &queueState{
			resourceKey:   resourceKey,
			totalResource: totalResource,
			userRunning:   make(map[string]float64),
			userDemand:    make(map[string]float64),
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
		user := fsp.getUserFromJob(job)

		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, task := range tasks {
					res := taskResource(task, qs.resourceKey)
					qs.userRunning[user] += res

					if elapsed > 0 {
						ensureGlobalQueueUsage(queueName)[user] += res * elapsed.Seconds()
					}
				}
			}
		}

		if pendingTasks, ok := job.TaskStatusIndex[api.Pending]; ok {
			for _, task := range pendingTasks {
				qs.userDemand[user] += taskResource(task, qs.resourceKey)
			}
		}
	}

	// Snapshot globalUsage so callbacks can read without holding the lock.
	fsp.sessionUsage = snapshotUsage()
	globalMu.Unlock()

	for queueName, qs := range fsp.queues {
		totalDemand := make(map[string]float64)
		for user := range qs.userRunning {
			totalDemand[user] = qs.userRunning[user] + qs.userDemand[user]
		}
		for user := range qs.userDemand {
			if _, ok := totalDemand[user]; !ok {
				totalDemand[user] = qs.userDemand[user]
			}
		}

		qs.fairShares = CalculateFairShares(totalDemand, qs.totalResource)

		usage := fsp.sessionUsage[queueName]
		klog.V(2).Infof("fairshare: queue=%s users=%d totalResource=%.0f running=%v demand=%v",
			queueName, len(totalDemand), qs.totalResource, qs.userRunning, qs.userDemand)
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
		lUser := fsp.getUserFromJob(lJob)
		rUser := fsp.getUserFromJob(rJob)

		queueUsage := fsp.sessionUsage[lQueue]
		lUsage := queueUsage[lUser]
		rUsage := queueUsage[rUser]

		klog.V(5).Infof("fairshare: JobOrderFn: <%s/%s> user=%s usage=%.1f running=%.0f, <%s/%s> user=%s usage=%.1f running=%.0f",
			lJob.Namespace, lJob.Name, lUser, lUsage, qs.userRunning[lUser],
			rJob.Namespace, rJob.Name, rUser, rUsage, qs.userRunning[rUser])

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

		lRunning := qs.userRunning[lUser]
		rRunning := qs.userRunning[rUser]
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
			user := fsp.getUserFromJob(job)
			share, ok := qs.fairShares[user]
			if !ok {
				return util.Abstain
			}

			running := qs.userRunning[user]
			jobRes := jobTotalResource(job, qs.resourceKey)

			klog.V(5).Infof("fairshare: JobEnqueueableFn: job=<%s/%s> user=%s running=%.0f jobRes=%.0f share=%.0f",
				job.Namespace, job.Name, user, running, jobRes, share)

			if running >= share && jobRes > 0 {
				klog.V(3).Infof("fairshare: REJECT enqueue for <%s/%s>: user %s at %.0f (share=%.0f)",
					job.Namespace, job.Name, user, running, share)
				return util.Reject
			}

			return util.Abstain
		})
	}

	ssn.AddEventHandler(&framework.EventHandler{
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
			user := fsp.getUserFromJob(job)
			res := taskResource(task, qs.resourceKey)
			qs.userRunning[user] += res

			klog.V(4).Infof("fairshare: AllocateFunc: task=<%s/%s> user=%s res=%.0f newRunning=%.0f usage=%.1f share=%.0f",
				task.Namespace, task.Name, user, res, qs.userRunning[user],
				fsp.sessionUsage[queueName][user], qs.fairShares[user])
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
			user := fsp.getUserFromJob(job)
			res := taskResource(task, qs.resourceKey)
			qs.userRunning[user] -= res
			if qs.userRunning[user] < 0 {
				qs.userRunning[user] = 0
			}

			klog.V(4).Infof("fairshare: DeallocateFunc: task=<%s/%s> user=%s res=%.0f newRunning=%.0f usage=%.1f share=%.0f",
				task.Namespace, task.Name, user, res, qs.userRunning[user],
				fsp.sessionUsage[queueName][user], qs.fairShares[user])
		},
	})
}

func (fsp *fairSharePlugin) OnSessionClose(ssn *framework.Session) {
	klog.V(4).Infof("fairshare: OnSessionClose")
}

// decayAllUsage applies exponential decay to all historical usage:
// usage *= 2^(-elapsed/halfLife). Caller must hold globalMu.
func decayAllUsage(elapsed, halfLife time.Duration) {
	if elapsed <= 0 || halfLife <= 0 {
		return
	}
	factor := math.Pow(2.0, -elapsed.Seconds()/halfLife.Seconds())

	for _, users := range globalUsage {
		for user, usage := range users {
			decayed := usage * factor
			if decayed < usageCleanupThreshold {
				delete(users, user)
			} else {
				users[user] = decayed
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
	for queue, users := range globalUsage {
		userSnap := make(map[string]float64, len(users))
		for user, val := range users {
			userSnap[user] = val
		}
		snap[queue] = userSnap
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

func (fsp *fairSharePlugin) getQueueName(ssn *framework.Session, job *api.JobInfo) (string, bool) {
	queue, ok := ssn.Queues[job.Queue]
	if !ok {
		return "", false
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

func (fsp *fairSharePlugin) getUserFromJob(job *api.JobInfo) string {
	if job.Namespace != "" {
		return job.Namespace
	}
	return defaultUnknownUser
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
// Given a map of user -> total resource demand and the total available resources,
// it returns a map of user -> fair share allocation.
func CalculateFairShares(userDemand map[string]float64, totalResource float64) map[string]float64 {
	shares := make(map[string]float64, len(userDemand))

	if len(userDemand) == 0 || totalResource <= 0 {
		return shares
	}

	remaining := make(map[string]float64, len(userDemand))
	for user, demand := range userDemand {
		if demand > 0 {
			remaining[user] = demand
		}
	}

	if len(remaining) == 0 {
		return shares
	}

	available := totalResource

	for len(remaining) > 0 {
		equalShare := available / float64(len(remaining))

		var satisfied []string
		for user, demand := range remaining {
			if demand <= equalShare {
				shares[user] = demand
				available -= demand
				satisfied = append(satisfied, user)
			}
		}

		for _, user := range satisfied {
			delete(remaining, user)
		}

		if len(satisfied) == 0 {
			for user := range remaining {
				shares[user] = equalShare
			}
			break
		}
	}

	return shares
}

// FormatShares returns a human-readable string of fair share allocations.
func FormatShares(shares map[string]float64) string {
	parts := make([]string, 0, len(shares))
	for user, share := range shares {
		parts = append(parts, fmt.Sprintf("%s=%.1f", user, share))
	}
	return strings.Join(parts, ", ")
}

func formatUsage(usage map[string]float64) string {
	parts := make([]string, 0, len(usage))
	for user, u := range usage {
		parts = append(parts, fmt.Sprintf("%s=%.1f", user, u))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return strings.Join(parts, ", ")
}
