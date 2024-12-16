/*
Copyright 2024 The Volcano Authors.

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

package hypernode

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

const (
	// maxRetries is the number of times an event will be retried
	// before it is dropped out of the queue
	maxRetries = 15
	// labelSeparator is the separator to split hypernodes in the label
	labelSeparator = ","
)

func (hnc *hyperNodeController) addNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("%T is not a type of Node", obj)
		return
	}

	nodeEvent := &event{
		eventType: addEvent,
		obj:       node,
	}
	hnc.nodeQueue.Add(nodeEvent)
}

func (hnc *hyperNodeController) updateNode(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		klog.Errorf("%T is not a type of Node", oldObj)
		return
	}

	newNode, ok := newObj.(*v1.Node)
	if !ok {
		klog.Errorf("%T is not a type of Node", newObj)
		return
	}

	if newNode.Labels[hyperNodesLabelKey] == oldNode.Labels[hyperNodesLabelKey] {
		return
	}

	nodeEvent := &event{
		eventType: updateEvent,
		obj:       newNode,
		oldObj:    oldNode,
	}
	hnc.nodeQueue.Add(nodeEvent)
}

func (hnc *hyperNodeController) deleteNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("%T is not a type of Node", obj)
		return
	}

	nodeEvent := &event{
		eventType: deleteEvent,
		obj:       node,
	}
	hnc.nodeQueue.Add(nodeEvent)
}

func (hnc *hyperNodeController) processNextNodeEvent() bool {
	eventObj, shutdown := hnc.nodeQueue.Get()
	if shutdown {
		klog.Errorf("Fail to get items from work queue, it is already been closed")
		return false
	}
	defer hnc.nodeQueue.Done(eventObj)

	nodeEvent := eventObj.(*event)
	switch nodeEvent.eventType {
	case addEvent:
		node := nodeEvent.obj.(*v1.Node)

		hyperNodesList := strings.Split(node.Labels[hyperNodesLabelKey], labelSeparator)
		err := func() error {
			hnas := make([]*hyperNodeAction, 0)

			addHNAs, err := hnc.addHyperNodesList(node, hyperNodesList)
			if err != nil {
				return err
			}
			hnas = append(hnas, addHNAs...)
			err = hnc.executeHyperNodeActions(hnas)
			if err != nil {
				return err
			}

			return nil
		}()
		hnc.handleErr(err, hnc.nodeQueue, eventObj)
	case updateEvent:
		newNode := nodeEvent.obj.(*v1.Node)
		oldNode := nodeEvent.oldObj.(*v1.Node)

		newHNsList := strings.Split(newNode.Labels[hyperNodesLabelKey], labelSeparator)
		oldHNsList := strings.Split(oldNode.Labels[hyperNodesLabelKey], labelSeparator)
		err := func() error {
			hnas := make([]*hyperNodeAction, 0)

			updateHNAs, err := hnc.updateHyperNodeList(newNode, newHNsList, oldHNsList)
			if err != nil {
				return err
			}
			hnas = append(hnas, updateHNAs...)
			err = hnc.executeHyperNodeActions(hnas)
			if err != nil {
				return err
			}

			return nil
		}()
		hnc.handleErr(err, hnc.nodeQueue, eventObj)
	case deleteEvent:
		node := nodeEvent.obj.(*v1.Node)
		err := func() error {
			hnas := make([]*hyperNodeAction, 0)

			if obj, exist := hnc.hyperNodesCache.MembersToHyperNode.Load(node.Name); exist {
				parentHN := obj.(*topologyv1alpha1.HyperNode)
				newHNAs, err := hnc.removeMemberFromHyperNode(parentHN.Name, node.Name)
				if err != nil {
					return err
				}
				hnas = append(hnas, newHNAs...)
				err = hnc.executeHyperNodeActions(hnas)
				if err != nil {
					return err
				}
				hnc.hyperNodesCache.MembersToHyperNode.Delete(node.Name)
			}

			return nil
		}()
		hnc.handleErr(err, hnc.nodeQueue, eventObj)
	}

	return true
}

func (hnc *hyperNodeController) addHyperNode(obj interface{}) {
	hn, ok := obj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.Errorf("%T is not a type of HyperNode", obj)
		return
	}

	hnEvent := &event{
		eventType: addEvent,
		obj:       hn,
	}
	hnc.hyperNodeQueue.Add(hnEvent)
}

func (hnc *hyperNodeController) updateHyperNode(oldObj, newObj interface{}) {
	oldHN, ok := oldObj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.Errorf("%T is not a type of HyperNode", oldObj)
		return
	}

	newHN, ok := newObj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.Errorf("%T is not a type of HyperNode", newObj)
		return
	}

	hnEvent := &event{
		eventType: updateEvent,
		obj:       newHN,
		oldObj:    oldHN,
	}
	hnc.hyperNodeQueue.Add(hnEvent)
}

func (hnc *hyperNodeController) deleteHyperNode(obj interface{}) {
	hn, ok := obj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.Errorf("%T is not a type of HyperNode", obj)
		return
	}

	hnEvent := &event{
		eventType: deleteEvent,
		obj:       hn,
	}
	hnc.hyperNodeQueue.Add(hnEvent)
}

func (hnc *hyperNodeController) processNextHyperNodeEvent() bool {
	eventObj, shutdown := hnc.hyperNodeQueue.Get()
	if shutdown {
		klog.Errorf("Fail to get items from work queue, it is already been closed")
		return false
	}
	defer hnc.hyperNodeQueue.Done(eventObj)

	hnEvent := eventObj.(*event)
	switch hnEvent.eventType {
	case addEvent:
		hn := hnEvent.obj.(*topologyv1alpha1.HyperNode)
		err := hnc.hyperNodesCache.UpdateHyperNode(hn)
		hnc.handleErr(err, hnc.hyperNodeQueue, eventObj)
		for _, member := range hn.Spec.Members {
			hnc.hyperNodesCache.MembersToHyperNode.Store(member.Selector.ExactMatch.Name, hn)
		}
	case updateEvent:
		newHN := hnEvent.obj.(*topologyv1alpha1.HyperNode)
		err := hnc.hyperNodesCache.UpdateHyperNode(newHN)
		hnc.handleErr(err, hnc.hyperNodeQueue, eventObj)
		oldHN := hnEvent.oldObj.(*topologyv1alpha1.HyperNode)
		addMembers, removeMembers := hnc.getMemberDifferences(oldHN.Spec.Members, newHN.Spec.Members)
		for _, member := range addMembers {
			hnc.hyperNodesCache.MembersToHyperNode.Store(member.Selector.ExactMatch.Name, newHN)
		}
		for _, member := range removeMembers {
			hnc.hyperNodesCache.MembersToHyperNode.Delete(member.Selector.ExactMatch.Name)
		}
	case deleteEvent:
		hn := eventObj.(*topologyv1alpha1.HyperNode)
		err := hnc.hyperNodesCache.DeleteHyperNode(hn)
		hnc.handleErr(err, hnc.hyperNodeQueue, eventObj)
		// 1. Delete the members cache entry of itself as a member
		hnc.hyperNodesCache.MembersToHyperNode.Delete(hn.Name)
		// 2. Delete the members cache entries under itself
		for _, member := range hn.Spec.Members {
			hnc.hyperNodesCache.MembersToHyperNode.Delete(member.Selector.ExactMatch.Name)
		}
	}

	return true
}

func (hnc *hyperNodeController) getMemberDifferences(oldMembers, newMembers []topologyv1alpha1.MemberSpec) (addMembers []topologyv1alpha1.MemberSpec, removeMembers []topologyv1alpha1.MemberSpec) {
	oldSet := make(map[string]struct{})
	newSet := make(map[string]struct{})

	for _, member := range oldMembers {
		oldSet[member.Selector.ExactMatch.Name] = struct{}{}
	}

	for _, member := range newMembers {
		newSet[member.Selector.ExactMatch.Name] = struct{}{}
	}

	for _, member := range oldMembers {
		if _, exists := newSet[member.Selector.ExactMatch.Name]; !exists {
			removeMembers = append(removeMembers, member)
		}
	}

	for _, member := range newMembers {
		if _, exists := oldSet[member.Selector.ExactMatch.Name]; !exists {
			addMembers = append(addMembers, member)
		}
	}

	return removeMembers, addMembers
}

func (hnc *hyperNodeController) handleErr(err error, queue workqueue.RateLimitingInterface, item interface{}) {
	if err == nil {
		queue.Forget(item)
		return
	}

	if queue.NumRequeues(item) < maxRetries {
		klog.Errorf("Error to handle event type: %v, retrying", err)
		queue.AddRateLimited(item)
		return
	}

	klog.Errorf("Retry budget exceeded: %v, dropping event type out of the queue", err)
	queue.Forget(item)
	utilruntime.HandleError(err)
}
