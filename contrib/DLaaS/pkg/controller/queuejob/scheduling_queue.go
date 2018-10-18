/*
Copyright 2017 The Kubernetes Authors.

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

// This file contains structures that implement scheduling queue types.
// Scheduling queues hold pods waiting to be scheduled. This file has two types
// of scheduling queue: 1) a FIFO, which is mostly the same as cache.FIFO, 2) a
// priority queue which has two sub queues. One sub-queue holds pods that are
// being considered for scheduling. This is called activeQ. Another queue holds
// pods that are already tried and are determined to be unschedulable. The latter
// is called unschedulableQ.
// FIFO is here for flag-gating purposes and allows us to use the traditional
// scheduling queue when util.PodPriorityEnabled() returns false.

package queuejob

import (
	"fmt"
	"sync"

	"k8s.io/client-go/tools/cache"
	qjobv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/golang/glog"
	"reflect"
)

// SchedulingQueue is an interface for a queue to store pods waiting to be scheduled.
// The interface follows a pattern similar to cache.FIFO and cache.Heap and
// makes it easy to use those data structures as a SchedulingQueue.
type SchedulingQueue interface {
	Add(qj *qjobv1.XQueueJob) error
	AddIfNotPresent(qj *qjobv1.XQueueJob) error
	AddUnschedulableIfNotPresent(qj *qjobv1.XQueueJob) error
	Pop() (*qjobv1.XQueueJob, error)
	Update(oldQJ, newQJ *qjobv1.XQueueJob) error
	Delete(QJ *qjobv1.XQueueJob) error
	MoveAllToActiveQueue()
}

// NewSchedulingQueue initializes a new scheduling queue. If pod priority is
// enabled a priority queue is returned. If it is disabled, a FIFO is returned.
func NewSchedulingQueue() SchedulingQueue {
		return NewPriorityQueue()
}

// UnschedulablePods is an interface for a queue that is used to keep unschedulable
// pods. These pods are not actively reevaluated for scheduling. They are moved
// to the active scheduling queue on certain events, such as termination of a pod
// in the cluster, addition of nodes, etc.
type UnschedulableQJs interface {
	Add(p *qjobv1.XQueueJob)
	Delete(p *qjobv1.XQueueJob)
	Update(p *qjobv1.XQueueJob)
	Get(p *qjobv1.XQueueJob) *qjobv1.XQueueJob
	Clear()
}

// PriorityQueue implements a scheduling queue. It is an alternative to FIFO.
// The head of PriorityQueue is the highest priority pending QJ. This structure
// has two sub queues. One sub-queue holds QJ that are being considered for
// scheduling. This is called activeQ and is a Heap. Another queue holds
// pods that are already tried and are determined to be unschedulable. The latter
// is called unschedulableQ.
// Heap is already thread safe, but we need to acquire another lock here to ensure
// atomicity of operations on the two data structures..
type PriorityQueue struct {
	lock sync.RWMutex
	cond sync.Cond
	// activeQ is heap structure that scheduler actively looks at to find QJs to
	// schedule. Head of heap is the highest priority QJ.
	activeQ *Heap
	// unschedulableQ holds QJs that have been tried and determined unschedulable.
	unschedulableQ *UnschedulableQJMap
	
	receivedMoveRequest bool
}

// Making sure that PriorityQueue implements SchedulingQueue.
var _ = SchedulingQueue(&PriorityQueue{})

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		activeQ:        newHeap(cache.MetaNamespaceKeyFunc, HigherPriorityQJ),
		unschedulableQ: newUnschedulableQJMap(),
	}
	pq.cond.L = &pq.lock
	return pq
}

// Add adds a QJ to the active queue. It should be called only when a new QJ
// is added so there is no chance the QJ is already in either queue.
func (p *PriorityQueue) Add(qj *qjobv1.XQueueJob) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	err := p.activeQ.Add(qj)
	if err != nil {
		glog.Errorf("Error adding QJ %v to the scheduling queue: %v", qj.Name, err)
	} else {
		if p.unschedulableQ.Get(qj) != nil {
			glog.Errorf("Error: QJ %v is already in the unschedulable queue.", qj.Name)
			p.unschedulableQ.Delete(qj)
		}
		p.cond.Broadcast()
	}
	return err
}

// AddIfNotPresent adds a pod to the active queue if it is not present in any of
// the two queues. If it is present in any, it doesn't do any thing.
func (p *PriorityQueue) AddIfNotPresent(qj *qjobv1.XQueueJob) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.unschedulableQ.Get(qj) != nil {
		return nil
	}
	if _, exists, _ := p.activeQ.Get(qj); exists {
		return nil
	}
	err := p.activeQ.Add(qj)
	if err != nil {
		glog.Errorf("Error adding pod %v to the scheduling queue: %v", qj.Name, err)
	} else {
		p.cond.Broadcast()
	}
	return err
}

func isPodUnschedulable(qj *qjobv1.XQueueJob) bool {
	//_, cond := podutil.GetPodCondition(&pod.Status, v1.PodScheduled)
	//return cond != nil && cond.Status == v1.ConditionFalse && cond.Reason == v1.PodReasonUnschedulable
	//TODO
	return false
}

// AddUnschedulableIfNotPresent does nothing if the pod is present in either
// queue. Otherwise it adds the pod to the unschedulable queue if
// p.receivedMoveRequest is false, and to the activeQ if p.receivedMoveRequest is true.
func (p *PriorityQueue) AddUnschedulableIfNotPresent(qj *qjobv1.XQueueJob) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.unschedulableQ.Get(qj) != nil {
		return fmt.Errorf("pod is already present in unschedulableQ")
	}
	if _, exists, _ := p.activeQ.Get(qj); exists {
		return fmt.Errorf("pod is already present in the activeQ")
	}
	if !p.receivedMoveRequest && isPodUnschedulable(qj) {
		p.unschedulableQ.Add(qj)
		return nil
	}
	err := p.activeQ.Add(qj)
	if err == nil {
		p.cond.Broadcast()
	}
	return err
}

// Pop removes the head of the active queue and returns it. It blocks if the
// activeQ is empty and waits until a new item is added to the queue. It also
// clears receivedMoveRequest to mark the beginning of a new scheduling cycle.
func (p *PriorityQueue) Pop() (*qjobv1.XQueueJob, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	for len(p.activeQ.data.queue) == 0 {
		p.cond.Wait()
	}
	obj, err := p.activeQ.Pop()
	if err != nil {
		return nil, err
	}
	qj := obj.(*qjobv1.XQueueJob)
	p.receivedMoveRequest = false
	return qj, err
}

// isPodUpdated checks if the pod is updated in a way that it may have become
// schedulable. It drops status of the pod and compares it with old version.
func (p *PriorityQueue) isQJUpdated(oldQJ, newQJ *qjobv1.XQueueJob) bool {
	strip := func(qj *qjobv1.XQueueJob) *qjobv1.XQueueJob {
		p := qj.DeepCopy()
		p.ResourceVersion = ""
		p.Generation = 0
		return p
	}
	return !reflect.DeepEqual(strip(oldQJ), strip(newQJ))
}

// Update updates a pod in the active queue if present. Otherwise, it removes
// the item from the unschedulable queue and adds the updated one to the active
// queue.
func (p *PriorityQueue) Update(oldQJ, newQJ *qjobv1.XQueueJob) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	// If the pod is already in the active queue, just update it there.
	if _, exists, _ := p.activeQ.Get(newQJ); exists {
		err := p.activeQ.Update(newQJ)
		return err
	}
	// If the pod is in the unschedulable queue, updating it may make it schedulable.
	if usQJ := p.unschedulableQ.Get(newQJ); usQJ != nil {
		if p.isQJUpdated(oldQJ, newQJ) {
			p.unschedulableQ.Delete(usQJ)
			err := p.activeQ.Add(newQJ)
			if err == nil {
				p.cond.Broadcast()
			}
			return err
		}
		p.unschedulableQ.Update(newQJ)
		return nil
	}
	// If pod is not in any of the two queue, we put it in the active queue.
	err := p.activeQ.Add(newQJ)
	if err == nil {
		p.cond.Broadcast()
	}
	return err
}

// Delete deletes the item from either of the two queues. It assumes the pod is
// only in one queue.
func (p *PriorityQueue) Delete(qj *qjobv1.XQueueJob) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if _, exists, _ := p.activeQ.Get(qj); exists {
		return p.activeQ.Delete(qj)
	}
	p.unschedulableQ.Delete(qj)
	return nil
}

// MoveAllToActiveQueue moves all pods from unschedulableQ to activeQ. This
// function adds all pods and then signals the condition variable to ensure that
// if Pop() is waiting for an item, it receives it after all the pods are in the
// queue and the head is the highest priority pod.
// TODO(bsalamat): We should add a back-off mechanism here so that a high priority
// pod which is unschedulable does not go to the head of the queue frequently. For
// example in a cluster where a lot of pods being deleted, such a high priority
// pod can deprive other pods from getting scheduled.
func (p *PriorityQueue) MoveAllToActiveQueue() {
	p.lock.Lock()
	defer p.lock.Unlock()
	var unschedulableQJs []interface{}
	for _, qj := range p.unschedulableQ.pods {
		unschedulableQJs = append(unschedulableQJs, qj)
	}
	p.activeQ.BulkAdd(unschedulableQJs)
	p.unschedulableQ.Clear()
	p.receivedMoveRequest = true
	p.cond.Broadcast()
}

func (p *PriorityQueue) movePodsToActiveQueue(pods []*qjobv1.XQueueJob) {
	p.lock.Lock()
	defer p.lock.Unlock()
	for _, pod := range pods {
		p.activeQ.Add(pod)
		p.unschedulableQ.Delete(pod)
	}
	p.receivedMoveRequest = true
	p.cond.Broadcast()
}

// UnschedulablePodsMap holds pods that cannot be scheduled. This data structure
// is used to implement unschedulableQ.
type UnschedulableQJMap struct {
	// pods is a map key by a pod's full-name and the value is a pointer to the pod.
	pods    map[string]*qjobv1.XQueueJob
	keyFunc func(*qjobv1.XQueueJob) string
}

type UnschedulableQueueJobs interface {
	Add(pod *qjobv1.XQueueJob)
	Delete(pod *qjobv1.XQueueJob)
	Update(pod *qjobv1.XQueueJob)
	Get(pod *qjobv1.XQueueJob) *qjobv1.XQueueJob
	Clear()
}

var _ = UnschedulableQueueJobs(&UnschedulableQJMap{})

// Add adds a pod to the unschedulable pods.
func (u *UnschedulableQJMap) Add(pod *qjobv1.XQueueJob) {
	podjkey := GetXQJFullName(pod)
	if _, exists := u.pods[podjkey]; !exists {
		u.pods[podjkey] = pod
	}
}

// Delete deletes a pod from the unschedulable pods.
func (u *UnschedulableQJMap) Delete(pod *qjobv1.XQueueJob) {
	podKey := GetXQJFullName(pod)
	if _, exists := u.pods[podKey]; exists {
		delete(u.pods, podKey)
	}
}

// Update updates a pod in the unschedulable pods.
func (u *UnschedulableQJMap) Update(pod *qjobv1.XQueueJob) {
	podKey := GetXQJFullName(pod)
	_, exists := u.pods[podKey]
	if !exists {
		u.Add(pod)
		return
	}
	u.pods[podKey] = pod
}

// Get returns the pod if a pod with the same key as the key of the given "pod"
// is found in the map. It returns nil otherwise.
func (u *UnschedulableQJMap) Get(pod *qjobv1.XQueueJob) *qjobv1.XQueueJob {
	podKey := GetXQJFullName(pod)
	if p, exists := u.pods[podKey]; exists {
		return p
	}
	return nil
}

// Clear removes all the entries from the unschedulable maps.
func (u *UnschedulableQJMap) Clear() {
	u.pods = make(map[string]*qjobv1.XQueueJob)
}

// newUnschedulablePodsMap initializes a new object of UnschedulablePodsMap.
func newUnschedulableQJMap() *UnschedulableQJMap {
	return &UnschedulableQJMap{
		pods:    make(map[string]*qjobv1.XQueueJob),
		keyFunc: GetXQJFullName,
	}
}

