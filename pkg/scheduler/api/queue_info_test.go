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

package api

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
)

func TestNewQueueInfo(t *testing.T) {
	reclaimable := false
	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "research",
			CreationTimestamp: metav1.NewTime(time.Unix(1, 0)),
			Annotations: map[string]string{
				"example.com/queue": "cluster",
			},
		},
		Spec: scheduling.QueueSpec{
			Parent:          "root",
			Capability:      resourceList("10"),
			Guarantee:       scheduling.Guarantee{Resource: resourceList("2")},
			Deserved:        resourceList("6"),
			Reclaimable:     &reclaimable,
			Priority:        5,
			DequeueStrategy: scheduling.DequeueStrategyFIFO,
			Affinity: &scheduling.Affinity{
				NodeGroupAffinity: &scheduling.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []string{"gpu"},
				},
			},
		},
		Status: scheduling.QueueStatus{
			State:     scheduling.QueueStateOpen,
			Allocated: resourceList("3"),
		},
	}

	info := NewQueueInfo(queue)
	if info.UID != QueueID("research") || info.Scope != ClusterQueueScope || info.Parent != QueueID("root") {
		t.Fatalf("unexpected queue identity: %#v", info)
	}
	if info.Capability.Cpu().Cmp(resource.MustParse("10")) != 0 ||
		info.Guarantee.Resource.Cpu().Cmp(resource.MustParse("2")) != 0 ||
		info.Deserved.Cpu().Cmp(resource.MustParse("6")) != 0 ||
		info.Allocated.Cpu().Cmp(resource.MustParse("3")) != 0 {
		t.Fatalf("queue resource fields were not copied: %#v", info)
	}
	if info.Priority != 5 || info.DequeueStrategy != scheduling.DequeueStrategyFIFO || info.State != scheduling.QueueStateOpen {
		t.Fatalf("queue scheduling fields were not copied: %#v", info)
	}
	if info.Reclaimable() {
		t.Fatal("expected non-reclaimable queue")
	}
	if info.Annotations["example.com/queue"] != "cluster" || info.Affinity == nil {
		t.Fatalf("queue metadata was not normalized: %#v", info)
	}
	if !info.CreationTimestamp.Equal(&queue.CreationTimestamp) {
		t.Fatal("queue creation timestamp was not normalized")
	}
}

func TestNewNamespaceQueueInfo(t *testing.T) {
	reclaimable := true
	nq := &scheduling.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "team-a",
			Name:        "training",
			Annotations: map[string]string{"example.com/queue": "namespace"},
		},
		Spec: scheduling.NamespaceQueueSpec{
			Parent:          "department",
			Capability:      resourceList("10"),
			Guarantee:       scheduling.Guarantee{Resource: resourceList("2")},
			Deserved:        resourceList("6"),
			Reclaimable:     &reclaimable,
			Priority:        5,
			DequeueStrategy: scheduling.DequeueStrategyFIFO,
		},
		Status: scheduling.NamespaceQueueStatus{
			State:     scheduling.QueueStateOpen,
			Allocated: resourceList("3"),
		},
	}

	info, err := NewNamespaceQueueInfo(nq)
	if err != nil {
		t.Fatalf("NewNamespaceQueueInfo() error = %v", err)
	}
	if info.UID != QueueID("team-a/training") || info.Scope != NamespaceQueueScope || info.Parent != QueueID("team-a/department") {
		t.Fatalf("unexpected namespace queue identity: %#v", info)
	}
	if info.Queue != nil || info.NamespaceQueue == nil {
		t.Fatalf("unexpected source objects: %#v", info)
	}
	if info.Weight != DefaultQueueWeight {
		t.Fatalf("namespace queue weight = %d, want %d", info.Weight, DefaultQueueWeight)
	}
	if info.Annotations["example.com/queue"] != "namespace" {
		t.Fatalf("namespace queue annotations were not normalized: %#v", info.Annotations)
	}
	if info.Capability.Cpu().Cmp(resource.MustParse("10")) != 0 ||
		info.Guarantee.Resource.Cpu().Cmp(resource.MustParse("2")) != 0 ||
		info.Deserved.Cpu().Cmp(resource.MustParse("6")) != 0 ||
		info.Allocated.Cpu().Cmp(resource.MustParse("3")) != 0 {
		t.Fatalf("namespace queue resource fields were not copied: %#v", info)
	}
	if !info.Reclaimable() {
		t.Fatal("expected reclaimable namespace queue")
	}
}

func TestQueueInfoClone(t *testing.T) {
	nq := &scheduling.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "training"},
		Spec: scheduling.NamespaceQueueSpec{
			Parent:     "cluster/default",
			Capability: resourceList("10"),
		},
	}
	info, err := NewNamespaceQueueInfo(nq)
	if err != nil {
		t.Fatalf("NewNamespaceQueueInfo() error = %v", err)
	}

	clone := info.Clone()
	if clone.Scope != NamespaceQueueScope || clone.Namespace != "team-a" || clone.Parent != QueueID("default") {
		t.Fatalf("clone lost queue identity: %#v", clone)
	}
	if clone.Queue != nil || clone.NamespaceQueue == nil || clone.NamespaceQueue == info.NamespaceQueue {
		t.Fatalf("clone did not preserve namespace queue source correctly")
	}
	clone.Capability[v1.ResourceCPU] = resource.MustParse("1")
	if info.Capability.Cpu().Cmp(resource.MustParse("10")) != 0 {
		t.Fatal("clone shares capability map with original")
	}
}

func TestQueueInfoCloneDoesNotShareMetadata(t *testing.T) {
	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "research",
			Annotations: map[string]string{"example.com/queue": "cluster"},
		},
		Spec: scheduling.QueueSpec{
			Affinity: &scheduling.Affinity{
				NodeGroupAffinity: &scheduling.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []string{"gpu"},
				},
			},
		},
	}

	clone := NewQueueInfo(queue).Clone()
	clone.Annotations["example.com/queue"] = "clone"
	clone.Affinity.NodeGroupAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0] = "cpu"

	if queue.Annotations["example.com/queue"] != "cluster" {
		t.Fatal("clone shares annotations with source queue")
	}
	if queue.Spec.Affinity.NodeGroupAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0] != "gpu" {
		t.Fatal("clone shares affinity with source queue")
	}
}

func TestQueueInfoIsOpen(t *testing.T) {
	if (&QueueInfo{State: scheduling.QueueStateClosed}).IsOpen() {
		t.Fatal("closed queue reported as open")
	}
	if (&QueueInfo{State: scheduling.QueueStateOpen}).IsOpen() == false {
		t.Fatal("open queue reported as closed")
	}
	var queue *QueueInfo
	if queue.IsOpen() {
		t.Fatal("nil queue reported as open")
	}
}

func TestQueueInfoReclaimableDefaultsToTrue(t *testing.T) {
	if !(&QueueInfo{}).Reclaimable() {
		t.Fatal("expected an unspecified reclaimable flag to default to true")
	}
}

func resourceList(cpu string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(cpu)}
}
