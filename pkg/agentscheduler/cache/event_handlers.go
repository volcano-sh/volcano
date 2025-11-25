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
	"math"
	"slices"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/apis/pkg/apis/utils"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

var DefaultAttachableVolumeQuantity int64 = math.MaxInt32

func isTerminated(status schedulingapi.TaskStatus) bool {
	return status == schedulingapi.Succeeded || status == schedulingapi.Failed
}

// getOrCreateJob will return corresponding Job for pi if it exists, or it will create a Job and return it if
// pi.Pod.Spec.SchedulerName is same as volcano scheduler's name, otherwise it will return nil.
func (sc *SchedulerCache) getOrCreateJob(pi *schedulingapi.TaskInfo) *schedulingapi.JobInfo {
	if len(pi.Job) == 0 {
		if !slices.Contains(sc.schedulerNames, pi.Pod.Spec.SchedulerName) {
			klog.V(4).Infof("Pod %s/%s is not scheduled by %s, skip creating PodGroup and Job for it in cache.",
				pi.Pod.Namespace, pi.Pod.Name, strings.Join(sc.schedulerNames, ","))
		}
		return nil
	}

	//if _, found := sc.Jobs[pi.Job]; !found {
	//	sc.Jobs[pi.Job] = schedulingapi.NewJobInfo(pi.Job)
	//}
	//
	return nil
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
	pi, err := sc.NewTaskInfo(pod)
	if err != nil {
		klog.Errorf("generate taskInfo for pod(%s) failed: %v", pod.Name, err)
		sc.resyncTask(pi)
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
	var nodeErr error

	//if len(ti.Job) != 0 {
	//	if job, found := sc.Jobs[ti.Job]; found {
	//		jobErr = job.DeleteTaskInfo(ti)
	//	} else {
	//		klog.Warningf("Failed to find Job <%v> for Task <%v/%v> in cache.", ti.Job, ti.Namespace, ti.Name)
	//	}
	//} else {
	//	klog.V(4).Infof("Task <%s/%s> has null jobID in cache.", ti.Namespace, ti.Name)
	//}

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
	pi := schedulingapi.NewTaskInfo(pod)
	//
	//// Delete the Task in cache to handle Binding status.
	//task := pi
	//if job, found := sc.Jobs[pi.Job]; found {
	//	if t, found := job.Tasks[pi.UID]; found {
	//		task = t
	//	}
	//}
	_, task, err := sc.findJobAndTask(pi)
	if err = sc.deleteTask(task); err != nil {
		klog.Warningf("Failed to delete task from cache: %v", err)
	}

	//// If job was terminated, delete it.
	//if job, found := sc.Jobs[pi.Job]; found && schedulingapi.JobTerminated(job) {
	//	sc.deleteJob(job)
	//}

	return nil
}

// AddPod add pod to scheduler cache
func (sc *SchedulerCache) AddPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Cannot convert to *v1.Pod: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.addPod(pod)
	if err != nil {
		klog.Errorf("Failed to add pod <%s/%s> into cache: %v",
			pod.Namespace, pod.Name, err)
		return
	}
	klog.V(3).Infof("Added pod <%s/%v> into cache.", pod.Namespace, pod.Name)
}

// UpdatePod update pod to scheduler cache
func (sc *SchedulerCache) UpdatePod(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *v1.Pod: %v", oldObj)
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		klog.Errorf("Cannot convert newObj to *v1.Pod: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.updatePod(oldPod, newPod)
	if err != nil {
		klog.Errorf("Failed to update pod %v in cache: %v", oldPod.Name, err)
		return
	}
	klog.V(4).Infof("Updated pod <%s/%v> in cache.", oldPod.Namespace, oldPod.Name)
}

// DeletePod delete pod from scheduler cache
func (sc *SchedulerCache) DeletePod(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			klog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deletePod(pod)
	if err != nil {
		klog.Errorf("Failed to delete pod %v from cache: %v", pod.Name, err)
		return
	}

	klog.V(3).Infof("Deleted pod <%s/%v> from cache.", pod.Namespace, pod.Name)
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

	if sc.Nodes[node.Name] != nil {
		sc.Nodes[node.Name].SetNode(node)
		sc.removeNodeImageStates(node.Name)
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

	if _, ok := sc.Nodes[nodeName]; !ok {
		return fmt.Errorf("node <%s> does not exist", nodeName)
	}

	numaInfo := sc.Nodes[nodeName].NumaInfo
	if numaInfo != nil {
		klog.V(3).Infof("delete numatopo <%s/%s>", numaInfo.Namespace, numaInfo.Name)
		err := sc.vcClient.NodeinfoV1alpha1().Numatopologies().Delete(context.TODO(), numaInfo.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("delete numatopo <%s/%s> failed.", numaInfo.Namespace, numaInfo.Name)
		}
	}
	delete(sc.Nodes, nodeName)
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
	if !responsibleForNode(node.Name, sc.schedulerPodName, sc.c) {
		return false
	}
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

func (sc *SchedulerCache) updateResourceQuota(quota *v1.ResourceQuota) {
	collection, ok := sc.NamespaceCollection[quota.Namespace]
	if !ok {
		collection = schedulingapi.NewNamespaceCollection(quota.Namespace)
		sc.NamespaceCollection[quota.Namespace] = collection
	}

	collection.Update(quota)
}

func (sc *SchedulerCache) deleteResourceQuota(quota *v1.ResourceQuota) {
	collection, ok := sc.NamespaceCollection[quota.Namespace]
	if !ok {
		return
	}

	collection.Delete(quota)
}

// DeleteResourceQuota delete ResourceQuota from the scheduler cache
func (sc *SchedulerCache) DeleteResourceQuota(obj interface{}) {
	var r *v1.ResourceQuota
	switch t := obj.(type) {
	case *v1.ResourceQuota:
		r = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		r, ok = t.Obj.(*v1.ResourceQuota)
		if !ok {
			klog.Errorf("Cannot convert to *v1.ResourceQuota: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *v1.ResourceQuota: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	klog.V(3).Infof("Delete ResourceQuota <%s/%v> in cache", r.Namespace, r.Name)
	sc.deleteResourceQuota(r)
}

// UpdateResourceQuota update ResourceQuota to scheduler cache
func (sc *SchedulerCache) UpdateResourceQuota(oldObj, newObj interface{}) {
	newR, ok := newObj.(*v1.ResourceQuota)
	if !ok {
		klog.Errorf("Cannot convert newObj to *v1.ResourceQuota: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	klog.V(3).Infof("Update ResourceQuota <%s/%v> in cache, with spec: %v.", newR.Namespace, newR.Name, newR.Spec.Hard)
	sc.updateResourceQuota(newR)
}

// AddResourceQuota add ResourceQuota to scheduler cache
func (sc *SchedulerCache) AddResourceQuota(obj interface{}) {
	var r *v1.ResourceQuota
	switch t := obj.(type) {
	case *v1.ResourceQuota:
		r = t
	default:
		klog.Errorf("Cannot convert to *v1.ResourceQuota: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	klog.V(3).Infof("Add ResourceQuota <%s/%v> in cache, with spec: %v.", r.Namespace, r.Name, r.Spec.Hard)
	sc.updateResourceQuota(r)
}

func getNumaInfo(srcInfo *nodeinfov1alpha1.Numatopology) *schedulingapi.NumatopoInfo {
	numaInfo := &schedulingapi.NumatopoInfo{
		Namespace:   srcInfo.Namespace,
		Name:        srcInfo.Name,
		Policies:    make(map[nodeinfov1alpha1.PolicyName]string),
		NumaResMap:  make(map[string]*schedulingapi.ResourceInfo),
		CPUDetail:   topology.CPUDetails{},
		ResReserved: make(v1.ResourceList),
	}

	policies := srcInfo.Spec.Policies
	for name, policy := range policies {
		numaInfo.Policies[name] = policy
	}

	numaResMap := srcInfo.Spec.NumaResMap
	for name, resInfo := range numaResMap {
		tmp := schedulingapi.ResourceInfo{}
		tmp.Capacity = resInfo.Capacity
		allocatable, err := cpuset.Parse(resInfo.Allocatable)
		if err != nil {
			klog.ErrorS(err, "Failed to parse input as CPUSet", resInfo.Allocatable)
		}
		tmp.Allocatable = allocatable
		numaInfo.NumaResMap[name] = &tmp
	}

	cpuDetail := srcInfo.Spec.CPUDetail
	for key, detail := range cpuDetail {
		cpuID, _ := strconv.Atoi(key)
		numaInfo.CPUDetail[cpuID] = topology.CPUInfo{
			NUMANodeID: detail.NUMANodeID,
			SocketID:   detail.SocketID,
			CoreID:     detail.CoreID,
		}
	}

	resReserved, err := schedulingapi.ParseResourceList(srcInfo.Spec.ResReserved)
	if err != nil {
		klog.Errorf("ParseResourceList failed, err=%v", err)
	} else {
		numaInfo.ResReserved = resReserved
	}

	return numaInfo
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addNumaInfo(info *nodeinfov1alpha1.Numatopology) error {
	if sc.Nodes[info.Name] == nil {
		sc.Nodes[info.Name] = schedulingapi.NewNodeInfo(nil)
		sc.Nodes[info.Name].Name = info.Name
	}

	if sc.Nodes[info.Name].NumaInfo == nil {
		sc.Nodes[info.Name].NumaInfo = getNumaInfo(info)
		sc.Nodes[info.Name].NumaChgFlag = schedulingapi.NumaInfoMoreFlag
	} else {
		newLocalInfo := getNumaInfo(info)
		if sc.Nodes[info.Name].NumaInfo.Compare(newLocalInfo) {
			sc.Nodes[info.Name].NumaChgFlag = schedulingapi.NumaInfoMoreFlag
		} else {
			sc.Nodes[info.Name].NumaChgFlag = schedulingapi.NumaInfoLessFlag
		}

		sc.Nodes[info.Name].NumaInfo = newLocalInfo
	}

	for resName, NumaResInfo := range sc.Nodes[info.Name].NumaInfo.NumaResMap {
		klog.V(3).Infof("resource %s Allocatable %v on node[%s] into cache", resName, NumaResInfo, info.Name)
	}

	klog.V(3).Infof("Policies %v on node[%s] into cache, change= %v",
		sc.Nodes[info.Name].NumaInfo.Policies, info.Name, sc.Nodes[info.Name].NumaChgFlag)
	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deleteNumaInfo(info *nodeinfov1alpha1.Numatopology) {
	if sc.Nodes[info.Name] != nil {
		sc.Nodes[info.Name].NumaInfo = nil
		sc.Nodes[info.Name].NumaChgFlag = schedulingapi.NumaInfoResetFlag
		klog.V(3).Infof("delete numainfo in cache for node<%s>", info.Name)
	}
}

// AddNumaInfoV1alpha1 add numa information to scheduler cache
func (sc *SchedulerCache) AddNumaInfoV1alpha1(obj interface{}) {
	ss, ok := obj.(*nodeinfov1alpha1.Numatopology)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *nodeinfov1alpha1.Numatopology: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	sc.addNumaInfo(ss)
}

// UpdateNumaInfoV1alpha1 update numa information to scheduler cache
func (sc *SchedulerCache) UpdateNumaInfoV1alpha1(oldObj, newObj interface{}) {
	ss, ok := newObj.(*nodeinfov1alpha1.Numatopology)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *nodeinfov1alpha1.Numatopology: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.addNumaInfo(ss)
	klog.V(3).Infof("update numaInfo<%s> in cache, with spec: Policy: %v, resMap: %v", ss.Name, ss.Spec.Policies, ss.Spec.NumaResMap)
}

// DeleteNumaInfoV1alpha1 delete numa information from scheduler cache
func (sc *SchedulerCache) DeleteNumaInfoV1alpha1(obj interface{}) {
	var ss *nodeinfov1alpha1.Numatopology
	switch t := obj.(type) {
	case *nodeinfov1alpha1.Numatopology:
		ss = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		ss, ok = t.Obj.(*nodeinfov1alpha1.Numatopology)
		if !ok {
			klog.Errorf("Cannot convert to Numatopo: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to Numatopo: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	sc.deleteNumaInfo(ss)
	klog.V(3).Infof("Delete numaInfo<%s> from cache, with spec: Policy: %v, resMap: %v", ss.Name, ss.Spec.Policies, ss.Spec.NumaResMap)
}
