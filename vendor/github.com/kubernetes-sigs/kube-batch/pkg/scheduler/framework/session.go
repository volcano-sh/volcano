/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	arbcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
)

type Session struct {
	UID types.UID

	cache cache.Cache

	Jobs       []*api.JobInfo
	JobIndex   map[api.JobID]*api.JobInfo
	Nodes      []*api.NodeInfo
	NodeIndex  map[string]*api.NodeInfo
	Queues     []*api.QueueInfo
	QueueIndex map[api.QueueID]*api.QueueInfo
	Others     []*api.TaskInfo
	Backlog    []*api.JobInfo

	plugins        []Plugin
	eventHandlers  []*EventHandler
	jobOrderFns    []api.CompareFn
	queueOrderFns  []api.CompareFn
	taskOrderFns   []api.CompareFn
	predicateFns   []api.PredicateFn
	preemptableFns []api.PreemptableFn
	reclaimableFns []api.ReclaimableFn
	overusedFns    []api.ValidateFn
	jobReadyFns    []api.ValidateFn
}

func openSession(cache cache.Cache) *Session {
	ssn := &Session{
		UID:        uuid.NewUUID(),
		cache:      cache,
		JobIndex:   map[api.JobID]*api.JobInfo{},
		NodeIndex:  map[string]*api.NodeInfo{},
		QueueIndex: map[api.QueueID]*api.QueueInfo{},
	}

	snapshot := cache.Snapshot()

	ssn.Jobs = snapshot.Jobs
	for _, job := range ssn.Jobs {
		ssn.JobIndex[job.UID] = job
	}

	ssn.Nodes = snapshot.Nodes
	for _, node := range ssn.Nodes {
		ssn.NodeIndex[node.Name] = node
	}

	ssn.Queues = snapshot.Queues
	for _, queue := range ssn.Queues {
		ssn.QueueIndex[queue.UID] = queue
	}

	ssn.Others = snapshot.Others

	glog.V(3).Infof("Open Session %v with <%d> Job and <%d> Queues",
		ssn.UID, len(ssn.Jobs), len(ssn.Queues))

	return ssn
}

func closeSession(ssn *Session) {
	ssn.Jobs = nil
	ssn.JobIndex = nil
	ssn.Nodes = nil
	ssn.NodeIndex = nil
	ssn.Backlog = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.jobOrderFns = nil
	ssn.queueOrderFns = nil

	glog.V(3).Infof("Close Session %v", ssn.UID)
}

func (ssn *Session) Statement() *Statement {
	return &Statement{
		ssn: ssn,
	}
}

func (ssn *Session) Pipeline(task *api.TaskInfo, hostname string) error {
	// Only update status in session
	job, found := ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Pipelined); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Pipelined, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	task.NodeName = hostname

	if node, found := ssn.NodeIndex[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
		}
		glog.V(3).Infof("After added Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, ssn.UID)
	}

	for _, eh := range ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	return nil
}

func (ssn *Session) Allocate(task *api.TaskInfo, hostname string) error {
	// Only update status in session
	job, found := ssn.JobIndex[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Allocated, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	task.NodeName = hostname

	if node, found := ssn.NodeIndex[hostname]; found {
		if err := node.AddTask(task); err != nil {
			glog.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, ssn.UID, err)
		}
		glog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		glog.Errorf("Failed to found Node <%s> in Session <%s> index when binding.",
			hostname, ssn.UID)
	}

	// Callbacks
	for _, eh := range ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	if ssn.JobReady(job) {
		for _, task := range job.TaskStatusIndex[api.Allocated] {
			ssn.dispatch(task)
		}
	}

	return nil
}

func (ssn *Session) dispatch(task *api.TaskInfo) error {
	if err := ssn.cache.Bind(task, task.NodeName); err != nil {
		return err
	}

	// Update status in session
	if job, found := ssn.JobIndex[task.Job]; found {
		if err := job.UpdateTaskStatus(task, api.Binding); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, api.Binding, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			task.Job, ssn.UID)
	}

	return nil
}

func (ssn *Session) Reclaimable(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) []*api.TaskInfo {
	var victims []*api.TaskInfo

	for _, rf := range ssn.reclaimableFns {
		candidates := rf(reclaimer, reclaimees)
		if victims == nil {
			victims = candidates
		} else {
			intersection := []*api.TaskInfo{}
			// Get intersection of victims and candidates.
			for _, v := range victims {
				for _, c := range candidates {
					if v.UID == c.UID {
						intersection = append(intersection, v)
					}
				}
			}

			// Update victims to intersection
			victims = intersection
		}
	}

	return victims
}

func (ssn *Session) Evict(reclaimee *api.TaskInfo, reason string) error {
	if err := ssn.cache.Evict(reclaimee, reason); err != nil {
		return err
	}

	// Update status in session
	job, found := ssn.JobIndex[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, api.Releasing); err != nil {
			glog.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, api.Releasing, ssn.UID, err)
		}
	} else {
		glog.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, ssn.UID)
	}

	// Update task in node.
	if node, found := ssn.NodeIndex[reclaimee.NodeName]; found {
		node.UpdateTask(reclaimee)
	}

	for _, eh := range ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	return nil
}

func (ssn *Session) Preemptable(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
	if len(ssn.preemptableFns) == 0 {
		return nil
	}

	victims := ssn.preemptableFns[0](preemptor, preemptees)
	for _, pf := range ssn.preemptableFns[1:] {
		intersection := []*api.TaskInfo{}

		candidates := pf(preemptor, preemptees)
		// Get intersection of victims and candidates.
		for _, v := range victims {
			for _, c := range candidates {
				if v.UID == c.UID {
					intersection = append(intersection, v)
				}
			}
		}

		// Update victims to intersection
		victims = intersection
	}

	return victims
}

// Backoff discards a job from session, so no plugin/action handles it.
func (ssn *Session) Backoff(job *api.JobInfo, event arbcorev1.Event, reason string) error {
	if err := ssn.cache.Backoff(job, event, reason); err != nil {
		glog.Errorf("Failed to backoff job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return err
	}

	glog.V(3).Infof("Discard Job <%s/%s> because %s",
		job.Namespace, job.Name, reason)

	// Delete Job from Session after recording event.
	delete(ssn.JobIndex, job.UID)
	for i, j := range ssn.Jobs {
		if j.UID == job.UID {
			ssn.Jobs[i] = ssn.Jobs[len(ssn.Jobs)-1]
			ssn.Jobs = ssn.Jobs[:len(ssn.Jobs)-1]
			break
		}
	}

	return nil
}

func (ssn *Session) AddEventHandler(eh *EventHandler) {
	ssn.eventHandlers = append(ssn.eventHandlers, eh)
}

func (ssn *Session) AddJobOrderFn(cf api.CompareFn) {
	ssn.jobOrderFns = append(ssn.jobOrderFns, cf)
}

func (ssn *Session) AddQueueOrderFn(qf api.CompareFn) {
	ssn.queueOrderFns = append(ssn.queueOrderFns, qf)
}

func (ssn *Session) AddTaskOrderFn(cf api.CompareFn) {
	ssn.taskOrderFns = append(ssn.taskOrderFns, cf)
}

func (ssn *Session) AddPreemptableFn(cf api.PreemptableFn) {
	ssn.preemptableFns = append(ssn.preemptableFns, cf)
}

func (ssn *Session) AddReclaimableFn(rf api.ReclaimableFn) {
	ssn.reclaimableFns = append(ssn.reclaimableFns, rf)
}

func (ssn *Session) AddJobReadyFn(vf api.ValidateFn) {
	ssn.jobReadyFns = append(ssn.jobReadyFns, vf)
}

func (ssn *Session) AddPredicateFn(pf api.PredicateFn) {
	ssn.predicateFns = append(ssn.predicateFns, pf)
}

func (ssn *Session) AddOverusedFn(fn api.ValidateFn) {
	ssn.overusedFns = append(ssn.overusedFns, fn)
}

func (ssn *Session) Overused(queue *api.QueueInfo) bool {
	for _, of := range ssn.overusedFns {
		if of(queue) {
			return true
		}
	}

	return false
}

func (ssn *Session) JobReady(obj interface{}) bool {
	for _, jrf := range ssn.jobReadyFns {
		if !jrf(obj) {
			return false
		}
	}

	return true
}

func (ssn *Session) JobOrderFn(l, r interface{}) bool {
	for _, jof := range ssn.jobOrderFns {
		if j := jof(l, r); j != 0 {
			return j < 0
		}
	}

	// If no job order funcs, order job by UID.
	lv := l.(*api.JobInfo)
	rv := r.(*api.JobInfo)

	if lv.CreationTimestamp.Equal(&rv.CreationTimestamp) {
		return lv.UID < rv.UID
	}

	return lv.CreationTimestamp.Before(&rv.CreationTimestamp)

}

func (ssn *Session) QueueOrderFn(l, r interface{}) bool {
	for _, qof := range ssn.queueOrderFns {
		if j := qof(l, r); j != 0 {
			return j < 0
		}
	}

	// If no queue order funcs, order queue by UID.
	lv := l.(*api.QueueInfo)
	rv := r.(*api.QueueInfo)

	return lv.UID < rv.UID
}

func (ssn *Session) TaskCompareFns(l, r interface{}) int {
	for _, tof := range ssn.taskOrderFns {
		if j := tof(l, r); j != 0 {
			return j
		}
	}

	return 0
}

func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	if res := ssn.TaskCompareFns(l, r); res != 0 {
		return res < 0
	}

	// If no task order funcs, order task by UID.
	lv := l.(*api.TaskInfo)
	rv := r.(*api.TaskInfo)

	return lv.UID < rv.UID
}

func (ssn *Session) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	for _, pfn := range ssn.predicateFns {
		err := pfn(task, node)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ssn Session) String() string {
	msg := fmt.Sprintf("Session %v: \n", ssn.UID)

	for _, job := range ssn.Jobs {
		msg = fmt.Sprintf("%s%v\n", msg, job)
	}

	for _, node := range ssn.Nodes {
		msg = fmt.Sprintf("%s%v\n", msg, node)
	}

	return msg

}
