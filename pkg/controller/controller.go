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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	crv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type SchedulerCacheController struct {
	SchedulerCacheControllerConfig *rest.Config
	SchedulerCache                 schedulercache.Cache
}

func (c *SchedulerCacheController) RunNodesInformer(ctx context.Context) {
	kubecli := kubernetes.NewForConfigOrDie(c.SchedulerCacheControllerConfig)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	nodeInformer := informerFactory.Core().V1().Nodes()
	nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addNodeToCache,
			UpdateFunc: c.updateNodeInCache,
			DeleteFunc: c.deleteNodeFromCache,
		},
		0,
	)

	go nodeInformer.Informer().Run(ctx.Done())
}

func (c *SchedulerCacheController) RunPodsInformer(ctx context.Context) {
	kubecli := kubernetes.NewForConfigOrDie(c.SchedulerCacheControllerConfig)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	podInformer := informerFactory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					fmt.Printf("====== pod filter, name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    c.addPodToCache,
				UpdateFunc: c.updatePodInCache,
				DeleteFunc: c.deletePodFromCache,
			},
		},
	)

	go podInformer.Informer().Run(ctx.Done())
}

func (c *SchedulerCacheController) RunAllocatorInformer(ctx context.Context) {
	extensionsclientset, err := apiextensionsclient.NewForConfig(c.SchedulerCacheControllerConfig)
	if err != nil {
		panic(err)
	}
	_, err = client.CreateCustomResourceDefinition(extensionsclientset)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		panic(err)
	}

	rqaClient, _, err := client.NewClient(c.SchedulerCacheControllerConfig)
	if err != nil {
		panic(err)
	}
	source := cache.NewListWatchFromClient(
		rqaClient,
		crv1.ResourceQuotaAllocatorPlural,
		v1.NamespaceAll,
		fields.Everything())

	_, controller := cache.NewInformer(
		source,
		&crv1.ResourceQuotaAllocator{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addAllocatorToCache,
			UpdateFunc: c.updateAllocatorInCache,
			DeleteFunc: c.deleteAllocatorFromCache,
		})
	go controller.Run(ctx.Done())
}

func (c *SchedulerCacheController) Run(ctx context.Context) {
	c.RunNodesInformer(ctx)
	c.RunPodsInformer(ctx)
	c.RunAllocatorInformer(ctx)
}

func (c *SchedulerCacheController) addPodToCache(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("cannot convert to *v1.Pod: %v", obj)
		return
	}

	fmt.Printf("======== ADD Pod(%s) into cache, status (%s)\n", pod.Name, pod.Status.Phase)
	if err := c.SchedulerCache.AddPod(pod); err != nil {
		glog.Errorf("scheduler cache AddPod failed: %v", err)
	}
}

func (c *SchedulerCacheController) updatePodInCache(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		glog.Errorf("cannot convert oldObj to *v1.Pod: %v", oldObj)
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Errorf("cannot convert newObj to *v1.Pod: %v", newObj)
		return
	}

	fmt.Printf("======== UPDATE oldPod(%s) status(%s) newPod(%s) status(%s) in cache\n", oldPod.Name, oldPod.Status.Phase, newPod.Name, newPod.Status.Phase)
	if err := c.SchedulerCache.UpdatePod(oldPod, newPod); err != nil {
		glog.Errorf("scheduler cache UpdatePod failed: %v", err)
	}
}

func (c *SchedulerCacheController) deletePodFromCache(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("cannot convert to *v1.Pod: %v", t)
		return
	}

	fmt.Printf("======== DELETE Pod(%s) status(%s) from cache\n", pod.Name, pod.Status.Phase)
	if err := c.SchedulerCache.RemovePod(pod); err != nil {
		glog.Errorf("scheduler cache RemovePod failed: %v", err)
	}
}

func (c *SchedulerCacheController) addNodeToCache(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		glog.Errorf("cannot convert to *v1.Node: %v", obj)
		return
	}

	fmt.Printf("======== ADD Node(%s) into cache\n", node.Name)
	if err := c.SchedulerCache.AddNode(node); err != nil {
		glog.Errorf("scheduler cache AddNode failed: %v", err)
	}
}

func (c *SchedulerCacheController) updateNodeInCache(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		glog.Errorf("cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		glog.Errorf("cannot convert newObj to *v1.Node: %v", newObj)
		return
	}

	fmt.Printf("======== UPDATE oldNode(%s) newNode(%s) in cache\n", oldNode.Name, newNode.Name)
	if err := c.SchedulerCache.UpdateNode(oldNode, newNode); err != nil {
		glog.Errorf("scheduler cache UpdateNode failed: %v", err)
	}
}

func (c *SchedulerCacheController) deleteNodeFromCache(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			glog.Errorf("cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("cannot convert to *v1.Node: %v", t)
		return
	}

	fmt.Printf("======== DELETE Node(%s) from cache\n", node.Name)
	if err := c.SchedulerCache.RemoveNode(node); err != nil {
		glog.Errorf("scheduler cache RemoveNode failed: %v", err)
	}
}

func (c *SchedulerCacheController) addAllocatorToCache(obj interface{}) {
	allocator, ok := obj.(*crv1.ResourceQuotaAllocator)
	if !ok {
		glog.Errorf("cannot convert to *v1.Pod: %v", obj)
		return
	}

	fmt.Printf("======== ADD Allocator(%s) into cache, status(%#v), spec(%#v)\n", allocator.Name, allocator.Status, allocator.Spec)
	if err := c.SchedulerCache.AddResourceQuotaAllocator(allocator); err != nil {
		glog.Errorf("scheduler cache AddPod failed: %v", err)
	}
}

func (c *SchedulerCacheController) updateAllocatorInCache(oldObj, newObj interface{}) {
	oldAllocator, ok := oldObj.(*crv1.ResourceQuotaAllocator)
	if !ok {
		glog.Errorf("cannot convert oldObj to *v1.Node: %v", oldObj)
		return
	}
	newAllocator, ok := newObj.(*crv1.ResourceQuotaAllocator)
	if !ok {
		glog.Errorf("cannot convert newObj to *v1.Node: %v", newObj)
		return
	}

	fmt.Printf("======== UPDATE oldAllocator(%s) in cache, status(%#v), spec(%#v)\n", oldAllocator.Name, oldAllocator.Status, oldAllocator.Spec)
	fmt.Printf("======== UPDATE newAllocator(%s) in cache, status(%#v), spec(%#v)\n", newAllocator.Name, newAllocator.Status, newAllocator.Spec)
	if err := c.SchedulerCache.UpdateResourceQuotaAllocator(oldAllocator, newAllocator); err != nil {
		glog.Errorf("scheduler cache UpdateNode failed: %v", err)
	}
}

func (c *SchedulerCacheController) deleteAllocatorFromCache(obj interface{}) {
	var allocator *crv1.ResourceQuotaAllocator
	switch t := obj.(type) {
	case *crv1.ResourceQuotaAllocator:
		allocator = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		allocator, ok = t.Obj.(*crv1.ResourceQuotaAllocator)
		if !ok {
			glog.Errorf("cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("cannot convert to *v1.Node: %v", t)
		return
	}

	fmt.Printf("======== DELETE Allocator(%s) from cache, status(%#v), spec(%#v)\n", allocator.Name, allocator.Status, allocator.Spec)
	if err := c.SchedulerCache.RemoveResourceQuotaAllocator(allocator); err != nil {
		glog.Errorf("scheduler cache RemoveNode failed: %v", err)
	}
}

func NewPodInformer(client kubernetes.Clientset, resyncPeriod time.Duration) cache.SharedIndexInformer {
	selector := fields.ParseSelectorOrDie("status.phase!=" + string(v1.PodSucceeded) + ",status.phase!=" + string(v1.PodFailed))
	lw := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), string(v1.ResourcePods), metav1.NamespaceAll, selector)
	return cache.NewSharedIndexInformer(lw, &v1.Pod{}, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

}

// assignedNonTerminatedPod selects pods that are assigned and non-terminal (scheduled and running).
func assignedNonTerminatedPod(pod *v1.Pod) bool {
	if len(pod.Spec.NodeName) == 0 {
		return false
	}
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false
	}
	return true
}

// unassignedNonTerminatedPod selects pods that are unassigned and non-terminal.
func unassignedNonTerminatedPod(pod *v1.Pod) bool {
	if len(pod.Spec.NodeName) != 0 {
		return false
	}
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false
	}
	return true
}

// nonTerminatedPod selects pods that are non-terminal.
func nonTerminatedPod(pod *v1.Pod) bool {
	if len(pod.Spec.NodeName) != 0 {
		return false
	}
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false
	}
	return true
}
