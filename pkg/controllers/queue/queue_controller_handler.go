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
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	busv1alpha1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func (c *Controller) enqueue(req *apis.Request) {
	c.queue.Add(req)
}

func (c *Controller) addQueue(obj interface{}) {
	queue := obj.(*schedulingv1beta1.Queue)

	req := &apis.Request{
		QueueName: queue.Name,

		Event:  busv1alpha1.OutOfSyncEvent,
		Action: busv1alpha1.SyncQueueAction,
	}

	c.enqueue(req)
}

func (c *Controller) deleteQueue(obj interface{}) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %#v.", obj)
			return
		}
		queue, ok = tombstone.Obj.(*schedulingv1beta1.Queue)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a Queue: %#v.", obj)
			return
		}
	}

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()
	delete(c.podGroups, queue.Name)
}

func (c *Controller) updateQueue(old, new interface{}) {
	oldQueue, ok := old.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("Can not covert old object %v to queues.scheduling.volcano.sh.", old)
		return
	}

	newQueue, ok := new.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("Can not covert new object %v to queues.scheduling.volcano.sh.", old)
		return
	}

	if oldQueue.ResourceVersion == newQueue.ResourceVersion {
		return
	}

	c.addQueue(newQueue)

	return
}

func (c *Controller) addPodGroup(obj interface{}) {
	pg := obj.(*schedulingv1beta1.PodGroup)
	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()

	if c.podGroups[pg.Spec.Queue] == nil {
		c.podGroups[pg.Spec.Queue] = make(map[string]struct{})
	}
	c.podGroups[pg.Spec.Queue][key] = struct{}{}

	req := &apis.Request{
		QueueName: pg.Spec.Queue,

		Event:  busv1alpha1.OutOfSyncEvent,
		Action: busv1alpha1.SyncQueueAction,
	}

	c.enqueue(req)
}

func (c *Controller) updatePodGroup(old, new interface{}) {
	oldPG := old.(*schedulingv1beta1.PodGroup)
	newPG := new.(*schedulingv1beta1.PodGroup)

	// Note: we have no use case update PodGroup.Spec.Queue
	// So do not consider it here.
	if oldPG.Status.Phase != newPG.Status.Phase {
		c.addPodGroup(newPG)
	}
}

func (c *Controller) deletePodGroup(obj interface{}) {
	pg, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %#v.", obj)
			return
		}
		pg, ok = tombstone.Obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a PodGroup: %#v.", obj)
			return
		}
	}

	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	defer c.pgMutex.Unlock()

	delete(c.podGroups[pg.Spec.Queue], key)

	req := &apis.Request{
		QueueName: pg.Spec.Queue,

		Event:  busv1alpha1.OutOfSyncEvent,
		Action: busv1alpha1.SyncQueueAction,
	}

	c.enqueue(req)
}

func (c *Controller) addCommand(obj interface{}) {
	cmd, ok := obj.(*busv1alpha1.Command)
	if !ok {
		klog.Errorf("Obj %v is not command.", obj)
		return
	}

	c.commandQueue.Add(cmd)
}

func (c *Controller) getPodGroups(key string) []string {
	c.pgMutex.RLock()
	defer c.pgMutex.RUnlock()

	if c.podGroups[key] == nil {
		return nil
	}
	podGroups := make([]string, 0, len(c.podGroups[key]))
	for pgKey := range c.podGroups[key] {
		podGroups = append(podGroups, pgKey)
	}

	return podGroups
}

func (c *Controller) recordEventsForQueue(name, eventType, reason, message string) {
	queue, err := c.queueLister.Get(name)
	if err != nil {
		klog.Errorf("Get queue %s failed for %v.", name, err)
		return
	}

	c.recorder.Event(queue, eventType, reason, message)
	return
}
