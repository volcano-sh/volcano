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
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/apis/pkg/client/listers/topology/v1alpha1"
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

func UpdateHyperNode(vcClient vcclientset.Interface, lister v1alpha1.HyperNodeLister, updated *topologyv1alpha1.HyperNode) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := lister.Get(updated.Name)
		if err != nil {
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

		_, err = vcClient.TopologyV1alpha1().HyperNodes().UpdateStatus(context.Background(), current, metav1.UpdateOptions{})
		return err
	})
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
