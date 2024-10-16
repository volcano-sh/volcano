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
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/queue/state"
)

func (c *queuecontroller) syncQueue(queue *schedulingv1beta1.Queue, updateStateFn state.UpdateQueueStatusFn) error {
	klog.V(4).Infof("Begin to sync queue %s.", queue.Name)
	defer klog.V(4).Infof("End sync queue %s.", queue.Name)

	podGroups := c.getPodGroups(queue.Name)
	queueStatus := schedulingv1beta1.QueueStatus{}

	for _, pgKey := range podGroups {
		// Ignore error here, tt can not occur.
		ns, name, _ := cache.SplitMetaNamespaceKey(pgKey)

		pg, err := c.pgLister.PodGroups(ns).Get(name)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}

			klog.V(4).Infof("The podGroup %s is not found, skip it and continue to sync cache", pgKey)
			c.pgMutex.Lock()
			delete(c.podGroups[queue.Name], pgKey)
			c.pgMutex.Unlock()
			continue
		}

		switch pg.Status.Phase {
		case schedulingv1beta1.PodGroupPending:
			queueStatus.Pending++
		case schedulingv1beta1.PodGroupRunning:
			queueStatus.Running++
		case schedulingv1beta1.PodGroupUnknown:
			queueStatus.Unknown++
		case schedulingv1beta1.PodGroupInqueue:
			queueStatus.Inqueue++
		}
	}

	if updateStateFn != nil {
		updateStateFn(&queueStatus, podGroups)
	} else {
		queueStatus.State = queue.Status.State
	}

	queueStatus.Allocated = queue.Status.Allocated.DeepCopy()
	// queue.status.allocated will be updated after every session close in volcano scheduler, we should not depend on it because session may be time-consuming,
	// and queue.status.allocated can't be updated timely. We initialize queue.status.allocated and update it here explicitly
	// to avoid update queue err because update will fail when queue.status.allocated is nil.
	if queueStatus.Allocated == nil {
		queueStatus.Allocated = v1.ResourceList{}
	}

	// ignore update when status does not change
	if equality.Semantic.DeepEqual(queueStatus, queue.Status) {
		return nil
	}

	newQueue := queue.DeepCopy()
	newQueue.Status = queueStatus
	if _, err := c.vcClient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), newQueue, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update status of Queue %s: %v.", newQueue.Name, err)
		return err
	}

	return nil
}

func (c *queuecontroller) openQueue(queue *schedulingv1beta1.Queue, updateStateFn state.UpdateQueueStatusFn) error {
	klog.V(4).Infof("Begin to open queue %s.", queue.Name)

	if queue.Status.State != schedulingv1beta1.QueueStateOpen {
		continued, err := c.openHierarchicalQueue(queue)
		if !continued {
			return err
		}
	}

	newQueue := queue.DeepCopy()
	newQueue.Status.State = schedulingv1beta1.QueueStateOpen

	if queue.Status.State != newQueue.Status.State {
		if _, err := c.vcClient.SchedulingV1beta1().Queues().Update(context.TODO(), newQueue, metav1.UpdateOptions{}); err != nil {
			c.recorder.Event(newQueue, v1.EventTypeWarning, string(v1alpha1.OpenQueueAction),
				fmt.Sprintf("Open queue failed for %v", err))
			return err
		}

		c.recorder.Event(newQueue, v1.EventTypeNormal, string(v1alpha1.OpenQueueAction), "Open queue succeed")
	} else {
		return nil
	}

	q, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), newQueue.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	newQueue = q.DeepCopy()
	if updateStateFn != nil {
		updateStateFn(&newQueue.Status, nil)
	} else {
		return fmt.Errorf("internal error, update state function should be provided")
	}

	if queue.Status.State != newQueue.Status.State {
		if _, err := c.vcClient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), newQueue, metav1.UpdateOptions{}); err != nil {
			c.recorder.Event(newQueue, v1.EventTypeWarning, string(v1alpha1.OpenQueueAction),
				fmt.Sprintf("Update queue status from %s to %s failed for %v",
					queue.Status.State, newQueue.Status.State, err))
			return err
		}
	}

	return nil
}

func (c *queuecontroller) closeQueue(queue *schedulingv1beta1.Queue, updateStateFn state.UpdateQueueStatusFn) error {
	klog.V(4).Infof("Begin to close queue %s.", queue.Name)

	if queue.Status.State != schedulingv1beta1.QueueStateClosed && queue.Status.State != schedulingv1beta1.QueueStateClosing {
		continued, err := c.closeHierarchicalQueue(queue)
		if !continued {
			return err
		}
	}

	newQueue := queue.DeepCopy()
	newQueue.Status.State = schedulingv1beta1.QueueStateClosed

	if queue.Status.State != newQueue.Status.State {
		if _, err := c.vcClient.SchedulingV1beta1().Queues().Update(context.TODO(), newQueue, metav1.UpdateOptions{}); err != nil {
			c.recorder.Event(newQueue, v1.EventTypeWarning, string(v1alpha1.CloseQueueAction),
				fmt.Sprintf("Close queue failed for %v", err))
			return err
		}

		c.recorder.Event(newQueue, v1.EventTypeNormal, string(v1alpha1.CloseQueueAction), "Close queue succeed")
	} else {
		return nil
	}

	q, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), newQueue.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	newQueue = q.DeepCopy()
	podGroups := c.getPodGroups(newQueue.Name)
	if updateStateFn != nil {
		updateStateFn(&newQueue.Status, podGroups)
	} else {
		return fmt.Errorf("internal error, update state function should be provided")
	}

	if queue.Status.State != newQueue.Status.State {
		if _, err := c.vcClient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), newQueue, metav1.UpdateOptions{}); err != nil {
			c.recorder.Event(newQueue, v1.EventTypeWarning, string(v1alpha1.CloseQueueAction),
				fmt.Sprintf("Update queue status from %s to %s failed for %v",
					queue.Status.State, newQueue.Status.State, err))
			return err
		}
	}

	return nil
}

func (c *queuecontroller) openHierarchicalQueue(queue *schedulingv1beta1.Queue) (bool, error) {
	if queue.Spec.Parent == "" || queue.Spec.Parent == "root" {
		return true, nil
	}

	parentQueue, err := c.queueLister.Get(queue.Spec.Parent)
	if err != nil {
		return false, fmt.Errorf("Failed to get parent queue <%s> of queue <%s>: %v", queue.Spec.Parent, queue.Name, err)
	}

	if parentQueue.Status.State == schedulingv1beta1.QueueStateClosing || parentQueue.Status.State == schedulingv1beta1.QueueStateClosed {
		klog.Errorf("Failed to open queue %s because its parent queue %s is closing or closed. Open the parent queue first.", queue.Name, queue.Spec.Parent)
		return false, nil
	}

	return true, nil
}

func (c *queuecontroller) closeHierarchicalQueue(queue *schedulingv1beta1.Queue) (bool, error) {
	if queue.Name == "root" {
		klog.Errorf("Root queue cannot be closed")
		return false, nil
	}

	queueList, err := c.queueLister.List(labels.Everything())
	if err != nil {
		return false, err
	}

	openChildQueue := make([]string, 0)
	for _, childQueue := range queueList {
		if childQueue.Spec.Parent != queue.Name {
			continue
		}
		if childQueue.Status.State != schedulingv1beta1.QueueStateClosed && childQueue.Status.State != schedulingv1beta1.QueueStateClosing {
			req := &apis.Request{
				QueueName: childQueue.Name,
				Action:    busv1alpha1.CloseQueueAction,
			}

			c.enqueue(req)
			openChildQueue = append(openChildQueue, childQueue.Name)
			klog.V(3).Infof("Closing child queue <%s> because its parent queue %s is closing or closed.", childQueue.Name, queue.Name)
		}
	}

	if len(openChildQueue) > 0 {
		return false, fmt.Errorf("failed to close queue %s because its child queues %v are still open", queue.Name, strings.Join(openChildQueue, ","))
	}

	return true, nil
}
