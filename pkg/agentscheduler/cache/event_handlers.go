/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Migrated to v1beta1 API versions for PodGroup and Queue resources
- Added support for ResourceQuota/PriorityClass/NUMA topology management
- Enhanced caching with HyperNode support and CSI resource handling

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"

	"volcano.sh/apis/pkg/apis/utils"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func isTerminated(status schedulingapi.TaskStatus) bool {
	return status == schedulingapi.Succeeded || status == schedulingapi.Failed
}

func (sc *SchedulerCache) addTask(pi *schedulingapi.TaskInfo) error {
	if len(pi.NodeName) != 0 {
		if _, found := sc.Nodes[pi.NodeName]; !found {
			sc.Nodes[pi.NodeName] = schedulingapi.NewNodeInfo(nil)
			sc.Nodes[pi.NodeName].Name = pi.NodeName
		}

		node := sc.Nodes[pi.NodeName]
		if !isTerminated(pi.Status) {
			if err := node.AddTask(pi); err != nil {
				return err
			}
		} else {
			klog.V(4).Infof("Pod <%v/%v> is in status %s.", pi.Namespace, pi.Name, pi.Status.String())
		}
	}

	return nil
}

func (sc *SchedulerCache) NewTaskInfo(pod *v1.Pod) (*schedulingapi.TaskInfo, error) {
	taskInfo := schedulingapi.NewTaskInfo(pod)
	// Update BestEffort because the InitResreq maybe changes
	taskInfo.BestEffort = taskInfo.InitResreq.IsEmpty()
	return taskInfo, nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addPod(pod *v1.Pod) error {
	pi, exist := sc.GetTaskInfo(schedulingapi.TaskID(pod.UID))
	if !exist {
		// If it doesn't exist, then new a taskinfo, especially for those pods which are not scheduled by agent-scheduler or in restarting scenario
		pi = schedulingapi.NewTaskInfo(pod)
	}

	return sc.addTask(pi)
}

func (sc *SchedulerCache) syncTask(oldTask *schedulingapi.TaskInfo) error {
	newPod, err := sc.kubeClient.CoreV1().Pods(oldTask.Namespace).Get(context.TODO(), oldTask.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			sc.Mutex.Lock()
			defer sc.Mutex.Unlock()
			err := sc.deleteTask(oldTask)
			if err != nil {
				klog.Errorf("Failed to delete Pod <%v/%v> and remove from cache: %s", oldTask.Namespace, oldTask.Name, err.Error())
				return err
			}
			klog.V(3).Infof("Pod <%v/%v> was deleted, removed from cache.", oldTask.Namespace, oldTask.Name)

			return nil
		}
		return fmt.Errorf("failed to get Pod <%v/%v>: err %v", oldTask.Namespace, oldTask.Name, err)
	}

	newTask, err := sc.NewTaskInfo(newPod)
	if err != nil {
		return fmt.Errorf("failed to generate taskInfo of pod(%s), error: %v", newPod.Name, err)
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	return sc.updateTask(oldTask, newTask)
}

func (sc *SchedulerCache) updateTask(oldTask, newTask *schedulingapi.TaskInfo) error {
	if err := sc.deleteTask(oldTask); err != nil {
		klog.Warningf("Failed to delete task from cache: %v", err)
	}

	return sc.addTask(newTask)
}

// Check the pod allocated status in cache
func (sc *SchedulerCache) allocatedPodInCache(pod *v1.Pod) bool {
	// TODO: check the pod allocated status in cache
	pi := schedulingapi.NewTaskInfo(pod)
	return schedulingapi.AllocatedStatus(pi.Status)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	//TODO ignore the update event if pod is allocated in cache but not present in NodeName
	if sc.allocatedPodInCache(newPod) && newPod.Spec.NodeName == "" {
		klog.V(4).Infof("Pod <%s/%v> already in cache with allocated status, ignore the update event", newPod.Namespace, newPod.Name)
		return nil
	}

	if err := sc.deletePod(oldPod); err != nil {
		return err
	}
	//when delete pod, the ownerreference of pod will be set nil, just as orphan pod
	if len(utils.GetController(newPod)) == 0 {
		newPod.OwnerReferences = oldPod.OwnerReferences
	}
	return sc.addPod(newPod)
}

func (sc *SchedulerCache) deleteTask(ti *schedulingapi.TaskInfo) error {
	// TODO need to refactoring
	var nodeErr error
	if len(ti.NodeName) != 0 {
		// We don't need to delete tasks from the Nodes cache that are already terminated.
		// These tasks will be cleaned up during the UpdatePod -> updatePod -> deletePod -> deleteTask sequence,
		// and will not be re-added with node.AddTask when updatePod -> addPod -> addTask occurs.
		// This covers the case when taskStatus changes from Releasing to Failed or Succeeded.
		if !isTerminated(ti.Status) {
			node := sc.Nodes[ti.NodeName]
			if node != nil {
				nodeErr = node.RemoveTask(ti)
			}
		}
	}

	if nodeErr != nil {
		return schedulingapi.MergeErrors(nodeErr)
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePod(pod *v1.Pod) error {
	pi, exist := sc.GetTaskInfo(schedulingapi.TaskID(pod.UID))
	if !exist {
		// If it doesn't exist, then new a taskinfo, especially for those pods which are not scheduled by agent-scheduler or in restarting scenario
		pi = schedulingapi.NewTaskInfo(pod)
	}
	if err := sc.deleteTask(pi); err != nil {
		klog.Errorf("Failed to delete task from cache: %v", err)
		return err
	}

	return nil
}

// AddPodToCache add pod to scheduler cache
func (sc *SchedulerCache) AddPodToCache(obj interface{}) {
	_, pod, err := schedutil.As[*v1.Pod](nil, obj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err = sc.addPod(pod)
	if err != nil {
		klog.Errorf("Failed to add pod <%s/%s> into cache: %v",
			pod.Namespace, pod.Name, err)
		return
	}
	klog.V(3).Infof("Added pod <%s/%v> into cache.", pod.Namespace, pod.Name)

	// Currently we still use AssignedPodAdded and only care about pod affinity and pod topology spread,
	// directly using MoveAllToActiveOrBackoffQueue may lead to a decrease in throughput.
	sc.schedulingQueue.AssignedPodAdded(klog.Background(), pod)
}

// UpdatePodInCache update pod to scheduler cache
func (sc *SchedulerCache) UpdatePodInCache(oldObj, newObj interface{}) {
	oldPod, newPod, err := schedutil.As[*v1.Pod](oldObj, newObj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err = sc.updatePod(oldPod, newPod)
	if err != nil {
		klog.Errorf("Failed to update pod %v in cache: %v", oldPod.Name, err)
		return
	}
	klog.V(4).Infof("Updated pod <%s/%v> in cache.", oldPod.Namespace, oldPod.Name)

	events := framework.PodSchedulingPropertiesChange(newPod, oldPod)
	for _, evt := range events {
		// Currently we still use AssignedPodUpdated and only care about pod affinity and pod topology spread,
		// directly using MoveAllToActiveOrBackoffQueue may lead to a decrease in throughput.
		sc.schedulingQueue.AssignedPodUpdated(klog.Background(), oldPod, newPod, evt)
	}
}

// DeletePodFromCache delete pod from scheduler cache
func (sc *SchedulerCache) DeletePodFromCache(obj interface{}) {
	_, pod, err := schedutil.As[*v1.Pod](nil, obj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err = sc.deletePod(pod)
	if err != nil {
		klog.Errorf("Failed to delete pod %v from cache: %v", pod.Name, err)
		return
	}

	klog.V(3).Infof("Deleted pod <%s/%v> from cache.", pod.Namespace, pod.Name)

	// If QueueingHint is not enabled, the scheduler will move all unschedulable pods to backoffQ/activeQ when a pod delete event occurs,
	// therefore we can add queueing hint support in the future.
	sc.schedulingQueue.MoveAllToActiveOrBackoffQueue(klog.Background(), framework.EventAssignedPodDelete, pod, nil, nil)
}

func (sc *SchedulerCache) AddPodToSchedulingQueue(obj interface{}) {
	_, pod, err := schedutil.As[*v1.Pod](nil, obj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	ti := schedulingapi.NewTaskInfo(pod)
	sc.AddTaskInfo(ti)

	sc.schedulingQueue.Add(klog.Background(), pod)
}

func (sc *SchedulerCache) UpdatePodInSchedulingQueue(oldObj, newObj interface{}) {
	oldPod, newPod, err := schedutil.As[*v1.Pod](oldObj, newObj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}

	// For update scenario, we simply override the old taskInfo and let gc reclaim the old taskInfo
	ti := schedulingapi.NewTaskInfo(newPod)
	sc.AddTaskInfo(ti)

	sc.schedulingQueue.Update(klog.Background(), oldPod, newPod)
}

func (sc *SchedulerCache) DeletePodFromSchedulingQueue(obj interface{}) {
	_, pod, err := schedutil.As[*v1.Pod](nil, obj)
	if err != nil {
		klog.Errorf("Failed to convert objects to *v1.Pod: %v", err)
		return
	}

	tid := schedulingapi.TaskID(pod.UID)
	if _, exist := sc.GetTaskInfo(tid); !exist {
		klog.Errorf("Failed to find task <%s/%s> in cache.", pod.Namespace, pod.Name)
		return
	}
	sc.DeleteTaskInfo(tid)

	sc.schedulingQueue.Delete(pod)
}

// addNodeImageStates adds states of the images on given node to the given nodeInfo and update the imageStates in
// scheduler cache. This function assumes the lock to scheduler cache has been acquired.
func (sc *SchedulerCache) addNodeImageStates(node *v1.Node, nodeInfo *schedulingapi.NodeInfo) {
	newSum := make(map[string]*fwk.ImageStateSummary)

	for _, image := range node.Status.Images {
		for _, name := range image.Names {
			// update the entry in imageStates
			state, ok := sc.imageStates[name]
			if !ok {
				state = &imageState{
					size:  image.SizeBytes,
					nodes: sets.New(node.Name),
				}
				sc.imageStates[name] = state
			} else {
				state.nodes.Insert(node.Name)
			}
			// create the imageStateSummary for this image
			if _, ok := newSum[name]; !ok {
				newSum[name] = sc.createImageStateSummary(state)
			}
		}
	}
	nodeInfo.ImageStates = newSum
}

// removeNodeImageStates removes the given node record from image entries having the node
// in imageStates cache. After the removal, if any image becomes free, i.e., the image
// is no longer available on any node, the image entry will be removed from imageStates.
func (sc *SchedulerCache) removeNodeImageStates(node string) {
	for image, state := range sc.imageStates {
		state.nodes.Delete(node)
		if len(state.nodes) == 0 {
			// Remove the unused image to make sure the length of
			// imageStates represents the total number of different
			// images on all nodes
			delete(sc.imageStates, image)
		}
	}
}

// AddOrUpdateNode adds or updates node info in cache.
func (sc *SchedulerCache) AddOrUpdateNode(node *v1.Node) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	var oldNode *v1.Node
	if n, ok := sc.Nodes[node.Name]; ok {
		oldNode = n.Node
	}

	if sc.Nodes[node.Name] != nil {
		sc.Nodes[node.Name].SetNode(node)
		sc.removeNodeImageStates(node.Name)
		// TODO The generation needs to be optimized to increment globally by one.
		sc.Nodes[node.Name].Generation++
		klog.V(5).Infof("Node %s added/updated, generation incremented to %d", node.Name, sc.Nodes[node.Name].Generation)
	} else {
		sc.Nodes[node.Name] = schedulingapi.NewNodeInfo(node)
	}
	sc.addNodeImageStates(node, sc.Nodes[node.Name])

	var nodeExisted bool
	for _, name := range sc.NodeList {
		if name == node.Name {
			nodeExisted = true
			break
		}
	}
	if !nodeExisted {
		sc.NodeList = append(sc.NodeList, node.Name)
	}

	if oldNode != nil {
		events := framework.NodeSchedulingPropertiesChange(node, oldNode)
		for _, evt := range events {
			// TODO: We may need to add a preCheckForNode function to filter nodes before moving pods
			sc.schedulingQueue.MoveAllToActiveOrBackoffQueue(klog.Background(), evt, oldNode, node, nil)
		}
	} else {
		evt := fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}
		// TODO: We may need to add a preCheckForNode function to filter nodes before moving pods
		sc.schedulingQueue.MoveAllToActiveOrBackoffQueue(klog.Background(), evt, nil, node, nil)
	}

	return nil
}

// RemoveNode removes node info from cache
func (sc *SchedulerCache) RemoveNode(nodeName string) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	for i, name := range sc.NodeList {
		if name == nodeName {
			sc.NodeList = append(sc.NodeList[:i], sc.NodeList[i+1:]...)
			break
		}
	}
	sc.removeNodeImageStates(nodeName)
	sc.ConflictAwareBinder.RemoveBindRecord(nodeName)

	if _, ok := sc.Nodes[nodeName]; !ok {
		return fmt.Errorf("node <%s> does not exist", nodeName)
	}

	node := sc.Nodes[nodeName].Node

	numaInfo := sc.Nodes[nodeName].NumaInfo
	if numaInfo != nil {
		klog.V(3).Infof("delete numatopo <%s/%s>", numaInfo.Namespace, numaInfo.Name)
		err := sc.vcClient.NodeinfoV1alpha1().Numatopologies().Delete(context.TODO(), numaInfo.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("delete numatopo <%s/%s> failed.", numaInfo.Namespace, numaInfo.Name)
		}
	}
	delete(sc.Nodes, nodeName)

	evt := fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Delete}
	sc.schedulingQueue.MoveAllToActiveOrBackoffQueue(klog.Background(), evt, node, nil, nil)

	return nil
}

// AddNode add node to scheduler cache
func (sc *SchedulerCache) AddNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert to *v1.Node: %v", obj)
		return
	}
	sc.nodeQueue.Add(node.Name)
}

// UpdateNode update node to scheduler cache
func (sc *SchedulerCache) UpdateNode(oldObj, newObj interface{}) {
	_, ok := oldObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert newObj to *v1.Node: %v", newObj)
		return
	}
	sc.nodeQueue.Add(newNode.Name)
}

// DeleteNode delete node from scheduler cache
func (sc *SchedulerCache) DeleteNode(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			klog.Errorf("Cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *v1.Node: %v", t)
		return
	}
	sc.nodeQueue.Add(node.Name)
}

func (sc *SchedulerCache) SyncNode(nodeName string) error {
	node, err := sc.nodeInformer.Lister().Get(nodeName)
	if err != nil {
		if errors.IsNotFound(err) {
			deleteErr := sc.RemoveNode(nodeName)
			if deleteErr != nil {
				klog.Errorf("Failed to delete node <%s> and remove from cache: %s", nodeName, deleteErr.Error())
				return deleteErr
			}

			klog.V(3).Infof("Node <%s> was deleted, removed from cache.", nodeName)
			return nil
		}
		klog.Errorf("Failed to get node %s, error: %v", nodeName, err)
		return err
	}

	if !sc.nodeCanAddCache(node) {
		return nil
	}
	nodeCopy := node.DeepCopy()
	return sc.AddOrUpdateNode(nodeCopy)
}

func (sc *SchedulerCache) nodeCanAddCache(node *v1.Node) bool {
	if len(sc.nodeSelectorLabels) == 0 {
		return true
	}
	for labelName, labelValue := range node.Labels {
		key := labelName + ":" + labelValue
		if _, ok := sc.nodeSelectorLabels[key]; ok {
			return true
		}
	}
	klog.Infof("node %s ignore add/update/delete into schedulerCache", node.Name)
	return false
}
