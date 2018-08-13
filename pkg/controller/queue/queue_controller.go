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

package queue

import (
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	arbschedv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset/versioned"
	arbinformers "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/externalversions"
	schedinfov1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/externalversions/scheduling/v1alpha1"
	schedlisterv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/listers/scheduling/v1alpha1"
)

type Controller struct {
	config        *rest.Config
	queueInformer schedinfov1.QueueInformer
	nsInformer    coreinformers.NamespaceInformer

	// A store of jobs
	queueLister schedlisterv1.QueueLister
	queueSynced func() bool

	// A store of pods, populated by the podController
	nsListr  corelisters.NamespaceLister
	nsSynced func() bool

	kubeclients *kubernetes.Clientset
	arbclients  *versioned.Clientset

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
}

func NewController(config *rest.Config) *Controller {
	c := &Controller{
		config:      config,
		kubeclients: kubernetes.NewForConfigOrDie(config),
		arbclients:  versioned.NewForConfigOrDie(config),
		eventQueue:  cache.NewFIFO(eventKey),
	}

	c.queueInformer = arbinformers.NewSharedInformerFactory(c.arbclients, 0).Scheduling().V1alpha1().Queues()
	c.queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteQueue,
	})
	c.queueLister = c.queueInformer.Lister()
	c.queueSynced = c.queueInformer.Informer().HasSynced

	c.nsInformer = informers.NewSharedInformerFactory(c.kubeclients, 0).Core().V1().Namespaces()
	c.nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNS,
		UpdateFunc: c.updateNS,
		DeleteFunc: c.deleteNS,
	})

	c.nsListr = c.nsInformer.Lister()
	c.nsSynced = c.nsInformer.Informer().HasSynced

	return c
}

// Run start Job Controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	go c.queueInformer.Informer().Run(stopCh)
	go c.nsInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, c.queueSynced, c.nsSynced)

	go wait.Until(c.worker, time.Second, stopCh)
}

func (c *Controller) worker() {
	c.eventQueue.Pop(func(item interface{}) error {
		nsName := item.(string)

		// If the Namespace does not exist, delete Queue accordingly.
		if _, err := c.kubeclients.CoreV1().Namespaces().Get(nsName, metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				if err := c.arbclients.SchedulingV1alpha1().Queues().Delete(nsName, metav1.NewDeleteOptions(0)); err != nil {
					if !errors.IsNotFound(err) {
						return err
					}
					return nil
				}
				return nil
			}
			return err
		}

		if _, err := c.arbclients.SchedulingV1alpha1().Queues().Get(nsName, metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				q := &arbschedv1.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName,
					},
					Spec: arbschedv1.QueueSpec{
						Weight: int32(1),
					},
				}
				_, err := c.arbclients.SchedulingV1alpha1().Queues().Create(q)
				return err
			}
			return err
		}

		return nil
	})
}

func (c *Controller) deleteQueue(obj interface{}) {
	q := obj.(*arbschedv1.Queue)
	c.eventQueue.AddIfNotPresent(q.Name)
}

func (c *Controller) addNS(obj interface{}) {
	ns := obj.(*v1.Namespace)
	c.eventQueue.AddIfNotPresent(ns.Name)
}

func (c *Controller) updateNS(old, obj interface{}) {
	ns := obj.(*v1.Namespace)
	c.eventQueue.AddIfNotPresent(ns.Name)
}

func (c *Controller) deleteNS(obj interface{}) {
	ns := obj.(*v1.Namespace)
	c.eventQueue.AddIfNotPresent(ns.Name)
}
