/*
Copyright 2019 The Volcano Authors.

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
	"fmt"
	"reflect"
	"sync"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	schedulingv1alpha2 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	kbclientset "volcano.sh/volcano/pkg/client/clientset/versioned"
	kbinformerfactory "volcano.sh/volcano/pkg/client/informers/externalversions"
	kbinformer "volcano.sh/volcano/pkg/client/informers/externalversions/scheduling/v1alpha2"
	kblister "volcano.sh/volcano/pkg/client/listers/scheduling/v1alpha2"
)

// Controller manages queue status.
type Controller struct {
	kubeClient kubernetes.Interface
	kbClient   kbclientset.Interface

	// informer
	queueInformer kbinformer.QueueInformer
	pgInformer    kbinformer.PodGroupInformer

	// queueLister
	queueLister kblister.QueueLister
	queueSynced cache.InformerSynced

	// podGroup lister
	pgLister kblister.PodGroupLister
	pgSynced cache.InformerSynced

	// queues that need to be updated.
	queue workqueue.RateLimitingInterface

	pgMutex   sync.RWMutex
	podGroups map[string]map[string]struct{}
}

// NewQueueController creates a QueueController
func NewQueueController(
	kubeClient kubernetes.Interface,
	kbClient kbclientset.Interface,
) *Controller {
	factory := kbinformerfactory.NewSharedInformerFactory(kbClient, 0)
	queueInformer := factory.Scheduling().V1alpha2().Queues()
	pgInformer := factory.Scheduling().V1alpha2().PodGroups()
	c := &Controller{
		kubeClient: kubeClient,
		kbClient:   kbClient,

		queueInformer: queueInformer,
		pgInformer:    pgInformer,

		queueLister: queueInformer.Lister(),
		queueSynced: queueInformer.Informer().HasSynced,

		pgLister: pgInformer.Lister(),
		pgSynced: pgInformer.Informer().HasSynced,

		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		podGroups: make(map[string]map[string]struct{}),
	}

	queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addQueue,
		DeleteFunc: c.deleteQueue,
	})

	pgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addPodGroup,
		UpdateFunc: c.updatePodGroup,
		DeleteFunc: c.deletePodGroup,
	})

	return c
}

// Run starts QueueController
func (c *Controller) Run(stopCh <-chan struct{}) {

	go c.queueInformer.Informer().Run(stopCh)
	go c.pgInformer.Informer().Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.queueSynced, c.pgSynced) {
		glog.Errorf("unable to sync caches for queue controller")
		return
	}

	go wait.Until(c.worker, 0, stopCh)
	glog.Infof("QueueController is running ...... ")
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same `queue`
// at the same time.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	eKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(eKey)

	if err := c.syncQueue(eKey.(string)); err != nil {
		glog.V(2).Infof("Error syncing queues %q, retrying. Error: %v", eKey, err)
		c.queue.AddRateLimited(eKey)
		return true
	}

	c.queue.Forget(eKey)
	return true
}

func (c *Controller) getPodGroups(key string) ([]string, error) {
	c.pgMutex.RLock()
	defer c.pgMutex.RUnlock()

	if c.podGroups[key] == nil {
		return nil, fmt.Errorf("queue %s has not been seen or deleted", key)
	}
	podGroups := make([]string, 0, len(c.podGroups[key]))
	for pgKey := range c.podGroups[key] {
		podGroups = append(podGroups, pgKey)
	}

	return podGroups, nil
}

func (c *Controller) syncQueue(key string) error {
	glog.V(4).Infof("Begin sync queue %s", key)

	podGroups, err := c.getPodGroups(key)
	if err != nil {
		return err
	}

	queueStatus := schedulingv1alpha2.QueueStatus{}

	for _, pgKey := range podGroups {
		// Ignore error here, tt can not occur.
		ns, name, _ := cache.SplitMetaNamespaceKey(pgKey)

		// TODO: check NotFound error and sync local cache.
		pg, err := c.pgLister.PodGroups(ns).Get(name)
		if err != nil {
			return err
		}

		switch pg.Status.Phase {
		case schedulingv1alpha2.PodGroupPending:
			queueStatus.Pending++
		case schedulingv1alpha2.PodGroupRunning:
			queueStatus.Running++
		case schedulingv1alpha2.PodGroupUnknown:
			queueStatus.Unknown++
		case schedulingv1alpha2.PodGroupInqueue:
			queueStatus.Inqueue++
		}
	}

	queue, err := c.queueLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(2).Infof("queue %s has been deleted", key)
			return nil
		}
		// TODO: do not retry to syncQueue for this error
		return err
	}

	// ignore update when status does not change
	if reflect.DeepEqual(queueStatus, queue.Status) {
		return nil
	}

	newQueue := queue.DeepCopy()
	newQueue.Status = queueStatus

	if _, err := c.kbClient.SchedulingV1alpha2().Queues().UpdateStatus(newQueue); err != nil {
		glog.Errorf("Failed to update status of Queue %s: %v", newQueue.Name, err)
		return err
	}

	return nil
}

func (c *Controller) addQueue(obj interface{}) {
	queue := obj.(*schedulingv1alpha2.Queue)
	c.queue.Add(queue.Name)
}

func (c *Controller) deleteQueue(obj interface{}) {
	queue, ok := obj.(*schedulingv1alpha2.Queue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		queue, ok = tombstone.Obj.(*schedulingv1alpha2.Queue)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a Queue: %#v", obj)
			return
		}
	}

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()
	delete(c.podGroups, queue.Name)
}

func (c *Controller) addPodGroup(obj interface{}) {
	pg := obj.(*schedulingv1alpha2.PodGroup)
	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()

	if c.podGroups[pg.Spec.Queue] == nil {
		c.podGroups[pg.Spec.Queue] = make(map[string]struct{})
	}
	c.podGroups[pg.Spec.Queue][key] = struct{}{}

	// enqueue
	c.queue.Add(pg.Spec.Queue)
}

func (c *Controller) updatePodGroup(old, new interface{}) {
	oldPG := old.(*schedulingv1alpha2.PodGroup)
	newPG := new.(*schedulingv1alpha2.PodGroup)

	// Note: we have no use case update PodGroup.Spec.Queue
	// So do not consider it here.
	if oldPG.Status.Phase != newPG.Status.Phase {
		c.queue.Add(newPG.Spec.Queue)
	}
}

func (c *Controller) deletePodGroup(obj interface{}) {
	pg, ok := obj.(*schedulingv1alpha2.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		pg, ok = tombstone.Obj.(*schedulingv1alpha2.PodGroup)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a PodGroup: %#v", obj)
			return
		}
	}

	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()

	delete(c.podGroups[pg.Spec.Queue], key)

	c.queue.Add(pg.Spec.Queue)
}
