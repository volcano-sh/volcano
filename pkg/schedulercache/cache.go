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

package schedulercache

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	qclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/queue/v1"
	qjobclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/queuejob/v1"
	tsclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/taskset/v1"

	"k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/informers"
	clientv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// New returns a Cache implementation.
func New(
	config *rest.Config,
) Cache {
	return newSchedulerCache(config)
}

type schedulerCache struct {
	sync.Mutex

	podInformer      clientv1.PodInformer
	nodeInformer     clientv1.NodeInformer
	queueInformer    qclient.QueueInformer
	queueJobInformer qjobclient.QueueJobInformer
	taskSetInformer  tsclient.TaskSetInformer

	pods      map[string]*PodInfo
	nodes     map[string]*NodeInfo
	queues    map[string]*QueueInfo
	queuejobs map[string]*QueueJobInfo
	tasksets  map[string]*TaskSetInfo
}

func newSchedulerCache(config *rest.Config) *schedulerCache {
	sc := &schedulerCache{
		nodes:     make(map[string]*NodeInfo),
		pods:      make(map[string]*PodInfo),
		queues:    make(map[string]*QueueInfo),
		queuejobs: make(map[string]*QueueJobInfo),
		tasksets:  make(map[string]*TaskSetInfo),
	}

	kubecli := kubernetes.NewForConfigOrDie(config)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	// create informer for node information
	sc.nodeInformer = informerFactory.Core().V1().Nodes()
	sc.nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddNode,
			UpdateFunc: sc.UpdateNode,
			DeleteFunc: sc.DeleteNode,
		},
		0,
	)

	// create informer for pod information
	sc.podInformer = informerFactory.Core().V1().Pods()
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					glog.V(4).Infof("Filter pod name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddPod,
				UpdateFunc: sc.UpdatePod,
				DeleteFunc: sc.DeletePod,
			},
		})

	// create queue resource first
	err := createQueueCRD(config)
	if err != nil {
		panic(err)
	}

	// create queue informer
	queueClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	qInformerFactory := qInformerfactory.NewSharedInformerFactory(queueClient, 0)
	// create informer for queue information
	sc.queueInformer = qInformerFactory.Queue().Queues()
	sc.queueInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.Queue:
					glog.V(4).Infof("Filter queue name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddQueue,
				UpdateFunc: sc.UpdateQueue,
				DeleteFunc: sc.DeleteQueue,
			},
		})

	// create queue resource first
	err = createQueueJob(config)
	if err != nil {
		panic(err)
	}

	// create queuejob informer
	queuejobClient, _, err := client.NewQueueJobClient(config)
	if err != nil {
		panic(err)
	}

	qjobInformerFactory := qInformerfactory.NewSharedInformerFactory(queuejobClient, 0)

	// create informer for queuejob information
	sc.queueJobInformer = qjobInformerFactory.QueueJob().QueueJobs()
	sc.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.QueueJob:
					glog.V(4).Infof("filter queuejob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddQueueJob,
				UpdateFunc: sc.UpdateQueueJob,
				DeleteFunc: sc.DeleteQueueJob,
			},
		})

	// create taskset resource first
	err = createTaskSet(config)
	if err != nil {
		panic(err)
	}

	// create taskset informer
	tasksetClient, _, err := client.NewTaskSetClient(config)
	if err != nil {
		panic(err)
	}

	tsInformerFactory := qInformerfactory.NewSharedInformerFactory(tasksetClient, 0)
	// create informer for taskset information
	sc.taskSetInformer = tsInformerFactory.TaskSet().TaskSets()
	sc.taskSetInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.TaskSet:
					glog.V(4).Infof("Filter taskset name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddTaskSet,
				UpdateFunc: sc.UpdateTaskSet,
				DeleteFunc: sc.DeleteTaskSet,
			},
		})

	return sc
}

func createQueueCRD(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createQueueJob(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJob(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createTaskSet(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateTaskSetCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (sc *schedulerCache) Run(stopCh <-chan struct{}) {
	go sc.podInformer.Informer().Run(stopCh)
	go sc.nodeInformer.Informer().Run(stopCh)
	go sc.queueInformer.Informer().Run(stopCh)
	go sc.queueJobInformer.Informer().Run(stopCh)
	go sc.taskSetInformer.Informer().Run(stopCh)
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) addPod(pod *v1.Pod) error {
	key, err := getPodKey(pod)
	if err != nil {
		return err
	}

	if _, ok := sc.pods[key]; ok {
		return fmt.Errorf("pod %v exist", key)
	}

	info := &PodInfo{
		name: key,
		pod:  pod.DeepCopy(),
	}
	sc.pods[key] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	if err := sc.deletePod(oldPod); err != nil {
		return err
	}
	sc.addPod(newPod)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deletePod(pod *v1.Pod) error {
	key, err := getPodKey(pod)
	if err != nil {
		return err
	}

	if _, ok := sc.pods[key]; !ok {
		return fmt.Errorf("pod %v doesn't exist", key)
	}
	delete(sc.pods, key)
	return nil
}

func (sc *schedulerCache) AddPod(obj interface{}) {
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

func (sc *schedulerCache) UpdatePod(oldObj, newObj interface{}) {
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

func (sc *schedulerCache) DeletePod(obj interface{}) {
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
func (sc *schedulerCache) addNode(node *v1.Node) error {
	if _, ok := sc.nodes[node.Name]; ok {
		return fmt.Errorf("node %v exist", node.Name)
	}

	info := &NodeInfo{
		name: node.Name,
		node: node.DeepCopy(),
	}
	sc.nodes[node.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateNode(oldNode, newNode *v1.Node) error {
	if err := sc.deleteNode(oldNode); err != nil {
		return err
	}
	sc.addNode(newNode)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteNode(node *v1.Node) error {
	if _, ok := sc.nodes[node.Name]; !ok {
		return fmt.Errorf("node %v doesn't exist", node.Name)
	}
	delete(sc.nodes, node.Name)
	return nil
}

func (sc *schedulerCache) AddNode(obj interface{}) {
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

func (sc *schedulerCache) UpdateNode(oldObj, newObj interface{}) {
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

func (sc *schedulerCache) DeleteNode(obj interface{}) {
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
func (sc *schedulerCache) addQueue(queue *apiv1.Queue) error {
	if _, ok := sc.queues[queue.Name]; ok {
		return fmt.Errorf("queue %v exist", queue.Name)
	}

	info := &QueueInfo{
		name:  queue.Name,
		queue: queue.DeepCopy(),
		Pods:  make([]*v1.Pod, 0),
	}

	// init Request if it is nil
	if info.Queue().Spec.Request.Resources == nil {
		info.Queue().Spec.Request.Resources = map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}

	// init Deserved/Allocated/Used/Preemping if it is nil
	if info.Queue().Status.Deserved.Resources == nil {
		info.Queue().Status.Deserved.Resources = map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.Queue().Status.Allocated.Resources == nil {
		info.Queue().Status.Allocated.Resources = map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.Queue().Status.Used.Resources == nil {
		info.Queue().Status.Used.Resources = map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.Queue().Status.Preempting.Resources == nil {
		info.Queue().Status.Preempting.Resources = map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	sc.queues[queue.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateQueue(oldQueue, newQueue *apiv1.Queue) error {
	if err := sc.deleteQueue(oldQueue); err != nil {
		return err
	}
	sc.addQueue(newQueue)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteQueue(queue *apiv1.Queue) error {
	if _, ok := sc.queues[queue.Name]; !ok {
		return fmt.Errorf("queue %v doesn't exist", queue.Name)
	}
	delete(sc.queues, queue.Name)
	return nil
}

func (sc *schedulerCache) AddQueue(obj interface{}) {
	queue, ok := obj.(*apiv1.Queue)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.Queue: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add queue(%s) into cache, status(%#v), spec(%#v)", queue.Name, queue.Status, queue.Spec)
	err := sc.addQueue(queue)
	if err != nil {
		glog.Errorf("Failed to add queue %s into cache: %v", queue.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) UpdateQueue(oldObj, newObj interface{}) {
	oldQueue, ok := oldObj.(*apiv1.Queue)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *apiv1.Queue: %v", oldObj)
		return
	}
	newQueue, ok := newObj.(*apiv1.Queue)
	if !ok {
		glog.Errorf("Cannot convert newObj to *apiv1.Queue: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldQueue(%s) in cache, status(%#v), spec(%#v)", oldQueue.Name, oldQueue.Status, oldQueue.Spec)
	glog.V(4).Infof("Update newQueue(%s) in cache, status(%#v), spec(%#v)", newQueue.Name, newQueue.Status, newQueue.Spec)
	err := sc.updateQueue(oldQueue, newQueue)
	if err != nil {
		glog.Errorf("Failed to update queue %s into cache: %v", oldQueue.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) DeleteQueue(obj interface{}) {
	var queue *apiv1.Queue
	switch t := obj.(type) {
	case *apiv1.Queue:
		queue = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		queue, ok = t.Obj.(*apiv1.Queue)
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

	err := sc.deleteQueue(queue)
	if err != nil {
		glog.Errorf("Failed to delete queue %s from cache: %v", queue.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) addQueueJob(queuejob *apiv1.QueueJob) error {
	if _, ok := sc.queuejobs[queuejob.Name]; ok {
		return fmt.Errorf("queuejob %v exist", queuejob.Name)
	}

	info := &QueueJobInfo{
		name:     queuejob.Name,
		queuejob: queuejob.DeepCopy(),
	}
	sc.queuejobs[queuejob.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateQueueJob(oldQueueJob, newQueueJob *apiv1.QueueJob) error {
	if err := sc.deleteQueueJob(oldQueueJob); err != nil {
		return err
	}
	sc.addQueueJob(newQueueJob)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteQueueJob(queuejob *apiv1.QueueJob) error {
	if _, ok := sc.queuejobs[queuejob.Name]; !ok {
		return fmt.Errorf("queuejob %v doesn't exist", queuejob.Name)
	}
	delete(sc.queuejobs, queuejob.Name)
	return nil
}

func (sc *schedulerCache) AddQueueJob(obj interface{}) {

	queuejob, ok := obj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.QueueJob: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add queuejob(%s) into cache, status(%#v), spec(%#v)\n", queuejob.Name, queuejob.Status, queuejob.Spec)
	err := sc.addQueueJob(queuejob)
	if err != nil {
		glog.Errorf("Failed to add queuejob %s into cache: %v", queuejob.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) UpdateQueueJob(oldObj, newObj interface{}) {
	oldQueueJob, ok := oldObj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *apiv1.QueueJob: %v", oldObj)
		return
	}
	newQueueJob, ok := newObj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert newObj to *apiv1.QueueJob: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldQueueJob(%s) in cache, status(%#v), spec(%#v)", oldQueueJob.Name, oldQueueJob.Status, oldQueueJob.Spec)
	glog.V(4).Infof("Update newQueueJob(%s) in cache, status(%#v), spec(%#v)", newQueueJob.Name, newQueueJob.Status, newQueueJob.Spec)
	err := sc.updateQueueJob(oldQueueJob, newQueueJob)
	if err != nil {
		glog.Errorf("Failed to update queuejob %s into cache: %v", oldQueueJob.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) DeleteQueueJob(obj interface{}) {
	var queuejob *apiv1.QueueJob
	switch t := obj.(type) {
	case *apiv1.QueueJob:
		queuejob = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		queuejob, ok = t.Obj.(*apiv1.QueueJob)
		if !ok {
			glog.Errorf("Cannot convert to *v1.QueueJob: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.QueueJob: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteQueueJob(queuejob)
	if err != nil {
		glog.Errorf("Failed to delete queuejob %s from cache: %v", queuejob.Name, err)
		return
	}
	return
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) addTaskSet(taskset *apiv1.TaskSet) error {
	if _, ok := sc.tasksets[taskset.Name]; ok {
		return fmt.Errorf("taskset %v exist", taskset.Name)
	}

	info := &TaskSetInfo{
		name:    taskset.Name,
		taskSet: taskset.DeepCopy(),
	}
	sc.tasksets[taskset.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateTaskSet(oldTaskset, newTaskset *apiv1.TaskSet) error {
	if err := sc.deleteTaskSet(oldTaskset); err != nil {
		return err
	}
	if err := sc.addTaskSet(newTaskset); err != nil {
		return err
	}
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteTaskSet(taskset *apiv1.TaskSet) error {
	if _, ok := sc.tasksets[taskset.Name]; !ok {
		return fmt.Errorf("taskset %v doesn't exist", taskset.Name)
	}
	delete(sc.tasksets, taskset.Name)
	return nil
}

func (sc *schedulerCache) AddTaskSet(obj interface{}) {
	taskset, ok := obj.(*apiv1.TaskSet)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.TaskSet: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add taskset(%s) into cache, status(%#v), spec(%#v)", taskset.Name, taskset.Status, taskset.Spec)
	err := sc.addTaskSet(taskset)
	if err != nil {
		glog.Errorf("Failed to add taskset %s into cache: %v", taskset.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) UpdateTaskSet(oldObj, newObj interface{}) {
	oldTaskset, ok := oldObj.(*apiv1.TaskSet)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *apiv1.TaskSet: %v", oldObj)
		return
	}
	newTaskset, ok := newObj.(*apiv1.TaskSet)
	if !ok {
		glog.Errorf("Cannot convert newObj to *apiv1.TaskSet: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldTaskset(%s) in cache, status(%#v), spec(%#v)", oldTaskset.Name, oldTaskset.Status, oldTaskset.Spec)
	glog.V(4).Infof("Update newTaskset(%s) in cache, status(%#v), spec(%#v)", newTaskset.Name, newTaskset.Status, newTaskset.Spec)
	err := sc.updateTaskSet(oldTaskset, newTaskset)
	if err != nil {
		glog.Errorf("Failed to update taskset %s into cache: %v", oldTaskset.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) DeleteTaskSet(obj interface{}) {
	var taskset *apiv1.TaskSet
	switch t := obj.(type) {
	case *apiv1.TaskSet:
		taskset = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		taskset, ok = t.Obj.(*apiv1.TaskSet)
		if !ok {
			glog.Errorf("Cannot convert to *v1.TaskSet: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.TaskSet: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteTaskSet(taskset)
	if err != nil {
		glog.Errorf("Failed to delete taskset %s from cache: %v", taskset.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) PodInformer() clientv1.PodInformer {
	return sc.podInformer
}

func (sc *schedulerCache) NodeInformer() clientv1.NodeInformer {
	return sc.nodeInformer
}

func (sc *schedulerCache) QueueInformer() qclient.QueueInformer {
	return sc.queueInformer
}

func (sc *schedulerCache) QueueJobInformer() qjobclient.QueueJobInformer {
	return sc.queueJobInformer
}

func (sc *schedulerCache) TaskSetInformer() tsclient.TaskSetInformer {
	return sc.taskSetInformer
}

func (sc *schedulerCache) Dump() *CacheSnapshot {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &CacheSnapshot{
		Nodes:     make([]*NodeInfo, 0, len(sc.nodes)),
		Pods:      make([]*PodInfo, 0, len(sc.pods)),
		Queues:    make([]*QueueInfo, 0, len(sc.queues)),
		QueueJobs: make([]*QueueJobInfo, 0, len(sc.queuejobs)),
		TaskSets:  make([]*TaskSetInfo, 0, len(sc.tasksets)),
	}

	for _, value := range sc.nodes {
		snapshot.Nodes = append(snapshot.Nodes, value.Clone())
	}
	for _, value := range sc.pods {
		snapshot.Pods = append(snapshot.Pods, value.Clone())
	}
	for _, value := range sc.queues {
		snapshot.Queues = append(snapshot.Queues, value.Clone())
	}
	for _, value := range sc.queuejobs {
		snapshot.QueueJobs = append(snapshot.QueueJobs, value.Clone())
	}
	for _, value := range sc.tasksets {
		snapshot.TaskSets = append(snapshot.TaskSets, value.Clone())
	}
	return snapshot
}
