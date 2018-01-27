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
	"sync"

	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client"
	informerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/informers"
	arbclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/informers/v1"

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

	podInformer            clientv1.PodInformer
	nodeInformer           clientv1.NodeInformer
	quotaAllocatorInformer arbclient.QuotaAllocatorInformer

	pods            map[string]*PodInfo
	nodes           map[string]*NodeInfo
	quotaAllocators map[string]*QuotaAllocatorInfo
}

func newSchedulerCache(config *rest.Config) *schedulerCache {
	sc := &schedulerCache{
		nodes:           make(map[string]*NodeInfo),
		pods:            make(map[string]*PodInfo),
		quotaAllocators: make(map[string]*QuotaAllocatorInfo),
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

	// create quotaAllocator resource first
	err := createQuotaAllocatorCRD(config)
	if err != nil {
		panic(err)
	}

	// create quotaAllocator informer
	quotaAllocatorClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	qInformerFactory := informerfactory.NewSharedInformerFactory(quotaAllocatorClient, 0)
	// create informer for quotaAllocator information
	sc.quotaAllocatorInformer = qInformerFactory.QuotaAllocator().QuotaAllocators()
	sc.quotaAllocatorInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.QuotaAllocator:
					glog.V(4).Infof("Filter quotaAllocator name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddQuotaAllocator,
				UpdateFunc: sc.UpdateQuotaAllocator,
				DeleteFunc: sc.DeleteQuotaAllocator,
			},
		})

	return sc
}

func createQuotaAllocatorCRD(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQuotaAllocatorCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (sc *schedulerCache) Run(stopCh <-chan struct{}) {
	go sc.podInformer.Informer().Run(stopCh)
	go sc.nodeInformer.Informer().Run(stopCh)
	go sc.quotaAllocatorInformer.Informer().Run(stopCh)
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
func (sc *schedulerCache) addQuotaAllocator(quotaAllocator *arbv1.QuotaAllocator) error {
	if _, ok := sc.quotaAllocators[quotaAllocator.Name]; ok {
		return fmt.Errorf("quotaAllocator %v exist", quotaAllocator.Name)
	}

	info := &QuotaAllocatorInfo{
		name:           quotaAllocator.Name,
		quotaAllocator: quotaAllocator.DeepCopy(),
		Pods:           make([]*v1.Pod, 0),
	}

	// init Request if it is nil
	if info.QuotaAllocator().Spec.Request.Resources == nil {
		info.QuotaAllocator().Spec.Request.Resources = map[arbv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}

	// init Deserved/Allocated/Used/Preemping if it is nil
	if info.QuotaAllocator().Status.Deserved.Resources == nil {
		info.QuotaAllocator().Status.Deserved.Resources = map[arbv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.QuotaAllocator().Status.Allocated.Resources == nil {
		info.QuotaAllocator().Status.Allocated.Resources = map[arbv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.QuotaAllocator().Status.Used.Resources == nil {
		info.QuotaAllocator().Status.Used.Resources = map[arbv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	if info.QuotaAllocator().Status.Preempting.Resources == nil {
		info.QuotaAllocator().Status.Preempting.Resources = map[arbv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		}
	}
	sc.quotaAllocators[quotaAllocator.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateQuotaAllocator(oldQuotaAllocator, newQuotaAllocator *arbv1.QuotaAllocator) error {
	if err := sc.deleteQuotaAllocator(oldQuotaAllocator); err != nil {
		return err
	}
	sc.addQuotaAllocator(newQuotaAllocator)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteQuotaAllocator(quotaAllocator *arbv1.QuotaAllocator) error {
	if _, ok := sc.quotaAllocators[quotaAllocator.Name]; !ok {
		return fmt.Errorf("quotaAllocator %v doesn't exist", quotaAllocator.Name)
	}
	delete(sc.quotaAllocators, quotaAllocator.Name)
	return nil
}

func (sc *schedulerCache) AddQuotaAllocator(obj interface{}) {
	quotaAllocator, ok := obj.(*arbv1.QuotaAllocator)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.QuotaAllocator: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add quotaAllocator(%s) into cache, status(%#v), spec(%#v)", quotaAllocator.Name, quotaAllocator.Status, quotaAllocator.Spec)
	err := sc.addQuotaAllocator(quotaAllocator)
	if err != nil {
		glog.Errorf("Failed to add quotaAllocator %s into cache: %v", quotaAllocator.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) UpdateQuotaAllocator(oldObj, newObj interface{}) {
	oldQuotaAllocator, ok := oldObj.(*arbv1.QuotaAllocator)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *arbv1.QuotaAllocator: %v", oldObj)
		return
	}
	newQuotaAllocator, ok := newObj.(*arbv1.QuotaAllocator)
	if !ok {
		glog.Errorf("Cannot convert newObj to *arbv1.QuotaAllocator: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldQuotaAllocator(%s) in cache, status(%#v), spec(%#v)", oldQuotaAllocator.Name, oldQuotaAllocator.Status, oldQuotaAllocator.Spec)
	glog.V(4).Infof("Update newQuotaAllocator(%s) in cache, status(%#v), spec(%#v)", newQuotaAllocator.Name, newQuotaAllocator.Status, newQuotaAllocator.Spec)
	err := sc.updateQuotaAllocator(oldQuotaAllocator, newQuotaAllocator)
	if err != nil {
		glog.Errorf("Failed to update quotaAllocator %s into cache: %v", oldQuotaAllocator.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) DeleteQuotaAllocator(obj interface{}) {
	var quotaAllocator *arbv1.QuotaAllocator
	switch t := obj.(type) {
	case *arbv1.QuotaAllocator:
		quotaAllocator = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		quotaAllocator, ok = t.Obj.(*arbv1.QuotaAllocator)
		if !ok {
			glog.Errorf("Cannot convert to *v1.QuotaAllocator: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.QuotaAllocator: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteQuotaAllocator(quotaAllocator)
	if err != nil {
		glog.Errorf("Failed to delete quotaAllocator %s from cache: %v", quotaAllocator.Name, err)
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

func (sc *schedulerCache) QuotaAllocatorInformer() arbclient.QuotaAllocatorInformer {
	return sc.quotaAllocatorInformer
}

func (sc *schedulerCache) Dump() *CacheSnapshot {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &CacheSnapshot{
		Nodes:           make([]*NodeInfo, 0, len(sc.nodes)),
		Pods:            make([]*PodInfo, 0, len(sc.pods)),
		QuotaAllocators: make([]*QuotaAllocatorInfo, 0, len(sc.quotaAllocators)),
	}

	for _, value := range sc.nodes {
		snapshot.Nodes = append(snapshot.Nodes, value.Clone())
	}
	for _, value := range sc.pods {
		snapshot.Pods = append(snapshot.Pods, value.Clone())
	}
	for _, value := range sc.quotaAllocators {
		snapshot.QuotaAllocators = append(snapshot.QuotaAllocators, value.Clone())
	}
	return snapshot
}
