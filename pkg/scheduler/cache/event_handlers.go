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
	"strconv"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/scheme"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/apis/pkg/apis/utils"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func isTerminated(status schedulingapi.TaskStatus) bool {
	return status == schedulingapi.Succeeded || status == schedulingapi.Failed
}

// getOrCreateJob will return corresponding Job for pi if it exists, or it will create a Job and return it if
// pi.Pod.Spec.SchedulerName is same as volcano scheduler's name, otherwise it will return nil.
func (sc *SchedulerCache) getOrCreateJob(pi *schedulingapi.TaskInfo) *schedulingapi.JobInfo {
	if len(pi.Job) == 0 {
		if pi.Pod.Spec.SchedulerName != sc.schedulerName {
			klog.V(4).Infof("Pod %s/%s will not scheduled by %s, skip creating PodGroup and Job for it",
				pi.Pod.Namespace, pi.Pod.Name, sc.schedulerName)
		}
		return nil
	}

	if _, found := sc.Jobs[pi.Job]; !found {
		sc.Jobs[pi.Job] = schedulingapi.NewJobInfo(pi.Job)
	}

	return sc.Jobs[pi.Job]
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
				if _, outOfSync := err.(*schedulingapi.AllocateFailError); outOfSync {
					node.State = schedulingapi.NodeState{
						Phase:  schedulingapi.NotReady,
						Reason: "OutOfSync",
					}
				}
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

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addPod(pod *v1.Pod) error {
	pi := schedulingapi.NewTaskInfo(pod)

	return sc.addTask(pi)
}

func (sc *SchedulerCache) syncTask(oldTask *schedulingapi.TaskInfo) error {
	newPod, err := sc.kubeClient.CoreV1().Pods(oldTask.Namespace).Get(context.TODO(), oldTask.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
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

	newTask := schedulingapi.NewTaskInfo(newPod)

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

func (sc *SchedulerCache) deleteTask(pi *schedulingapi.TaskInfo) error {
	var jobErr, nodeErr, numaErr error

	if len(pi.Job) != 0 {
		if job, found := sc.Jobs[pi.Job]; found {
			jobErr = job.DeleteTaskInfo(pi)
		} else {
			jobErr = fmt.Errorf("failed to find Job <%v> for Task %v/%v",
				pi.Job, pi.Namespace, pi.Name)
		}
	}

	if len(pi.NodeName) != 0 {
		node := sc.Nodes[pi.NodeName]
		if node != nil {
			nodeErr = node.RemoveTask(pi)
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

// Assumes that lock is already acquired.
func (sc *SchedulerCache) addNode(node *v1.Node) error {
	if sc.Nodes[node.Name] != nil {
		sc.Nodes[node.Name].SetNode(node)
	} else {
		sc.Nodes[node.Name] = schedulingapi.NewNodeInfo(node)
	}
	return nil
}

// Assumes that lock is already acquired.
func (sc *SchedulerCache) updateNode(oldNode, newNode *v1.Node) error {
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

	numaInfo := sc.Nodes[node.Name].NumaInfo
	if numaInfo != nil {
		klog.V(3).Infof("delete numatopo <%s/%s>", numaInfo.Namespace, numaInfo.Name)
		err := sc.vcClient.NodeinfoV1alpha1().Numatopologies().Delete(context.TODO(), numaInfo.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("delete numatopo <%s/%s> failed.", numaInfo.Namespace, numaInfo.Name)
		}
	}

	delete(sc.Nodes, node.Name)

	return nil
}

// AddNode add node to scheduler cache
func (sc *SchedulerCache) AddNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert to *v1.Node: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.addNode(node)
	if err != nil {
		klog.Errorf("Failed to add node %s into cache: %v", node.Name, err)
		return
	}
	sc.NodeList = append(sc.NodeList, node.Name)
}

// UpdateNode update node to scheduler cache
func (sc *SchedulerCache) UpdateNode(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert newObj to *v1.Node: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.updateNode(oldNode, newNode)
	if err != nil {
		klog.Errorf("Failed to update node %v in cache: %v", oldNode.Name, err)
		return
	}
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

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteNode(node)
	if err != nil {
		klog.Errorf("Failed to delete node %s from cache: %v", node.Name, err)
		return
	}

	for i, name := range sc.NodeList {
		if name == node.Name {
			sc.NodeList = append(sc.NodeList[:i], sc.NodeList[i+1:]...)
			break
		}
	}
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
	delete(sc.Queues, id)
}

//DeletePriorityClass delete priorityclass from the scheduler cache
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

//UpdatePriorityClass update priorityclass to scheduler cache
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

//AddPriorityClass add priorityclass to scheduler cache
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
		tmp.Allocatable = cpuset.MustParse(resInfo.Allocatable)
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
