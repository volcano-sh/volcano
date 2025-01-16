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
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync/atomic"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	sv1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/component-helpers/storage/ephemeral"
	storagehelpers "k8s.io/component-helpers/storage/volume"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/scheme"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/apis/pkg/apis/utils"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
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
			klog.V(4).Infof("Pod %s/%s will not scheduled by %#v, skip creating PodGroup and Job for it",
				pi.Pod.Namespace, pi.Pod.Name, sc.schedulerNames)
		}
		return nil
	}

	if _, found := sc.Jobs[pi.Job]; !found {
		sc.Jobs[pi.Job] = schedulingapi.NewJobInfo(pi.Job)
	}

	return sc.Jobs[pi.Job]
}

// addPodCSIVolumesToTask counts the csi volumes used by task
// @Lily922 TODO: support counting shared volumes. Currently, if two different pods use the same attachable volume
// and scheduled on the same nodes the volume will be count twice, but actually only use one attachable limit resource
func (sc *SchedulerCache) addPodCSIVolumesToTask(pi *schedulingapi.TaskInfo) error {
	volumes, err := sc.getPodCSIVolumes(pi.Pod)
	if err != nil {
		klog.Errorf("got pod csi attachment persistent volumes count error: %s", err.Error())
		return err
	}

	for key, count := range volumes {
		pi.Resreq.AddScalar(key, float64(count))
	}
	return nil
}

func (sc *SchedulerCache) getPodCSIVolumes(pod *v1.Pod) (map[v1.ResourceName]int64, error) {
	volumes := make(map[v1.ResourceName]int64)
	for _, vol := range pod.Spec.Volumes {
		pvcName := ""
		isEphemeral := false
		switch {
		case vol.PersistentVolumeClaim != nil:
			// Normal CSI volume can only be used through PVC
			pvcName = vol.PersistentVolumeClaim.ClaimName
		case vol.Ephemeral != nil:
			// Generic ephemeral inline volumes also use a PVC,
			// just with a computed name and certain ownership.
			// That is checked below once the pvc object is
			// retrieved.
			pvcName = ephemeral.VolumeClaimName(pod, &vol)
			isEphemeral = true
		default:
			// @Lily922 TODO: support Inline volumes.
			// Inline volume is not supported now
			continue
		}
		if pvcName == "" {
			return volumes, fmt.Errorf("PersistentVolumeClaim had no name")
		}

		pvc, err := sc.pvcInformer.Lister().PersistentVolumeClaims(pod.Namespace).Get(pvcName)
		if err != nil {
			// The PVC is required to proceed with
			// scheduling of a new pod because it cannot
			// run without it. Bail out immediately.
			return volumes, fmt.Errorf("looking up PVC %s/%s: %v", pod.Namespace, pvcName, err)
		}
		// The PVC for an ephemeral volume must be owned by the pod.
		if isEphemeral {
			if err := ephemeral.VolumeIsForPod(pod, pvc); err != nil {
				return volumes, err
			}
		}

		driverName := sc.getCSIDriverInfo(pvc)
		if sc.isIgnoredProvisioner(driverName) {
			klog.V(5).InfoS("Provisioner ignored, skip count pod pvc num", "driverName", driverName)
			continue
		}
		if driverName == "" {
			klog.V(5).InfoS("Could not find a CSI driver name for pvc(%s/%s), not counting volume", pvc.Namespace, pvc.Name)
			continue
		}

		// Count all csi volumes in cache, because the storage may change from unattachable volumes to attachable volumes after
		// the cache set up, in this case it is very difficult to refresh all task caches.
		// For unattachable volume, set the limits number to a very large value, in this way, scheduling will never
		// be limited due to insufficient quantity of it.
		k := v1.ResourceName(volumeutil.GetCSIAttachLimitKey(driverName))
		if _, ok := volumes[k]; !ok {
			volumes[k] = 1
		} else {
			volumes[k] += 1
		}
	}
	return volumes, nil
}

func (sc *SchedulerCache) isIgnoredProvisioner(driverName string) bool {
	return sc.IgnoredCSIProvisioners.Has(driverName)
}

func (sc *SchedulerCache) getCSIDriverInfo(pvc *v1.PersistentVolumeClaim) string {
	pvName := pvc.Spec.VolumeName

	if pvName == "" {
		klog.V(5).Infof("PV had no name for pvc <%s>", klog.KObj(pvc))
		return sc.getCSIDriverInfoFromSC(pvc)
	}

	pv, err := sc.pvInformer.Lister().Get(pvName)
	if err != nil {
		klog.V(5).InfoS("Failed to get pv <%s> for pvc <%s>: %v", klog.KRef("", pvName), klog.KObj(pvc), err)
		// If we can't fetch PV associated with PVC, may be it got deleted
		// or PVC was prebound to a PVC that hasn't been created yet.
		// fallback to using StorageClass for volume counting
		return sc.getCSIDriverInfoFromSC(pvc)
	}

	csiSource := pv.Spec.PersistentVolumeSource.CSI
	if csiSource == nil {
		// @Lily922 TODO: support non-CSI volumes and migrating pvc
		// CSIMigration is not supported now.
		klog.Warningf("Not support non-csi pvc.")
		return ""
	}

	return csiSource.Driver
}

// getCSIDriverInfoFromSC get the name of the csi driver through the sc of the pvc.
func (sc *SchedulerCache) getCSIDriverInfoFromSC(pvc *v1.PersistentVolumeClaim) string {
	scName := storagehelpers.GetPersistentVolumeClaimClass(pvc)

	// If StorageClass is not set or not found, then PVC must be using immediate binding mode
	// and hence it must be bound before scheduling. So it is safe to not count it.
	if scName == "" {
		klog.V(3).Infof("PVC <%s> has no StorageClass", klog.KObj(pvc))
		return ""
	}

	storageClass, err := sc.scInformer.Lister().Get(scName)
	if err != nil {
		klog.Errorf("Failed to get StorageClass for pvc <%s>: %v", klog.KObj(pvc), err)
		return ""
	}

	if driverName, ok := storageClass.Parameters["csi.storage.k8s.io/csi-driver-name"]; ok {
		return driverName
	}
	return storageClass.Provisioner
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

	job := sc.getOrCreateJob(pi)
	if job != nil {
		job.AddTaskInfo(pi)
	}

	return nil
}

func (sc *SchedulerCache) NewTaskInfo(pod *v1.Pod) (*schedulingapi.TaskInfo, error) {
	taskInfo := schedulingapi.NewTaskInfo(pod)
	if err := sc.addPodCSIVolumesToTask(taskInfo); err != nil {
		return taskInfo, err
	}
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
		klog.Warningf("Failed to delete task: %v", err)
	}

	return sc.addTask(newTask)
}

// Check the pod allocated status in cache
func (sc *SchedulerCache) allocatedPodInCache(pod *v1.Pod) bool {
	pi := schedulingapi.NewTaskInfo(pod)

	if job, found := sc.Jobs[pi.Job]; found {
		if t, found := job.Tasks[pi.UID]; found {
			return schedulingapi.AllocatedStatus(t.Status)
		}
	}

	return false
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	//ignore the update event if pod is allocated in cache but not present in NodeName
	if sc.allocatedPodInCache(newPod) && newPod.Spec.NodeName == "" {
		klog.V(4).Infof("Pod <%s/%v> already in cache with allocated status, ignore the update event", newPod.Namespace, newPod.Name)
		return nil
	}

	if err := sc.deletePod(oldPod); err != nil {
		return err
	}
	//when delete pod, the ownerreference of pod will be set nil,just as orphan pod
	if len(utils.GetController(newPod)) == 0 {
		newPod.OwnerReferences = oldPod.OwnerReferences
	}
	return sc.addPod(newPod)
}

func (sc *SchedulerCache) deleteTask(ti *schedulingapi.TaskInfo) error {
	var jobErr, nodeErr, numaErr error

	if len(ti.Job) != 0 {
		if job, found := sc.Jobs[ti.Job]; found {
			jobErr = job.DeleteTaskInfo(ti)
		} else {
			klog.Warningf("Failed to find Job <%v> for Task <%v/%v>", ti.Job, ti.Namespace, ti.Name)
		}
	} else { // should not run into here; record error so that easy to debug
		jobErr = fmt.Errorf("task %s/%s has null jobID", ti.Namespace, ti.Name)
	}

	if len(ti.NodeName) != 0 {
		node := sc.Nodes[ti.NodeName]
		if node != nil {
			nodeErr = node.RemoveTask(ti)
		}
	}

	if jobErr != nil || nodeErr != nil {
		return schedulingapi.MergeErrors(jobErr, nodeErr, numaErr)
	}

	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePod(pod *v1.Pod) error {
	pi := schedulingapi.NewTaskInfo(pod)

	// Delete the Task in cache to handle Binding status.
	task := pi
	if job, found := sc.Jobs[pi.Job]; found {
		if t, found := job.Tasks[pi.UID]; found {
			task = t
		}
	}
	if err := sc.deleteTask(task); err != nil {
		klog.Warningf("Failed to delete task: %v", err)
	}

	// If job was terminated, delete it.
	if job, found := sc.Jobs[pi.Job]; found && schedulingapi.JobTerminated(job) {
		sc.deleteJob(job)
	}

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

	atomic.AddInt32(&sc.PodNum, 1)
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

	atomic.AddInt32(&sc.PodNum, -1)
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
	newSum := make(map[string]*framework.ImageStateSummary)

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
	csiNode, err := sc.csiNodeInformer.Lister().Get(nodeName)
	if err == nil {
		sc.setCSIResourceOnNode(csiNode, nodeCopy)
	} else if !errors.IsNotFound(err) {
		return err
	}
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

func (sc *SchedulerCache) AddOrUpdateCSINode(obj interface{}) {
	csiNode, ok := obj.(*sv1.CSINode)
	if !ok {
		return
	}

	csiNodeStatus := &schedulingapi.CSINodeStatusInfo{
		CSINodeName:  csiNode.Name,
		DriverStatus: make(map[string]bool),
	}
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	for i := range csiNode.Spec.Drivers {
		d := csiNode.Spec.Drivers[i]
		csiNodeStatus.DriverStatus[d.Name] = d.Allocatable != nil && d.Allocatable.Count != nil
	}
	sc.CSINodesStatus[csiNode.Name] = csiNodeStatus
	sc.nodeQueue.Add(csiNode.Name)
}

func (sc *SchedulerCache) UpdateCSINode(oldObj, newObj interface{}) {
	oldCSINode, ok := oldObj.(*sv1.CSINode)
	if !ok {
		return
	}
	newCSINode, ok := newObj.(*sv1.CSINode)
	if !ok {
		return
	}
	if equality.Semantic.DeepEqual(oldCSINode.Spec, newCSINode.Spec) {
		return
	}
	sc.AddOrUpdateCSINode(newObj)
}

func (sc *SchedulerCache) DeleteCSINode(obj interface{}) {
	var csiNode *sv1.CSINode
	switch t := obj.(type) {
	case *sv1.CSINode:
		csiNode = obj.(*sv1.CSINode)
	case cache.DeletedFinalStateUnknown:
		var ok bool
		csiNode, ok = t.Obj.(*sv1.CSINode)
		if !ok {
			klog.Errorf("Cannot convert to *sv1.CSINode: %v", obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *sv1.CSINode: %v", obj)
		return
	}

	sc.Mutex.Lock()
	delete(sc.CSINodesStatus, csiNode.Name)
	sc.Mutex.Unlock()
	sc.nodeQueue.Add(csiNode.Name)
}

func getJobID(pg *schedulingapi.PodGroup) schedulingapi.JobID {
	return schedulingapi.JobID(fmt.Sprintf("%s/%s", pg.Namespace, pg.Name))
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) setPodGroup(ss *schedulingapi.PodGroup) error {
	job := getJobID(ss)
	if _, found := sc.Jobs[job]; !found {
		sc.Jobs[job] = schedulingapi.NewJobInfo(job)
	}

	sc.Jobs[job].SetPodGroup(ss)

	// TODO(k82cn): set default queue in admission.
	if len(ss.Spec.Queue) == 0 {
		sc.Jobs[job].Queue = schedulingapi.QueueID(sc.defaultQueue)
	}

	metrics.UpdateE2eSchedulingStartTimeByJob(sc.Jobs[job].Name, string(sc.Jobs[job].Queue), sc.Jobs[job].Namespace,
		sc.Jobs[job].CreationTimestamp.Time)
	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updatePodGroup(newPodGroup *schedulingapi.PodGroup) error {
	return sc.setPodGroup(newPodGroup)
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) deletePodGroup(id schedulingapi.JobID) error {
	job, found := sc.Jobs[id]
	if !found {
		return fmt.Errorf("can not found job %v", id)
	}

	// Unset SchedulingSpec
	job.UnsetPodGroup()

	sc.deleteJob(job)

	return nil
}

// AddPodGroupV1beta1 add podgroup to scheduler cache
func (sc *SchedulerCache) AddPodGroupV1beta1(obj interface{}) {
	ss, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		klog.Errorf("Cannot convert to *schedulingv1beta1.PodGroup: %v", obj)
		return
	}

	podgroup := scheduling.PodGroup{}
	if err := scheme.Scheme.Convert(ss, &podgroup, nil); err != nil {
		klog.Errorf("Failed to convert podgroup from %T to %T", ss, podgroup)
		return
	}

	pg := &schedulingapi.PodGroup{PodGroup: podgroup, Version: schedulingapi.PodGroupVersionV1Beta1}
	klog.V(4).Infof("Add PodGroup(%s) into cache, spec(%#v)", ss.Name, ss.Spec)

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if err := sc.setPodGroup(pg); err != nil {
		klog.Errorf("Failed to add PodGroup %s into cache: %v", ss.Name, err)
		return
	}
}

// UpdatePodGroupV1beta1 add podgroup to scheduler cache
func (sc *SchedulerCache) UpdatePodGroupV1beta1(oldObj, newObj interface{}) {
	oldSS, ok := oldObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *schedulingv1beta1.SchedulingSpec: %v", oldObj)
		return
	}
	newSS, ok := newObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		klog.Errorf("Cannot convert newObj to *schedulingv1beta1.SchedulingSpec: %v", newObj)
		return
	}

	if oldSS.ResourceVersion == newSS.ResourceVersion {
		return
	}

	podgroup := scheduling.PodGroup{}
	if err := scheme.Scheme.Convert(newSS, &podgroup, nil); err != nil {
		klog.Errorf("Failed to convert podgroup from %T to %T", newSS, podgroup)
		return
	}

	pg := &schedulingapi.PodGroup{PodGroup: podgroup, Version: schedulingapi.PodGroupVersionV1Beta1}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if err := sc.updatePodGroup(pg); err != nil {
		klog.Errorf("Failed to update SchedulingSpec %s into cache: %v", pg.Name, err)
		return
	}
}

// DeletePodGroupV1beta1 delete podgroup from scheduler cache
func (sc *SchedulerCache) DeletePodGroupV1beta1(obj interface{}) {
	var ss *schedulingv1beta1.PodGroup
	switch t := obj.(type) {
	case *schedulingv1beta1.PodGroup:
		ss = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		ss, ok = t.Obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			klog.Errorf("Cannot convert to podgroup: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to podgroup: %v", t)
		return
	}

	jobID := schedulingapi.JobID(fmt.Sprintf("%s/%s", ss.Namespace, ss.Name))

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if err := sc.deletePodGroup(jobID); err != nil {
		klog.Errorf("Failed to delete podgroup %s from cache: %v", ss.Name, err)
		return
	}
}

// AddQueueV1beta1 add queue to scheduler cache
func (sc *SchedulerCache) AddQueueV1beta1(obj interface{}) {
	ss, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("Cannot convert to *schedulingv1beta1.Queue: %v", obj)
		return
	}

	queue := &scheduling.Queue{}
	if err := scheme.Scheme.Convert(ss, queue, nil); err != nil {
		klog.Errorf("Failed to convert queue from %T to %T", ss, queue)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	klog.V(4).Infof("Add Queue(%s) into cache, spec(%#v)", ss.Name, ss.Spec)
	sc.addQueue(queue)
}

// UpdateQueueV1beta1 update queue to scheduler cache
func (sc *SchedulerCache) UpdateQueueV1beta1(oldObj, newObj interface{}) {
	oldSS, ok := oldObj.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *schedulingv1beta1.Queue: %v", oldObj)
		return
	}
	newSS, ok := newObj.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("Cannot convert newObj to *schedulingv1beta1.Queue: %v", newObj)
		return
	}

	if oldSS.ResourceVersion == newSS.ResourceVersion {
		return
	}

	newQueue := &scheduling.Queue{}
	if err := scheme.Scheme.Convert(newSS, newQueue, nil); err != nil {
		klog.Errorf("Failed to convert queue from %T to %T", newSS, newQueue)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.updateQueue(newQueue)
}

// DeleteQueueV1beta1 delete queue from the scheduler cache
func (sc *SchedulerCache) DeleteQueueV1beta1(obj interface{}) {
	var ss *schedulingv1beta1.Queue
	switch t := obj.(type) {
	case *schedulingv1beta1.Queue:
		ss = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		ss, ok = t.Obj.(*schedulingv1beta1.Queue)
		if !ok {
			klog.Errorf("Cannot convert to *schedulingv1beta1.Queue: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *schedulingv1beta1.Queue: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.deleteQueue(schedulingapi.QueueID(ss.Name))
}

func (sc *SchedulerCache) addQueue(queue *scheduling.Queue) {
	qi := schedulingapi.NewQueueInfo(queue)
	sc.Queues[qi.UID] = qi
}

func (sc *SchedulerCache) updateQueue(queue *scheduling.Queue) {
	sc.addQueue(queue)
}

func (sc *SchedulerCache) deleteQueue(id schedulingapi.QueueID) {
	if queue, ok := sc.Queues[id]; ok {
		delete(sc.Queues, id)
		metrics.DeleteQueueMetrics(queue.Name)
	}
}

// DeletePriorityClass delete priorityclass from the scheduler cache
func (sc *SchedulerCache) DeletePriorityClass(obj interface{}) {
	var ss *schedulingv1.PriorityClass
	switch t := obj.(type) {
	case *schedulingv1.PriorityClass:
		ss = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		ss, ok = t.Obj.(*schedulingv1.PriorityClass)
		if !ok {
			klog.Errorf("Cannot convert to *schedulingv1.PriorityClass: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *schedulingv1.PriorityClass: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	sc.deletePriorityClass(ss)
}

// UpdatePriorityClass update priorityclass to scheduler cache
func (sc *SchedulerCache) UpdatePriorityClass(oldObj, newObj interface{}) {
	oldSS, ok := oldObj.(*schedulingv1.PriorityClass)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *schedulingv1.PriorityClass: %v", oldObj)

		return
	}

	newSS, ok := newObj.(*schedulingv1.PriorityClass)
	if !ok {
		klog.Errorf("Cannot convert newObj to *schedulingv1.PriorityClass: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	sc.deletePriorityClass(oldSS)
	sc.addPriorityClass(newSS)
}

// AddPriorityClass add priorityclass to scheduler cache
func (sc *SchedulerCache) AddPriorityClass(obj interface{}) {
	ss, ok := obj.(*schedulingv1.PriorityClass)
	if !ok {
		klog.Errorf("Cannot convert to *schedulingv1.PriorityClass: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	sc.addPriorityClass(ss)
}

func (sc *SchedulerCache) deletePriorityClass(pc *schedulingv1.PriorityClass) {
	if pc.GlobalDefault {
		sc.defaultPriorityClass = nil
		sc.defaultPriority = 0
	}

	delete(sc.PriorityClasses, pc.Name)
}

func (sc *SchedulerCache) addPriorityClass(pc *schedulingv1.PriorityClass) {
	if pc.GlobalDefault {
		if sc.defaultPriorityClass != nil {
			klog.Errorf("Updated default priority class from <%s> to <%s> forcefully.",
				sc.defaultPriorityClass.Name, pc.Name)
		}
		sc.defaultPriorityClass = pc
		sc.defaultPriority = pc.Value
	}

	sc.PriorityClasses[pc.Name] = pc
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
		klog.V(3).Infof("delete numainfo in cahce for node<%s>", info.Name)
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
	klog.V(3).Infof("update numaInfo<%s> in cahce, with spec: Policy: %v, resMap: %v", ss.Name, ss.Spec.Policies, ss.Spec.NumaResMap)
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
	klog.V(3).Infof("Delete numaInfo<%s> from cahce, with spec: Policy: %v, resMap: %v", ss.Name, ss.Spec.Policies, ss.Spec.NumaResMap)
}

// AddJob add job to scheduler cache
func (sc *SchedulerCache) AddJob(obj interface{}) {
	job, ok := obj.(*schedulingapi.JobInfo)
	if !ok {
		klog.Errorf("Cannot convert to *api.JobInfo: %v", obj)
		return
	}
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.Jobs[job.UID] = job
}

func (sc *SchedulerCache) setCSIResourceOnNode(csiNode *sv1.CSINode, node *v1.Node) {
	if csiNode == nil || node == nil {
		return
	}

	csiResources := make(map[v1.ResourceName]resource.Quantity)
	for i := range csiNode.Spec.Drivers {
		d := csiNode.Spec.Drivers[i]
		k := v1.ResourceName(volumeutil.GetCSIAttachLimitKey(d.Name))
		if d.Allocatable != nil && d.Allocatable.Count != nil {
			csiResources[k] = *resource.NewScaledQuantity(int64(*d.Allocatable.Count), -3)
		} else {
			// Count all csi volumes in cache, because the storage may change from unattachable volumes to attachable volumes after
			// the cache set up, in this case it is very difficult to refresh all task caches.
			// For unattachable volume, set the limits number to a very large value, in this way, scheduling will never
			// be limited due to insufficient quantity of it.
			csiResources[k] = *resource.NewScaledQuantity(DefaultAttachableVolumeQuantity, -3)
		}
	}

	if len(csiResources) == 0 {
		return
	}

	if node.Status.Allocatable == nil {
		node.Status.Allocatable = make(map[v1.ResourceName]resource.Quantity)
	}

	if node.Status.Capacity == nil {
		node.Status.Capacity = make(map[v1.ResourceName]resource.Quantity)
	}

	for resourceName, quantity := range csiResources {
		node.Status.Allocatable[resourceName] = quantity
		node.Status.Capacity[resourceName] = quantity
	}
}
