/*
Copyright 2026 The Volcano Authors.

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

package cache

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	maxPendingPodStatuses   = 16384
	maxPodStatusBytes       = 32 << 20
	podStatusRequestTimeout = 5 * time.Second
)

// WithAsyncPodStatusUpdates moves ordinary PodScheduled=False publication out
// of session close. Nomination, pipelining and binding failures stay synchronous.
func WithAsyncPodStatusUpdates(qps float64) Option {
	return func(sc *SchedulerCache) { sc.podStatusPublisher = newPodStatusPublisher(sc, qps) }
}

// Only value data is retained: no session, JobInfo, TaskInfo or shared Pod.
type podStatusIntent struct {
	uid               types.UID
	job               api.JobID
	namespace, name   string
	condition         v1.PodCondition
	source, published *v1.PodCondition
	generation        uint64
	processing        bool
	canceled          bool
	refresh           bool
}

func (i podStatusIntent) size() int {
	n := 256 + len(i.uid) + len(i.job) + len(i.namespace) + len(i.name) + len(i.condition.Reason) + len(i.condition.Message)
	for _, c := range []*v1.PodCondition{i.source, i.published} {
		if c != nil {
			n += 128 + len(c.Reason) + len(c.Message)
		}
	}
	return n
}

type podStatusPublisher struct {
	cache     *SchedulerCache
	queue     workqueue.TypedRateLimitingInterface[types.UID]
	mu        sync.Mutex
	pending   map[types.UID]podStatusIntent
	bytes     int
	limit     int
	byteLimit int
	budget    *rate.Limiter
	// Fixed stripes serialize background publication with synchronous status
	// writes without retaining a lock for every Pod ever seen.
	stripes [256]chan struct{}
}

func newPodStatusPublisher(sc *SchedulerCache, qps float64) *podStatusPublisher {
	p := &podStatusPublisher{cache: sc, pending: make(map[types.UID]podStatusIntent), limit: maxPendingPodStatuses, byteLimit: maxPodStatusBytes,
		budget: rate.NewLimiter(rate.Limit(qps), 1),
		queue:  workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedItemExponentialFailureRateLimiter[types.UID](100*time.Millisecond, 2*time.Second))}
	for i := range p.stripes {
		p.stripes[i] = make(chan struct{}, 1)
	}
	return p
}

func scheduledCondition(pod *v1.Pod) *v1.PodCondition {
	_, c := podutil.GetPodCondition(&pod.Status, v1.PodScheduled)
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

func sameScheduledCondition(a, b *v1.PodCondition) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// enqueue coalesces by UID; saturation falls back to the synchronous path.
func (p *podStatusPublisher) enqueue(task *api.TaskInfo, reason, message string) bool {
	i := podStatusIntent{uid: task.Pod.UID, job: task.Job, namespace: task.Namespace, name: task.Name,
		condition: v1.PodCondition{Type: v1.PodScheduled, Status: v1.ConditionFalse, Reason: reason, Message: message}, source: scheduledCondition(task.Pod)}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.queue.ShuttingDown() {
		return false
	}
	old, exists := p.pending[i.uid]
	if exists && !old.canceled && sameScheduledCondition(&old.condition, &i.condition) && sameScheduledCondition(old.source, i.source) {
		return true
	}
	if !exists && !podConditionHaveUpdate(&task.Pod.Status, &i.condition) {
		return true
	}
	i.generation = old.generation + 1
	i.published = old.published
	i.processing = old.processing
	i.refresh = old.refresh
	oldSize := 0
	if exists {
		oldSize = old.size()
	}
	if (!exists && len(p.pending) >= p.limit) || p.bytes-oldSize+i.size() > p.byteLimit {
		return false
	}
	p.bytes += i.size() - oldSize
	p.pending[i.uid] = i
	// An existing idle item is already queued or waiting for its retry timer.
	// Waking it early would leave a stale retry timer after its removal.
	if !exists || old.processing {
		p.queue.Add(i.uid)
	}
	return true
}

func (p *podStatusPublisher) stripe(uid types.UID) chan struct{} {
	h := fnv.New32a()
	_, _ = h.Write([]byte(uid))
	return p.stripes[h.Sum32()%uint32(len(p.stripes))]
}

func (p *podStatusPublisher) cancel(uid types.UID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i, ok := p.pending[uid]; ok {
		p.bytes -= i.size()
		// Retain the slot until its queued/retrying key is consumed, so repeated
		// nomination/cancellation cannot bypass the capacity bound.
		i = podStatusIntent{uid: uid, generation: i.generation + 1, canceled: true}
		p.pending[uid] = i
		p.bytes += i.size()
	}
}

func (p *podStatusPublisher) run(stopCh <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	for n := 0; n < options.GetTaskUpdaterWorkerNum(); n++ {
		workers.Go(func() {
			for p.processNext(ctx) {
			}
		})
	}
	<-stopCh
	cancel()
	p.queue.ShutDown()
	workers.Wait()
	p.mu.Lock()
	clear(p.pending)
	p.bytes = 0
	p.mu.Unlock()
}

func (p *podStatusPublisher) processNext(ctx context.Context) bool {
	uid, shutdown := p.queue.Get()
	if shutdown {
		return false
	}
	defer p.queue.Done(uid)
	p.mu.Lock()
	initial, present := p.pending[uid]
	p.mu.Unlock()
	active := present && !initial.canceled && p.eligible(initial)
	// Wait outside the stripe: low-priority publication must not make a
	// synchronous nomination wait for its rate budget. Obsolete work drains
	// without consuming tokens or issuing requests.
	if active {
		if err := p.budget.Wait(ctx); err != nil {
			return false
		}
	}
	lock := p.stripe(uid)
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	defer func() { <-lock }()
	p.mu.Lock()
	i, exists := p.pending[uid]
	if exists && i.generation != initial.generation {
		p.queue.Add(uid)
		p.mu.Unlock()
		return true
	}
	if exists {
		current := i
		current.processing = true
		p.pending[uid] = current
	}
	p.mu.Unlock()
	if !exists {
		p.queue.Forget(uid)
		return true
	}
	requestCtx, cancel := context.WithTimeout(ctx, podStatusRequestTimeout)
	var written *v1.PodCondition
	var err error
	if active && !i.canceled {
		written, err = p.publish(requestCtx, i)
	}
	cancel()
	p.mu.Lock()
	current, exists := p.pending[uid]
	if exists && written != nil {
		oldSize := current.size()
		current.published = written
		current.refresh = true
		p.pending[uid] = current
		p.bytes += current.size() - oldSize
	}
	exhausted := err != nil && p.queue.NumRequeues(uid) >= 5
	if exists && current.generation != i.generation {
		current.processing = false
		p.pending[uid] = current
		p.queue.Forget(uid)
		p.queue.Add(uid)
	} else if exists && (err == nil || exhausted || ctx.Err() != nil) {
		p.bytes -= current.size()
		delete(p.pending, uid)
		p.queue.Forget(uid)
		if exhausted && ctx.Err() == nil {
			klog.ErrorS(err, "Pod status publication retries exhausted", "pod", klog.KRef(i.namespace, i.name), "uid", uid)
		}
	} else if exists {
		current.processing = false
		current.refresh = true
		p.pending[uid] = current
		p.queue.AddRateLimited(uid)
	}
	p.mu.Unlock()
	return true
}

// publish first uses a private copy of the latest cache Pod. The API enforces
// resourceVersion/UID preconditions; rejected attempts refresh from the API.
// A concurrent bind therefore rejects the write instead of accepting a stale
// PodScheduled=False after binding. A new UID at the same name is never touched.
func (p *podStatusPublisher) publish(ctx context.Context, i podStatusIntent) (*v1.PodCondition, error) {
	if !p.eligible(i) {
		return nil, nil
	}
	var pod *v1.Pod
	if i.refresh {
		var err error
		pod, err = p.cache.kubeClient.CoreV1().Pods(i.namespace).Get(ctx, i.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	} else {
		p.cache.Lock()
		if job := p.cache.Jobs[i.job]; job != nil {
			if task := job.Tasks[api.TaskID(i.uid)]; task != nil {
				pod = task.Pod.DeepCopy()
			}
		}
		p.cache.Unlock()
	}
	if pod == nil || pod.UID != i.uid || !ordinaryPendingPod(pod) {
		return nil, nil
	}
	if !p.eligible(i) {
		return nil, nil
	}
	current := scheduledCondition(pod)
	if !podConditionHaveUpdate(&pod.Status, &i.condition) {
		return nil, nil
	}
	// Do not overwrite a newer scheduling decision. Our own previous write is
	// allowed when a new session coalesced another desired condition in flight.
	if !sameScheduledCondition(current, i.source) && !sameScheduledCondition(current, i.published) {
		return nil, nil
	}
	p.mu.Lock()
	latest, ok := p.pending[i.uid]
	p.mu.Unlock()
	if !ok || latest.generation != i.generation {
		return nil, nil
	}
	podutil.UpdatePodCondition(&pod.Status, &i.condition)
	updated, err := p.cache.kubeClient.CoreV1().Pods(i.namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.cache.Recorder.Eventf(updated, v1.EventTypeWarning, "FailedScheduling", "%s", i.condition.Message)
	return scheduledCondition(updated), nil
}

func (p *podStatusPublisher) eligible(i podStatusIntent) bool {
	p.cache.Lock()
	defer p.cache.Unlock()
	job := p.cache.Jobs[i.job]
	if job == nil {
		return false
	}
	task := job.Tasks[api.TaskID(i.uid)]
	return task != nil && !task.SchGated && (task.Status == api.Pending || task.Status == api.Allocated) && task.Pod.UID == i.uid && task.Pod.Status.NominatedNodeName == ""
}

func ordinaryPendingPod(pod *v1.Pod) bool {
	return pod.Spec.NodeName == "" && pod.DeletionTimestamp == nil && len(pod.Spec.SchedulingGates) == 0 && pod.Status.NominatedNodeName == "" && pod.Status.Phase != v1.PodSucceeded && pod.Status.Phase != v1.PodFailed
}
