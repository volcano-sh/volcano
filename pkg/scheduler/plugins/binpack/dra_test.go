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

package binpack

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestAggregateDRANodeResources(t *testing.T) {
	const (
		gpuDriver = "gpu.example.com"
		nicDriver = "nic.example.com"
		gpuClass  = "gpu"
		nicClass  = "nic"
	)

	deviceClasses := []*resourcev1.DeviceClass{
		deviceClass(gpuClass, `device.driver == "gpu.example.com"`),
		deviceClass(nicClass, `device.driver == "nic.example.com"`),
	}
	resourceSlices := []*resourcev1.ResourceSlice{
		resourceSlice("node-a-gpus", gpuDriver, "gpu-pool", ptr.To("node-a"), nil,
			device("gpu-0", nil), device("gpu-1", nil)),
		resourceSlice("node-b-gpus", gpuDriver, "gpu-pool", ptr.To("node-b"), nil,
			device("gpu-0", nil)),
		resourceSlice("node-a-nics", nicDriver, "nic-pool", ptr.To("node-a"), nil,
			device("nic-0", nil)),
		// Non-node-local resources are deliberately excluded in the first phase.
		resourceSlice("all-node-gpus", gpuDriver, "shared-pool", nil, ptr.To(true),
			device("shared-gpu", nil)),
		// Exact per-device NodeName is supported.
		resourceSlice("partitioned-gpus", gpuDriver, "partitioned-pool", nil, nil,
			device("gpu-a", ptr.To("node-a")), device("gpu-b", ptr.To("node-b"))),
	}
	resourceSlices[len(resourceSlices)-1].Spec.PerDeviceNodeSelection = ptr.To(true)

	allocatedState := &structured.AllocatedState{
		AllocatedDevices: sets.New(
			structured.MakeDeviceID(gpuDriver, "gpu-pool", "gpu-1"),
			structured.MakeDeviceID(nicDriver, "nic-pool", "nic-0"),
			structured.MakeDeviceID(gpuDriver, "partitioned-pool", "gpu-b"),
		),
		AllocatedSharedDeviceIDs: sets.New[structured.SharedDeviceID](),
	}

	inventory := newDRAInventoryFromSnapshot(deviceClasses, resourceSlices)
	got, err := inventory.nodeResources(context.Background(), sets.New(gpuClass, nicClass), allocatedState)
	if err != nil {
		t.Fatalf("aggregate DRA node resources: %v", err)
	}

	want := draNodeResources{
		"node-a": {
			gpuClass: draCounts(3, 1),
			nicClass: draCounts(1, 1),
		},
		"node-b": {
			gpuClass: draCounts(2, 1),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DRA node resources:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestAggregateDRANodeResourcesRejectsInvalidSelector(t *testing.T) {
	classes := []*resourcev1.DeviceClass{deviceClass("invalid", `device.driver ==`)}
	slices := []*resourcev1.ResourceSlice{
		resourceSlice("gpu", "gpu.example.com", "pool", ptr.To("node-a"), nil, device("gpu-0", nil)),
	}

	inventory := newDRAInventoryFromSnapshot(classes, slices)
	if _, err := inventory.nodeResources(context.Background(), sets.New("invalid"), nil); err == nil {
		t.Fatal("expected invalid DeviceClass selector to fail")
	}
}

func TestDRAInventoryIgnoresUnrequestedInvalidDeviceClass(t *testing.T) {
	const gpuClass = "gpu"
	classes := []*resourcev1.DeviceClass{
		deviceClass(gpuClass, `device.driver == "gpu.example.com"`),
		deviceClass("invalid", `device.attributes["no-such-domain"].noSuchAttr.isGreaterThan(quantity("0"))`),
	}
	slices := []*resourcev1.ResourceSlice{
		resourceSlice("gpu", "gpu.example.com", "pool", ptr.To("node-a"), nil, device("gpu-0", nil)),
	}

	inventory := newDRAInventoryFromSnapshot(classes, slices)
	got, err := inventory.nodeResources(context.Background(), sets.New(gpuClass), nil)
	if err != nil {
		t.Fatalf("score requested DeviceClass: %v", err)
	}
	want := draNodeResources{"node-a": {gpuClass: draCounts(1, 0)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected DRA node resources: got %#v, want %#v", got, want)
	}
	if _, found := inventory.classMatches["invalid"]; found {
		t.Fatal("unrequested DeviceClass was evaluated")
	}
}

func TestDRAInventoryReusesAndInvalidatesDeviceClassMatches(t *testing.T) {
	const gpuClass = "gpu"
	class := deviceClass(gpuClass, `device.driver == "gpu.example.com"`)
	slices := []*resourcev1.ResourceSlice{
		resourceSlice("gpu", "gpu.example.com", "pool", ptr.To("node-a"), nil, device("gpu-0", nil)),
	}
	inventory := newDRAInventoryFromSnapshot(
		[]*resourcev1.DeviceClass{class},
		slices,
	)

	first, err := inventory.nodeResources(context.Background(), sets.New(gpuClass), nil)
	if err != nil {
		t.Fatalf("build initial inventory: %v", err)
	}
	want := draNodeResources{"node-a": {gpuClass: draCounts(1, 0)}}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected initial DRA node resources: got %#v, want %#v", first, want)
	}
	cachedMatch := inventory.classMatches[gpuClass]
	celCache := inventory.celCache
	inventory.refreshFromSnapshot([]*resourcev1.DeviceClass{class.DeepCopy()}, []*resourcev1.ResourceSlice{slices[0].DeepCopy()})
	if inventory.classMatches[gpuClass] != cachedMatch {
		t.Fatal("unchanged DRA objects invalidated the device/class matching cache")
	}

	class.Spec.Selectors[0].CEL.Expression = `device.driver ==`
	inventory.refreshFromSnapshot([]*resourcev1.DeviceClass{class}, slices)
	if inventory.celCache != celCache {
		t.Fatal("DRA object refresh replaced the session-scoped CEL cache")
	}
	if _, found := inventory.classMatches[gpuClass]; found {
		t.Fatal("changed DeviceClass did not invalidate the device/class matching cache")
	}
	if _, err := inventory.nodeResources(context.Background(), sets.New(gpuClass), nil); err == nil {
		t.Fatal("expected refreshed inventory to observe the changed DeviceClass")
	}
}

func TestDRAInventoryInvalidatesOnResourceSliceChange(t *testing.T) {
	const gpuClass = "gpu"
	class := deviceClass(gpuClass, `device.driver == "gpu.example.com"`)
	slice := resourceSlice("gpu", "gpu.example.com", "pool", ptr.To("node-a"), nil, device("gpu-0", nil))
	inventory := newDRAInventoryFromSnapshot([]*resourcev1.DeviceClass{class}, []*resourcev1.ResourceSlice{slice})

	first, err := inventory.nodeResources(context.Background(), sets.New(gpuClass), nil)
	if err != nil {
		t.Fatalf("build initial inventory: %v", err)
	}
	wantFirst := draNodeResources{"node-a": {gpuClass: draCounts(1, 0)}}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("unexpected initial DRA node resources: got %#v, want %#v", first, wantFirst)
	}

	updatedSlice := slice.DeepCopy()
	updatedSlice.Spec.Devices = append(updatedSlice.Spec.Devices, device("gpu-1", nil))
	inventory.refreshFromSnapshot([]*resourcev1.DeviceClass{class}, []*resourcev1.ResourceSlice{updatedSlice})
	second, err := inventory.nodeResources(context.Background(), sets.New(gpuClass), nil)
	if err != nil {
		t.Fatalf("build refreshed inventory: %v", err)
	}
	wantSecond := draNodeResources{"node-a": {gpuClass: draCounts(2, 0)}}
	if !reflect.DeepEqual(second, wantSecond) {
		t.Fatalf("unexpected refreshed DRA node resources: got %#v, want %#v", second, wantSecond)
	}
}

func TestBinPackingScoreWithDRA(t *testing.T) {
	task := &api.TaskInfo{
		Namespace: "default",
		Name:      "dra-pod",
		Resreq:    api.EmptyResource(),
		DRAResreq: map[string]*api.DRAResource{
			"gpu": {Count: 1},
		},
	}
	node := &api.NodeInfo{Name: "node-a"}
	weight := priorityWeight{BinPackingWeight: 1}

	tests := []struct {
		name      string
		resources map[string]*draNodeResource
		want      float64
	}{
		{
			name: "prefer a fuller node",
			resources: map[string]*draNodeResource{
				"gpu": draCounts(4, 2),
			},
			want: 75,
		},
		{
			name: "score a less utilized node lower",
			resources: map[string]*draNodeResource{
				"gpu": draCounts(4, 0),
			},
			want: 25,
		},
		{
			name: "reject insufficient devices",
			resources: map[string]*draNodeResource{
				"gpu": draCounts(1, 1),
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := binPackingScore(task, node, weight, task.DRAResreq, tt.resources); got != tt.want {
				t.Fatalf("unexpected score: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBinPackingScorePreservesLegacyScaling(t *testing.T) {
	task := &api.TaskInfo{Resreq: api.NewResource(api.BuildResourceList("1", "0"))}
	node := &api.NodeInfo{
		Allocatable: api.NewResource(api.BuildResourceList("4", "0")),
		Used:        api.NewResource(api.BuildResourceList("1", "0")),
	}
	weight := priorityWeight{
		BinPackingWeight: 2,
		BinPackingResources: map[v1.ResourceName]int{
			v1.ResourceCPU: 1,
		},
	}

	if got, want := BinPackingScore(task, node, weight), float64(100); got != want {
		t.Fatalf("unexpected legacy score: got %v, want %v", got, want)
	}
}

func TestBatchNodeOrderRefreshesDRAStatePerTask(t *testing.T) {
	const (
		driver   = "gpu.example.com"
		gpuClass = "gpu"
	)
	sharedClaimUID := types.UID("shared-claim-uid")
	testCase := &uthelper.TestCommonStruct{
		Plugins: map[string]framework.PluginBuilder{PluginName: New},
		Nodes: []*v1.Node{
			util.BuildNode("node-a", api.BuildResourceList("4", "8Gi"), nil),
			util.BuildNode("node-b", api.BuildResourceList("4", "8Gi"), nil),
		},
		DeviceClasses: []*resourcev1.DeviceClass{
			deviceClass(gpuClass, `device.driver == "gpu.example.com"`),
			deviceClass("unrequested-invalid", `device.attributes["no-such-domain"].noSuchAttr.isGreaterThan(quantity("0"))`),
		},
		ResourceSlices: []*resourcev1.ResourceSlice{
			resourceSlice("node-a-gpus", driver, "pool-a", ptr.To("node-a"), nil,
				device("gpu-a0", nil), device("gpu-a1", nil), device("gpu-a2", nil), device("gpu-a3", nil)),
			resourceSlice("node-b-gpus", driver, "pool-b", ptr.To("node-b"), nil,
				device("gpu-b0", nil), device("gpu-b1", nil), device("gpu-b2", nil), device("gpu-b3", nil)),
		},
		ResourceClaims: []*resourcev1.ResourceClaim{{
			ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default", UID: sharedClaimUID},
		}},
	}
	enabled := true
	ssn := testCase.RegisterSession([]conf.Tier{{Plugins: []conf.PluginOption{{
		Name:             PluginName,
		EnabledNodeOrder: &enabled,
		Arguments: framework.Arguments{
			"binpack.weight":                1,
			DynamicResourceAllocationEnable: true,
		},
	}}}}, nil)
	defer testCase.Close()
	nodes := []*api.NodeInfo{ssn.Nodes["node-a"], ssn.Nodes["node-b"]}

	task := &api.TaskInfo{
		UID:       "task",
		Namespace: "default",
		Name:      "task",
		Resreq:    api.EmptyResource(),
		DRAResreq: map[string]*api.DRAResource{gpuClass: {Count: 1}},
	}
	assertNodeScores(t, ssn, task, nodes, map[string]float64{"node-a": 25, "node-b": 25})

	allocatedClaim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default", UID: sharedClaimUID},
		Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{
			Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{{
				Request: "gpu", Driver: driver, Pool: "pool-a", Device: "gpu-a0",
			}}},
		}},
	}
	tracker := ssn.SharedDRAManager().ResourceClaims()
	if err := tracker.SignalClaimPendingAllocation(sharedClaimUID, allocatedClaim); err != nil {
		t.Fatalf("signal pending DRA allocation: %v", err)
	}
	defer tracker.MaybeRemoveClaimPendingAllocation(sharedClaimUID, true)

	// A new task sees the in-flight allocation made after the previous scoring cycle.
	assertNodeScores(t, ssn, task, nodes, map[string]float64{"node-a": 50, "node-b": 25})

	sharedTask := &api.TaskInfo{
		UID:                    "shared-task",
		Namespace:              "default",
		Name:                   "shared-task",
		Resreq:                 api.EmptyResource(),
		DRAResreq:              map[string]*api.DRAResource{gpuClass: {Count: 1}},
		ResourceClaimKeys:      []string{"default/shared"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{"default/shared": {gpuClass: {Count: 1}}},
	}
	// The already allocated shared claim adds no incremental request.
	assertNodeScores(t, ssn, sharedTask, nodes, map[string]float64{"node-a": 0, "node-b": 0})
}

func assertNodeScores(
	t *testing.T,
	ssn *framework.Session,
	task *api.TaskInfo,
	nodes []*api.NodeInfo,
	want map[string]float64,
) {
	t.Helper()
	got, err := ssn.BatchNodeOrderFn(task, nodes)
	if err != nil {
		t.Fatalf("score nodes: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected node scores: got %#v, want %#v", got, want)
	}
}

func TestIncrementalTaskDRA(t *testing.T) {
	task := &api.TaskInfo{
		DRAResreq: map[string]*api.DRAResource{"gpu": {Count: 3}},
		ResourceClaimKeys: []string{
			"default/allocated", "default/pending",
		},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"default/allocated": {"gpu": {Count: 1}},
			"default/pending":   {"gpu": {Count: 2}},
		},
	}

	got := incrementalTaskDRA(task, sets.New("default/allocated"))
	want := map[string]*api.DRAResource{"gpu": {Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected incremental DRA request: got %#v, want %#v", got, want)
	}
}

func deviceClass(name, expression string) *resourcev1.DeviceClass {
	return &resourcev1.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourcev1.DeviceClassSpec{
			Selectors: []resourcev1.DeviceSelector{{
				CEL: &resourcev1.CELDeviceSelector{Expression: expression},
			}},
		},
	}
}

func resourceSlice(
	name, driver, pool string,
	nodeName *string,
	allNodes *bool,
	devices ...resourcev1.Device,
) *resourcev1.ResourceSlice {
	return &resourcev1.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourcev1.ResourceSliceSpec{
			Driver:   driver,
			Pool:     resourcev1.ResourcePool{Name: pool},
			NodeName: nodeName,
			AllNodes: allNodes,
			Devices:  devices,
		},
	}
}

func device(name string, nodeName *string) resourcev1.Device {
	return resourcev1.Device{Name: name, NodeName: nodeName}
}

func draCounts(allocatable, used int64) *draNodeResource {
	return &draNodeResource{
		Allocatable: &api.DRAResource{Count: allocatable},
		Used:        &api.DRAResource{Count: used},
	}
}
