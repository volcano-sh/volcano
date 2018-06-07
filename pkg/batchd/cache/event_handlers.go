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

package cache

import (
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/golang/glog"
	arbapi "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/api"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
)

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addPod(pod *v1.Pod) error {
	key := arbapi.PodKey(pod)

	if _, ok := sc.Tasks[key]; ok {
		return fmt.Errorf("pod %v exist", key)
	}

	pi := arbapi.NewTaskInfo(pod)
	sc.Tasks[key] = pi

	if len(pi.Job) != 0 {
		if _, found := sc.Jobs[pi.Job]; !found {
			sc.Jobs[pi.Job] = arbapi.NewJobInfo(pi.Job)
		}

		sc.Jobs[pi.Job].AddTaskInfo(pi)
	} else {
		glog.Warningf("The controller of pod %v/%v is empty, can not schedule it.",
			pod.Namespace, pod.Name)
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	if err := sc.deletePod(oldPod); err != nil {
		return err
	}
	return sc.addPod(newPod)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePod(pod *v1.Pod) error {
	key := arbapi.PodKey(pod)

	pi, ok := sc.Tasks[key]
	if !ok {
		return fmt.Errorf("pod %v doesn't exist", key)
	}
	delete(sc.Tasks, key)

	if len(pi.Job) != 0 {
		if job, found := sc.Jobs[pi.Job]; found {
			job.DeleteTaskInfo(pi)
		} else {
			glog.Warningf("Failed to find Job for Task %v:%v/%v.",
				pi.UID, pi.Namespace, pi.Name)
		}
	}

	if len(pi.NodeName) != 0 {
		node := sc.Nodes[pi.NodeName]
		if node != nil {
			node.RemoveTask(pi)
		}
	}

	return nil
}

func (sc *SchedulerCache) AddPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert to *v1.Pod: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add pod(%s) into cache, status (%s)", pod.Name, pod.Status.Phase)
	err := sc.addPod(pod)
	if err != nil {
		glog.Errorf("Failed to add pod %s into cache: %v", pod.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdatePod(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *v1.Pod: %v", oldObj)
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Cannot convert newObj to *v1.Pod: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldPod(%s) status(%s) newPod(%s) status(%s) in cache", oldPod.Name, oldPod.Status.Phase, newPod.Name, newPod.Status.Phase)
	err := sc.updatePod(oldPod, newPod)
	if err != nil {
		glog.Errorf("Failed to update pod %v in cache: %v", oldPod.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeletePod(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Delete pod(%s) status(%s) from cache", pod.Name, pod.Status.Phase)
	err := sc.deletePod(pod)
	if err != nil {
		glog.Errorf("Failed to delete pod %v from cache: %v", pod.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addNode(node *v1.Node) error {
	if sc.Nodes[node.Name] != nil {
		sc.Nodes[node.Name].SetNode(node)
	} else {
		sc.Nodes[node.Name] = arbapi.NewNodeInfo(node)
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updateNode(oldNode, newNode *v1.Node) error {
	// Did not delete the old node, just update related info, e.g. allocatable.
	if sc.Nodes[newNode.Name] != nil {
		sc.Nodes[newNode.Name].SetNode(newNode)
		return nil
	}

	return fmt.Errorf("node <%s> does not exist", newNode.Name)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deleteNode(node *v1.Node) error {
	if _, ok := sc.Nodes[node.Name]; !ok {
		return fmt.Errorf("node <%s> does not exist", node.Name)
	}
	delete(sc.Nodes, node.Name)
	return nil
}

func (sc *SchedulerCache) AddNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert to *v1.Node: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add node(%s) into cache", node.Name)
	err := sc.addNode(node)
	if err != nil {
		glog.Errorf("Failed to add node %s into cache: %v", node.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdateNode(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		glog.Errorf("Cannot convert newObj to *v1.Node: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldNode(%s) newNode(%s) in cache", oldNode.Name, newNode.Name)
	err := sc.updateNode(oldNode, newNode)
	if err != nil {
		glog.Errorf("Failed to update node %v in cache: %v", oldNode.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeleteNode(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Node: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Delete node(%s) from cache", node.Name)
	err := sc.deleteNode(node)
	if err != nil {
		glog.Errorf("Failed to delete node %s from cache: %v", node.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) setSchedulingSpec(ss *arbv1.SchedulingSpec) error {
	job := arbapi.JobID(arbapi.GetController(ss))

	if len(job) == 0 {
		return fmt.Errorf("the controller of SchedulingSpec is empty")
	}

	if _, found := sc.Jobs[job]; !found {
		sc.Jobs[job] = arbapi.NewJobInfo(job)
	}

	sc.Jobs[job].SetSchedulingSpec(ss)

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updateSchedulingSpec(oldQueue, newQueue *arbv1.SchedulingSpec) error {
	return sc.setSchedulingSpec(newQueue)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deleteSchedulingSpec(queue *arbv1.SchedulingSpec) error {

	return nil
}

func (sc *SchedulerCache) AddSchedulingSpec(obj interface{}) {
	queue, ok := obj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.Queue: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add Queue(%s) into cache, spec(%#v)", queue.Name, queue.Spec)
	err := sc.setSchedulingSpec(queue)
	if err != nil {
		glog.Errorf("Failed to add Queue %s into cache: %v", queue.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) UpdateSchedulingSpec(oldObj, newObj interface{}) {
	oldQueue, ok := oldObj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *arbv1.Queue: %v", oldObj)
		return
	}
	newQueue, ok := newObj.(*arbv1.SchedulingSpec)
	if !ok {
		glog.Errorf("Cannot convert newObj to *arbv1.Queue: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldQueue(%s) in cache, spec(%#v)", oldQueue.Name, oldQueue.Spec)
	glog.V(4).Infof("Update newQueue(%s) in cache, spec(%#v)", newQueue.Name, newQueue.Spec)
	err := sc.updateSchedulingSpec(oldQueue, newQueue)
	if err != nil {
		glog.Errorf("Failed to update queue %s into cache: %v", oldQueue.Name, err)
		return
	}
	return
}

func (sc *SchedulerCache) DeleteSchedulingSpec(obj interface{}) {
	var queue *arbv1.SchedulingSpec
	switch t := obj.(type) {
	case *arbv1.SchedulingSpec:
		queue = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		queue, ok = t.Obj.(*arbv1.SchedulingSpec)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Queue: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Queue: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteSchedulingSpec(queue)
	if err != nil {
		glog.Errorf("Failed to delete Queue %s from cache: %v", queue.Name, err)
		return
	}
	return
}
