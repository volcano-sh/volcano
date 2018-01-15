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

package cache

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

func consumerInfoEqual(l, r *ConsumerInfo) bool {
	if !reflect.DeepEqual(l, r) {
		return false
	}

	return true
}

func TestConsumerInfo_AddPod(t *testing.T) {

	// case1
	case01_consumer := buildConsumer("c1", "c1")
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})

	// case2
	case02_consumer := buildConsumer("c1", "c1")
	case02_owner := metav1.OwnerReference{
		UID: "owner1",
	}
	case02_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner})
	case02_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner})

	tests := []struct {
		name     string
		consumer *arbv1.Consumer
		pods     []*v1.Pod
		expected *ConsumerInfo
	}{
		{
			name:     "add 1 pending non-owner pod, add 1 running non-owner pod",
			consumer: case01_consumer,
			pods:     []*v1.Pod{case01_pod1, case01_pod2},
			expected: &ConsumerInfo{
				Consumer:  case01_consumer,
				Name:      "c1",
				Namespace: "c1",
				PodSets:   make(map[types.UID]*PodSet),
				Pods: map[string]*PodInfo{
					"p1": NewPodInfo(case01_pod1),
					"p2": NewPodInfo(case01_pod2),
				},
			},
		},
		{
			name:     "add 1 pending owner pod, add 1 running owner pod",
			consumer: case02_consumer,
			pods:     []*v1.Pod{case02_pod1, case02_pod2},
			expected: &ConsumerInfo{
				Consumer:  case02_consumer,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*PodSet{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name: "owner1",
							UID:  "owner1",
						},
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*PodInfo{
							NewPodInfo(case02_pod2),
						},
						Pending: []*PodInfo{
							NewPodInfo(case02_pod1),
						},
						Others: []*PodInfo{},
					},
				},
				Pods: make(map[string]*PodInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewConsumerInfo(test.consumer)

		for _, pod := range test.pods {
			pi := NewPodInfo(pod)
			ci.AddPod(pi)
		}

		if !consumerInfoEqual(ci, test.expected) {
			t.Errorf("consumer info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}

func TestConsumerInfo_RemovePod(t *testing.T) {

	// case1
	case01_consumer := buildConsumer("c1", "c1")
	case01_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	case01_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})
	case01_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{})

	// case2
	case02_consumer := buildConsumer("c1", "c1")
	case02_owner := metav1.OwnerReference{
		UID: "owner1",
	}
	case02_pod1 := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner})
	case02_pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner})
	case02_pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner})

	tests := []struct {
		name     string
		consumer *arbv1.Consumer
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *ConsumerInfo
	}{
		{
			name:     "add 1 pending non-owner pod, add 2 running non-owner pod, remove 1 running non-owner pod",
			consumer: case01_consumer,
			pods:     []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			rmPods:   []*v1.Pod{case01_pod2},
			expected: &ConsumerInfo{
				Consumer:  case01_consumer,
				Name:      "c1",
				Namespace: "c1",
				PodSets:   make(map[types.UID]*PodSet),
				Pods: map[string]*PodInfo{
					"p1": NewPodInfo(case01_pod1),
					"p3": NewPodInfo(case01_pod3),
				},
			},
		},
		{
			name:     "add 1 pending owner pod, add 2 running owner pod, remove 1 running owner pod",
			consumer: case02_consumer,
			pods:     []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods:   []*v1.Pod{case02_pod2},
			expected: &ConsumerInfo{
				Consumer:  case02_consumer,
				Name:      "c1",
				Namespace: "c1",
				PodSets: map[types.UID]*PodSet{
					"owner1": {
						ObjectMeta: metav1.ObjectMeta{
							Name: "owner1",
							UID:  "owner1",
						},
						Allocated:    buildResource("1000m", "1G"),
						TotalRequest: buildResource("2000m", "2G"),
						Running: []*PodInfo{
							NewPodInfo(case02_pod3),
						},
						Pending: []*PodInfo{
							NewPodInfo(case02_pod1),
						},
						Others: []*PodInfo{},
					},
				},
				Pods: make(map[string]*PodInfo),
			},
		},
	}

	for i, test := range tests {
		ci := NewConsumerInfo(test.consumer)

		for _, pod := range test.pods {
			pi := NewPodInfo(pod)
			ci.AddPod(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewPodInfo(pod)
			ci.RemovePod(pi)
		}

		if !consumerInfoEqual(ci, test.expected) {
			t.Errorf("consumer info %d: \n expected %v, \n got %v \n",
				i, test.expected, ci)
		}
	}
}
