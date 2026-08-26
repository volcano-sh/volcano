/*
Copyright 2026 The Volcano Authors.

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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestGetMembers(t *testing.T) {
	nodes := []*corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "worker-1", Labels: map[string]string{"role": "worker"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "worker-2", Labels: map[string]string{"role": "worker"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "control-plane-1", Labels: map[string]string{"role": "control-plane"}}},
	}
	tests := []struct {
		name     string
		selector topologyv1alpha1.MemberSelector
		want     sets.Set[string]
	}{
		{name: "exact", selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "worker-1"}}, want: sets.New("worker-1")},
		{name: "regex", selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: `^worker-`}}, want: sets.New("worker-1", "worker-2")},
		{name: "invalid regex", selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: `[`}}, want: sets.New[string]()},
		{name: "label", selector: topologyv1alpha1.MemberSelector{LabelMatch: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}}}, want: sets.New("worker-1", "worker-2")},
		{name: "empty label", selector: topologyv1alpha1.MemberSelector{LabelMatch: &metav1.LabelSelector{}}, want: sets.New[string]()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := GetMembers(test.selector, nodes); !got.Equal(test.want) {
				t.Fatalf("GetMembers() = %v, want %v", got, test.want)
			}
		})
	}
}
