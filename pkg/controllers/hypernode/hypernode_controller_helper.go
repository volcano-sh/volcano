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
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"

	hncache "volcano.sh/volcano/pkg/controllers/hypernode/cache"
)

const (
	hyperNodesLabelKey = "volcano.sh/hypernodes"
	TypeHyperNode      = "HyperNode"
	TypeNode           = "Node"
)

type hyperNodeAction struct {
	hyperNode *topologyv1alpha1.HyperNode
	action    action
	// used in executeHyperNodeActions as a callback func
	callback func(*topologyv1alpha1.HyperNode)
}

// action represents the type of request sent to api-server
type action string

const (
	updateAction action = "update"
	createAction action = "create"
	deleteAction action = "delete"
)

func (a hyperNodeAction) String() string {
	return fmt.Sprintf("hypernode: %v, action: %s", a.hyperNode, a.action)
}

func (hnc *hyperNodeController) executeHyperNodeActions(hyperNodeActions []*hyperNodeAction) error {
	for _, hna := range hyperNodeActions {
		switch hna.action {
		case createAction:
			createdHN, err := hnc.vcClient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hna.hyperNode, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create hypernode %s: %v", hna.hyperNode.Name, err)
			}
			// Add the created HyperNode to the hyperNodesCache to avoid the situation where the controller has not listed/watched
			// the hyperNode to the informer cache yet but continues to process the next node, it may lead to the controller misjudges that
			// the hypernode does not exist but continues to create a new hyperNode.
			err = hnc.hyperNodesCache.UpdateHyperNode(createdHN)
			if err != nil {
				return fmt.Errorf("failed to add hypernode %s to local cache: %v", createdHN.Name, err)
			}
			klog.V(3).Infof("Succeeded to create the hypernode %s", createdHN.Name)
			hna.callback(createdHN)
		case updateAction:
			updatedHN, err := hnc.vcClient.TopologyV1alpha1().HyperNodes().Update(context.TODO(), hna.hyperNode, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update hypernode %s: %v", hna.hyperNode.Name, err)
			}
			err = hnc.hyperNodesCache.UpdateHyperNode(updatedHN)
			if err != nil {
				return fmt.Errorf("failed to update hypernode %s in local cache: %v", updatedHN.Name, err)
			}
			klog.V(3).Infof("Succeeded to update the hypernode %s", updatedHN.Name)
			hna.callback(updatedHN)
		case deleteAction:
			err := hnc.vcClient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hna.hyperNode.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete hypernode %s: %v", hna.hyperNode.Name, err)
			}
			err = hnc.hyperNodesCache.DeleteHyperNode(hna.hyperNode)
			if err != nil {
				return fmt.Errorf("failed to update hypernode %s in local cache: %v", hna.hyperNode.Name, err)
			}
			klog.V(3).Infof("Succeeded to delete the hypernode %s", hna.hyperNode.Name)
			hna.callback(hna.hyperNode)
		}
	}

	return nil
}

func newHyperNodeWithEmptyMembers(tier, hyperNodeName string) *topologyv1alpha1.HyperNode {
	hn := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: hyperNodeName,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:    tier,
			Members: []topologyv1alpha1.MemberSpec{},
		},
	}
	return hn
}

func newHyperNodeMember(nodeType, memberName string) topologyv1alpha1.MemberSpec {
	member := topologyv1alpha1.MemberSpec{
		Type: nodeType,
		Selector: topologyv1alpha1.MemberSelector{
			Type: topologyv1alpha1.ExactMatchMemberSelectorType,
			ExactMatch: &topologyv1alpha1.ExactMatch{
				Name: memberName,
			},
		},
	}
	return member
}

func newNodeInformer(client kubernetes.Interface, rsyncPeriod time.Duration) cache.SharedIndexInformer {
	require, _ := labels.NewRequirement(hyperNodesLabelKey, selection.Exists, nil)
	tweakListOptions := func(options *metav1.ListOptions) {
		options.LabelSelector = require.String()
	}
	informer := coreinformers.NewFilteredNodeInformer(
		client,
		rsyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		tweakListOptions,
	)
	return informer
}

func (hnc *hyperNodeController) addHyperNodesList(node *v1.Node, hyperNodesList []string) ([]*hyperNodeAction, error) {
	hnas := make([]*hyperNodeAction, 0)

	for index := 0; index < len(hyperNodesList); index++ {
		hna, err := hnc.createOrUpdateHyperNode(node, hyperNodesList, index)
		if err != nil {
			// if meet error when traverse hyperNodesList, we return empty hyperNodeActionList
			return []*hyperNodeAction{}, fmt.Errorf("failed to get action for hypernode %s: %v", hyperNodesList[index], err)
		}
		if hna != nil {
			hnas = append(hnas, hna)
		}
	}

	return hnas, nil
}

func (hnc *hyperNodeController) updateHyperNodeList(node *v1.Node, oldHyperNodesList, newHyperNodesList []string) ([]*hyperNodeAction, error) {
	hnas := make([]*hyperNodeAction, 0)

	// case 1: new hypernodes list label is empty but old is not, we need to delete the member node in the tier-1 hypernode
	if len(oldHyperNodesList) != 0 && len(newHyperNodesList) == 0 {
		if obj, exist := hnc.hyperNodesCache.MembersToHyperNode.Load(node.Name); exist {
			parentHN := obj.(*topologyv1alpha1.HyperNode)
			newHNAs, err := hnc.removeMemberFromHyperNode(parentHN.Name, node.Name)
			if err != nil {
				return []*hyperNodeAction{}, err
			}
			hnas = append(hnas, newHNAs...)
		}
	}

	// case 2:
	// oldHyperNodesList: [s0,s4,s6], newHyperNodesList: [s1,s4,s6]
	// The tier-1 hypernode to which the node belongs has changed. s0 needs to delete the node, and s1 needs to add the node as member.
	// Currently, we don't support change the tier>1 hypernode, such as from [s0,s4,s6] to [s0,s5,s6], so judge index 0 is enough.
	if len(oldHyperNodesList) != 0 && len(newHyperNodesList) != 0 && oldHyperNodesList[0] != newHyperNodesList[0] {
		newHNAs, err := hnc.removeMemberFromHyperNode(oldHyperNodesList[0], node.Name)
		if err != nil {
			return []*hyperNodeAction{}, err
		}
		hnas = append(hnas, newHNAs...)

		hna, err := hnc.createOrUpdateHyperNode(node, newHyperNodesList, 0)
		if err != nil {
			return []*hyperNodeAction{}, err
		}
		if hna != nil {
			hnas = append(hnas, hna)
		}
	}

	return hnas, nil
}

func (hnc *hyperNodeController) createOrUpdateHyperNode(node *v1.Node, hyperNodesList []string, index int) (*hyperNodeAction, error) {
	var hna *hyperNodeAction

	hn, err := hnc.hyperNodesCache.GetHyperNode(hyperNodesList[index])
	if err != nil {
		if !errors.Is(err, hncache.NotFoundError) {
			klog.Errorf("met error when get hypernode %s from local cache", hyperNodesList[index])
			return nil, err
		}
		// create a new hyper node with empty members
		tier := strconv.Itoa(index + 1)
		hn = newHyperNodeWithEmptyMembers(tier, hyperNodesList[index])
		hna = &hyperNodeAction{
			hyperNode: hn,
			action:    createAction,
		}
	} else {
		hna = &hyperNodeAction{
			hyperNode: hn,
			action:    updateAction,
		}
	}

	member := hnc.createMember(node, hyperNodesList, index)
	if member == nil {
		// no new members need to be added
		return nil, nil
	}
	hna.hyperNode.Spec.Members = append(hna.hyperNode.Spec.Members, *member)
	hna.callback = func(hn *topologyv1alpha1.HyperNode) {
		hnc.hyperNodesCache.MembersToHyperNode.Store(member.Selector.ExactMatch.Name, hn)
	}

	return hna, nil
}

func (hnc *hyperNodeController) createMember(node *v1.Node, hyperNodesList []string, index int) *topologyv1alpha1.MemberSpec {
	var member topologyv1alpha1.MemberSpec
	if index == 0 {
		member = newHyperNodeMember(TypeNode, node.Name)
	} else {
		member = newHyperNodeMember(TypeHyperNode, hyperNodesList[index-1])
	}

	if cachedObj, exists := hnc.hyperNodesCache.MembersToHyperNode.Load(member.Selector.ExactMatch.Name); exists {
		cachedHN := cachedObj.(*topologyv1alpha1.HyperNode)
		// The current member already exists, check whether the parent hypernode is the same
		if cachedHN.Name == hyperNodesList[index] {
			klog.V(4).Infof("member %s already exists under the hypernode %s, skipping", member.Selector.ExactMatch.Name, cachedHN.Name)
			return nil
		}
		// The parent hypernode is inconsistent and need to update
		klog.V(4).Infof("member %s exists but belongs to a different hypernode %s, updating parent", member.Selector.ExactMatch.Name, cachedHN.Name)
	}

	return &member
}

func (hnc *hyperNodeController) removeMemberFromHyperNode(hyperNodeName, memberName string) ([]*hyperNodeAction, error) {
	hn, err := hnc.hyperNodesCache.GetHyperNode(hyperNodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get hypernode %s from local cache: %v", hyperNodeName, err)
	}

	memberIndex := -1
	for index, member := range hn.Spec.Members {
		if member.Selector.ExactMatch.Name == memberName {
			memberIndex = index
			break
		}
	}

	if memberIndex == -1 {
		return nil, fmt.Errorf("did not find the member %s in hypernode %s", memberName, hyperNodeName)
	}

	// If the member to be deleted is the only member of this hypernode, then we just directly delete the hypernode.
	// Correspondingly, if this hypernode is a child hypernode of the other hypernode, then we should also remove the member of its parent hypernode.
	// e.g., there are two nodes node-0 and node-1 under hypernode s0. And then the hypernode which they belong to is changed to s1.
	// Then s0 needs to be deleted, and s0 needs to be removed from the member list of s4 ( s0 is the child of s4).
	if len(hn.Spec.Members) == 1 {
		return hnc.recursivelyDeleteHyperNode(hn)
	}

	hn.Spec.Members = append(hn.Spec.Members[:memberIndex], hn.Spec.Members[memberIndex+1:]...)
	klog.V(3).Infof("Removed member %s from hypernode %s", memberName, hyperNodeName)
	hna := &hyperNodeAction{
		hyperNode: hn,
		action:    updateAction,
		callback: func(hn *topologyv1alpha1.HyperNode) {
			hnc.hyperNodesCache.MembersToHyperNode.Delete(memberName)
		},
	}
	return []*hyperNodeAction{hna}, nil
}

func (hnc *hyperNodeController) recursivelyDeleteHyperNode(hn *topologyv1alpha1.HyperNode) ([]*hyperNodeAction, error) {
	hnas := []*hyperNodeAction{
		{
			hyperNode: hn,
			action:    deleteAction,
			callback: func(hn *topologyv1alpha1.HyperNode) {
				for _, member := range hn.Spec.Members {
					hnc.hyperNodesCache.MembersToHyperNode.Delete(member.Selector.ExactMatch.Name)
				}
			},
		},
	}

	// If the hypernode belongs to another hypernode, recursively delete the members of the parent hypernode
	if obj, exist := hnc.hyperNodesCache.MembersToHyperNode.Load(hn.Name); exist {
		parentHN := obj.(*topologyv1alpha1.HyperNode)
		recurHNAs, err := hnc.removeMemberFromHyperNode(parentHN.Name, hn.Name)
		if err != nil {
			return hnas, err
		}
		hnas = append(hnas, recurHNAs...)
	}

	return hnas, nil
}
