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

package util

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// fakeClaimTracker implements fwk.ResourceClaimTracker with only the methods
// exercised by the DRA preemption paths.
type fakeClaimTracker struct {
	claims      map[string]*resourceapi.ResourceClaim
	gatherState *structured.AllocatedState
	gatherErr   error
}

func (f *fakeClaimTracker) List() ([]*resourceapi.ResourceClaim, error) {
	result := make([]*resourceapi.ResourceClaim, 0, len(f.claims))
	for _, claim := range f.claims {
		result = append(result, claim)
	}
	return result, nil
}

func (f *fakeClaimTracker) Get(namespace, claimName string) (*resourceapi.ResourceClaim, error) {
	claim, found := f.claims[namespace+"/"+claimName]
	if !found {
		return nil, fmt.Errorf("resource claim %s/%s not found", namespace, claimName)
	}
	return claim, nil
}

func (f *fakeClaimTracker) ListAllAllocatedDevices() (sets.Set[structured.DeviceID], error) {
	return sets.New[structured.DeviceID](), nil
}

func (f *fakeClaimTracker) GatherAllocatedState() (*structured.AllocatedState, error) {
	if f.gatherErr != nil {
		return nil, f.gatherErr
	}
	return f.gatherState, nil
}

func (f *fakeClaimTracker) AssumeClaimAfterAPICall(*resourceapi.ResourceClaim) error { return nil }
func (f *fakeClaimTracker) SignalClaimPendingAllocation(types.UID, *resourceapi.ResourceClaim) error {
	return nil
}
func (f *fakeClaimTracker) GetPendingAllocation(types.UID) *resourceapi.AllocationResult {
	return nil
}
func (f *fakeClaimTracker) MaybeRemoveClaimPendingAllocation(types.UID, bool) bool { return false }
func (f *fakeClaimTracker) AssumedClaimRestore(string, string)                     {}

// fakeSliceLister implements fwk.ResourceSliceLister.
type fakeSliceLister struct{}

func (f *fakeSliceLister) ListWithDeviceTaintRules() ([]*resourceapi.ResourceSlice, error) {
	return nil, nil
}

// fakeClassLister implements fwk.DeviceClassLister.
type fakeClassLister struct{}

func (f *fakeClassLister) List() ([]*resourceapi.DeviceClass, error) { return nil, nil }
func (f *fakeClassLister) Get(string) (*resourceapi.DeviceClass, error) {
	return nil, fmt.Errorf("not found")
}

// fakeDRAManager implements fwk.SharedDRAManager.
type fakeDRAManager struct {
	claims  *fakeClaimTracker
	slices  *fakeSliceLister
	classes *fakeClassLister
}

func (f *fakeDRAManager) ResourceClaims() fwk.ResourceClaimTracker { return f.claims }
func (f *fakeDRAManager) ResourceSlices() fwk.ResourceSliceLister  { return f.slices }
func (f *fakeDRAManager) DeviceClasses() fwk.DeviceClassLister     { return f.classes }
func (f *fakeDRAManager) DeviceClassResolver() fwk.DeviceClassResolver {
	return nil
}
func (f *fakeDRAManager) PodGroups() fwk.PodGroupLister { return nil }

func newFakeDRAManager(claims map[string]*resourceapi.ResourceClaim) *fakeDRAManager {
	return &fakeDRAManager{
		claims:  &fakeClaimTracker{claims: claims, gatherState: &structured.AllocatedState{}},
		slices:  &fakeSliceLister{},
		classes: &fakeClassLister{},
	}
}

// newAllocatedClaim builds an allocated claim whose node selector (if any) matches the
// given topology label.
func newAllocatedClaim(namespace, name string, reservedFor []types.UID, matchLabels map[string]string) *resourceapi.ResourceClaim {
	refs := make([]resourceapi.ResourceClaimConsumerReference, 0, len(reservedFor))
	for _, uid := range reservedFor {
		refs = append(refs, resourceapi.ResourceClaimConsumerReference{UID: uid})
	}
	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{},
		},
	}
	if matchLabels != nil {
		claim.Status.Allocation.NodeSelector = &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{{
					Key:      "topology",
					Operator: v1.NodeSelectorOpIn,
					Values:   []string{matchLabels["topology"]},
				}},
			}},
		}
	}
	claim.Status.ReservedFor = refs
	return claim
}

func newDRATestTask(uid types.UID, claimKeys ...string) *api.TaskInfo {
	return &api.TaskInfo{
		Namespace:         "default",
		Name:              string(uid),
		Pod:               &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: string(uid), UID: uid}},
		ResourceClaimKeys: claimKeys,
	}
}

func newDRATestNode(name string, labels map[string]string) *api.NodeInfo {
	return &api.NodeInfo{
		Name: name,
		Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}},
	}
}

func TestIdleCheckNilManager(t *testing.T) {
	task := newDRATestTask(types.UID("t1"), "default/claim1")
	results, err := IdleCheck(context.TODO(), task, newDRATestNode("n1", nil), nil)
	if err != nil || results != nil {
		t.Fatalf("nil manager must degrade to a pass-through, got results=%v err=%v", results, err)
	}
}

func TestIdleCheckErrorPaths(t *testing.T) {
	t.Run("gather fails", func(t *testing.T) {
		manager := newFakeDRAManager(nil)
		manager.claims.gatherErr = fmt.Errorf("tracker unavailable")
		_, err := IdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/claim1"), newDRATestNode("n1", nil), manager)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("gather returns nil state", func(t *testing.T) {
		manager := newFakeDRAManager(nil)
		manager.claims.gatherState = nil
		_, err := IdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/claim1"), newDRATestNode("n1", nil), manager)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("claim does not exist", func(t *testing.T) {
		manager := newFakeDRAManager(map[string]*resourceapi.ResourceClaim{})
		_, err := IdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/missing"), newDRATestNode("n1", nil), manager)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("claim allocated on another node", func(t *testing.T) {
		claim := newAllocatedClaim("default", "claim1", nil, map[string]string{"topology": "rack2"})
		manager := newFakeDRAManager(map[string]*resourceapi.ResourceClaim{"default/claim1": claim})
		_, err := IdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/claim1"),
			newDRATestNode("n1", map[string]string{"topology": "rack1"}), manager)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("allocated claims pass without touching the allocator", func(t *testing.T) {
		claim := newAllocatedClaim("default", "claim1", nil, map[string]string{"topology": "rack1"})
		manager := newFakeDRAManager(map[string]*resourceapi.ResourceClaim{"default/claim1": claim})
		task := newDRATestTask(types.UID("t1"), "default/claim1")
		results, err := IdleCheck(context.TODO(), task, newDRATestNode("n1", map[string]string{"topology": "rack1"}), manager)
		if err != nil {
			t.Fatalf("expected the check to pass, got %v", err)
		}
		if results != nil {
			t.Fatalf("allocated claims need no re-allocation, expected nil results, got %v", results)
		}
		if task.DRAPreAlloc != nil {
			t.Fatal("allocator was not invoked, expected DRAPreAlloc to stay nil")
		}
	})
}

func TestFutureIdleCheck(t *testing.T) {
	t.Run("allocated claims pass with victims carrying nil devices", func(t *testing.T) {
		claim := newAllocatedClaim("default", "claim1", nil, map[string]string{"topology": "rack1"})
		manager := newFakeDRAManager(map[string]*resourceapi.ResourceClaim{"default/claim1": claim})
		node := newDRATestNode("n1", map[string]string{"topology": "rack1"})
		node.DraReleasing = api.NewDRADeviceState()
		node.DraPipelined = api.NewDRADeviceState()
		victim := newDRATestTask(types.UID("victim"))
		results, err := FutureIdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/claim1"), node, manager, []*api.TaskInfo{victim})
		if err != nil {
			t.Fatalf("expected the check to pass, got %v", err)
		}
		if results != nil {
			t.Fatalf("allocated claims need no re-allocation, expected nil results, got %v", results)
		}
	})

	t.Run("claim does not exist", func(t *testing.T) {
		manager := newFakeDRAManager(map[string]*resourceapi.ResourceClaim{})
		node := newDRATestNode("n1", nil)
		node.DraReleasing = api.NewDRADeviceState()
		node.DraPipelined = api.NewDRADeviceState()
		_, err := FutureIdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/missing"), node, manager, nil)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("nil manager degrades to a pass-through", func(t *testing.T) {
		results, err := FutureIdleCheck(context.TODO(), newDRATestTask(types.UID("t1"), "default/claim1"), newDRATestNode("n1", nil), nil, nil)
		if err != nil || results != nil {
			t.Fatalf("nil manager must degrade to a pass-through, got results=%v err=%v", results, err)
		}
	})
}

func TestCheckSharedClaim(t *testing.T) {
	evicteeUID := types.UID("evictee")

	tests := []struct {
		name     string
		claims   map[string]*resourceapi.ResourceClaim
		task     *api.TaskInfo
		expected bool
	}{
		{
			name:     "task without claims",
			claims:   map[string]*resourceapi.ResourceClaim{},
			task:     newDRATestTask(evicteeUID),
			expected: false,
		},
		{
			name:     "claim missing from the tracker",
			claims:   map[string]*resourceapi.ResourceClaim{},
			task:     newDRATestTask(evicteeUID, "default/missing"),
			expected: false,
		},
		{
			name: "claim not allocated yet",
			claims: map[string]*resourceapi.ResourceClaim{
				"default/pending": {ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "pending"}},
			},
			task:     newDRATestTask(evicteeUID, "default/pending"),
			expected: false,
		},
		{
			name: "claim held exclusively",
			claims: map[string]*resourceapi.ResourceClaim{
				"default/exclusive": newAllocatedClaim("default", "exclusive", []types.UID{evicteeUID}, nil),
			},
			task:     newDRATestTask(evicteeUID, "default/exclusive"),
			expected: false,
		},
		{
			name: "claim shared with another pod",
			claims: map[string]*resourceapi.ResourceClaim{
				"default/shared": newAllocatedClaim("default", "shared", []types.UID{evicteeUID, types.UID("task-2")}, nil),
			},
			task:     newDRATestTask(evicteeUID, "default/shared"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckSharedClaim(tt.task, newFakeDRAManager(tt.claims)); got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestClaimAvailableOnNode(t *testing.T) {
	node := newDRATestNode("n1", map[string]string{"topology": "rack1"}).Node

	buildClaim := func(values []string, operator v1.NodeSelectorOperator) *resourceapi.ResourceClaim {
		return &resourceapi.ResourceClaim{
			Status: resourceapi.ResourceClaimStatus{
				Allocation: &resourceapi.AllocationResult{
					NodeSelector: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{{
							MatchExpressions: []v1.NodeSelectorRequirement{{
								Key: "topology", Operator: operator, Values: values,
							}},
						}},
					},
				},
			},
		}
	}

	tests := []struct {
		name     string
		claim    *resourceapi.ResourceClaim
		expected bool
	}{
		{name: "unallocated claim is available", claim: &resourceapi.ResourceClaim{}, expected: true},
		{name: "allocation without node selector is available", claim: newAllocatedClaim("d", "c", nil, nil), expected: true},
		{name: "selector matches", claim: buildClaim([]string{"rack1"}, v1.NodeSelectorOpIn), expected: true},
		{name: "selector does not match", claim: buildClaim([]string{"rack2"}, v1.NodeSelectorOpIn), expected: false},
		{name: "invalid selector is unavailable", claim: buildClaim([]string{"rack1"}, "InvalidOp"), expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claimAvailableOnNode(tt.claim, node); got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
