/*
Copyright 2025 The Volcano Authors.

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

package utils

import (
	"context"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

func CreateHyperNode(vcClient vcclientset.Interface, node *topologyv1alpha1.HyperNode) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(
			context.Background(),
			node,
			metav1.CreateOptions{},
		)
		return err
	})
}

// CreateOrUpdateHyperNode handles a stale informer cache by treating an
// AlreadyExists response as an update of the latest API object.
func CreateOrUpdateHyperNode(vcClient vcclientset.Interface, node *topologyv1alpha1.HyperNode) error {
	err := CreateHyperNode(vcClient, node)
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	return UpdateHyperNode(vcClient, node)
}

func UpdateHyperNode(vcClient vcclientset.Interface, updated *topologyv1alpha1.HyperNode) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := vcClient.TopologyV1alpha1().HyperNodes().Get(
			context.Background(), updated.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := validateHyperNodeOwnership(current, updated); err != nil {
			return err
		}

		current.Spec = updated.Spec
		current.Status = updated.Status

		if current.Labels == nil {
			current.Labels = make(map[string]string)
		}
		for k, v := range updated.Labels {
			current.Labels[k] = v
		}

		if current.Annotations == nil {
			current.Annotations = make(map[string]string)
		}
		for k, v := range updated.Annotations {
			current.Annotations[k] = v
		}

		current, err = vcClient.TopologyV1alpha1().HyperNodes().Update(context.Background(), current, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		// Status subresources are not persisted by the regular Update call.
		current.Status = updated.Status
		_, err = vcClient.TopologyV1alpha1().HyperNodes().UpdateStatus(context.Background(), current, metav1.UpdateOptions{})
		return err
	})
}

func validateHyperNodeOwnership(current, updated *topologyv1alpha1.HyperNode) error {
	desiredSource, exists := updated.Labels[api.NetworkTopologySourceLabelKey]
	if !exists || desiredSource == "" {
		return fmt.Errorf("HyperNode %q is missing the required %q label", updated.Name, api.NetworkTopologySourceLabelKey)
	}
	currentSource := current.Labels[api.NetworkTopologySourceLabelKey]
	if currentSource == desiredSource {
		return nil
	}
	return fmt.Errorf("refusing to update HyperNode %q owned by discovery source %q with result from %q",
		current.Name, currentSource, desiredSource)
}

func DeleteHyperNode(vcClient vcclientset.Interface, name string) error {
	return vcClient.TopologyV1alpha1().HyperNodes().Delete(
		context.Background(),
		name,
		metav1.DeleteOptions{},
	)
}

// BuildHyperNode creates a HyperNode object
func BuildHyperNode(name string, tier int, members []topologyv1alpha1.MemberSpec, labels map[string]string) *topologyv1alpha1.HyperNode {
	return &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:    tier,
			Members: members,
		},
	}
}

func BuildHyperNodeWithTierName(name string, tier int, tierName string, members []topologyv1alpha1.MemberSpec, labels map[string]string) *topologyv1alpha1.HyperNode {
	hyperNode := BuildHyperNode(name, tier, members, labels)
	hyperNode.Spec.TierName = tierName
	return hyperNode
}

// BuildMembers creates a list of topology member references
func BuildMembers(names []string, memberType topologyv1alpha1.MemberType) []topologyv1alpha1.MemberSpec {
	members := make([]topologyv1alpha1.MemberSpec, 0, len(names))
	sort.Strings(names)
	for _, name := range names {
		members = append(members, topologyv1alpha1.MemberSpec{
			Type:     memberType,
			Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: name}},
		})
	}
	return members
}
