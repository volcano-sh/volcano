/*
Copyright 2018-2025 The Volcano Authors.

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

package proportion

import (
	"math"
	"math/rand"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const gpuResource = apiv1.ResourceName("nvidia.com/gpu")

func TestFillDimension(t *testing.T) {
	tests := []struct {
		name                string
		weights, floors, up []float64
		pool                float64
		want                []float64
	}{
		{
			name:    "no bound is active: plain weighted split",
			weights: []float64{1, 1, 1}, floors: []float64{0, 0, 0}, up: []float64{12, 12, 12},
			pool: 12, want: []float64{4, 4, 4},
		},
		{
			name:    "floor above the weighted share is kept and the rest is rebalanced",
			weights: []float64{1, 1, 1}, floors: []float64{8, 0, 0}, up: []float64{12, 12, 12},
			pool: 12, want: []float64{8, 2, 2},
		},
		{
			name:    "floor below the weighted share does not add a bonus",
			weights: []float64{1, 1, 1}, floors: []float64{5, 0, 0}, up: []float64{30, 30, 30},
			pool: 30, want: []float64{10, 10, 10},
		},
		{
			name:    "capped peers release their surplus to the guaranteed queue",
			weights: []float64{1, 1, 1}, floors: []float64{8, 0, 0}, up: []float64{12, 1, 1},
			pool: 12, want: []float64{10, 1, 1},
		},
		{
			name:    "floor is released once the water level rises above it",
			weights: []float64{1, 1, 1}, floors: []float64{5, 0, 0}, up: []float64{12, 1, 1},
			pool: 12, want: []float64{10, 1, 1},
		},
		{
			name:    "weights are respected above the floor",
			weights: []float64{1, 2}, floors: []float64{4, 0}, up: []float64{12, 12},
			pool: 12, want: []float64{4, 8},
		},
		{
			name:    "floors alone use the whole pool",
			weights: []float64{1, 1}, floors: []float64{6, 6}, up: []float64{8, 8},
			pool: 12, want: []float64{6, 6},
		},
		{
			name:    "demand below the pool leaves the rest unallocated",
			weights: []float64{1, 1}, floors: []float64{0, 0}, up: []float64{2, 3},
			pool: 12, want: []float64{2, 3},
		},
		{
			name:    "zero weight queue only keeps its floor",
			weights: []float64{0, 1}, floors: []float64{2, 0}, up: []float64{10, 10},
			pool: 8, want: []float64{2, 6},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fillDimension(tt.weights, tt.floors, tt.up, tt.pool)
			for i := range tt.want {
				if math.Abs(got[i]-tt.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestFillDimensionProperties checks the invariants of a weighted max-min fair
// allocation on random inputs.
func TestFillDimensionProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(6048))
	for iteration := 0; iteration < 2000; iteration++ {
		n := 1 + rng.Intn(6)
		weights, floors, caps := make([]float64, n), make([]float64, n), make([]float64, n)
		floorSum := 0.0
		for i := 0; i < n; i++ {
			weights[i] = float64(1 + rng.Intn(5))
			floors[i] = float64(rng.Intn(10))
			caps[i] = floors[i] + float64(rng.Intn(20))
			floorSum += floors[i]
		}
		pool := floorSum + float64(rng.Intn(60))

		got := fillDimension(weights, floors, caps, pool)

		sum, capSum := 0.0, 0.0
		for i := 0; i < n; i++ {
			if got[i] < floors[i]-1e-6 || got[i] > caps[i]+1e-6 {
				t.Fatalf("iteration %d: x[%d]=%v outside [%v, %v]", iteration, i, got[i], floors[i], caps[i])
			}
			sum += got[i]
			capSum += caps[i]
		}
		if want := math.Min(pool, capSum); math.Abs(sum-want) > 1e-6 {
			t.Fatalf("iteration %d: allocated %v, want %v (weights=%v floors=%v caps=%v pool=%v)", iteration, sum, want, weights, floors, caps, pool)
		}
		// Max-min fairness: a queue above its floor never has a higher normalized
		// share than a queue that could still take more.
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if got[i] > floors[i]+1e-6 && got[j] < caps[j]-1e-6 && got[i]/weights[i] > got[j]/weights[j]+1e-6 {
					t.Fatalf("iteration %d: queue %d (%v) is above its floor with a higher share than queue %d (%v)", iteration, i, got, j, got)
				}
			}
		}
	}
}

// cleanupQueueMetrics removes the queue metrics a test creates. They live in the
// process-wide registry and tests that sum them over all queues must not see them.
func cleanupQueueMetrics(t *testing.T, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			metrics.DeleteQueueMetrics(name)
		}
	})
}

type deservedQueue struct {
	name       string
	weight     int32
	guarantee  apiv1.ResourceList
	capability apiv1.ResourceList
	// request is the resource request of the only pod of the queue; nil means the queue has no job.
	request apiv1.ResourceList
}

func cpuList(cpu string) apiv1.ResourceList {
	return apiv1.ResourceList{apiv1.ResourceCPU: resource.MustParse(cpu)}
}

func memList(mem string) apiv1.ResourceList {
	return apiv1.ResourceList{apiv1.ResourceMemory: resource.MustParse(mem)}
}

func gpuList(gpu string) apiv1.ResourceList {
	return apiv1.ResourceList{gpuResource: resource.MustParse(gpu)}
}

func mergeLists(lists ...apiv1.ResourceList) apiv1.ResourceList {
	merged := apiv1.ResourceList{}
	for _, list := range lists {
		for name, quantity := range list {
			merged[name] = quantity
		}
	}
	return merged
}

// deservedOf opens a session for the given cluster and queues and returns the
// deserved resource of every queue that has a job.
func deservedOf(t *testing.T, nodeResource apiv1.ResourceList, queues []deservedQueue) map[string]*api.Resource {
	t.Helper()
	fixture := uthelper.TestCommonStruct{
		Nodes: []*apiv1.Node{util.BuildNode("n1", nodeResource, nil)},
	}
	for _, q := range queues {
		cleanupQueueMetrics(t, q.name)
		queue := util.BuildQueue(q.name, q.weight, q.capability)
		queue.Spec.Guarantee.Resource = q.guarantee
		fixture.Queues = append(fixture.Queues, queue)
		if q.request == nil {
			continue
		}
		fixture.PodGroups = append(fixture.PodGroups, util.BuildPodGroup(q.name, "ns1", q.name, 1, nil, schedulingv1beta1.PodGroupInqueue))
		fixture.Pods = append(fixture.Pods, util.BuildPod("ns1", q.name, "", apiv1.PodPending, q.request, q.name, nil, nil))
	}
	ssn := fixture.RegisterSession(nil, nil)
	defer fixture.Close()

	// The result must not depend on Go's randomized map iteration order.
	var first map[string]*api.Resource
	for iteration := 0; iteration < 16; iteration++ {
		plugin := New(nil).(*proportionPlugin)
		plugin.OnSessionOpen(ssn)
		got := map[string]*api.Resource{}
		for _, q := range queues {
			if attr := plugin.queueOpts[api.QueueID(q.name)]; attr != nil {
				got[q.name] = attr.deserved.Clone()
			}
		}
		plugin.OnSessionClose(ssn)
		if first == nil {
			first = got
			continue
		}
		for name, res := range got {
			if !res.Equal(first[name], api.Zero) {
				t.Fatalf("queue %s deserved is not deterministic: %v vs %v", name, first[name], res)
			}
		}
	}
	return first
}

func requireDeserved(t *testing.T, deserved map[string]*api.Resource, queue string, dim apiv1.ResourceName, want float64) {
	t.Helper()
	res, ok := deserved[queue]
	if !ok {
		t.Fatalf("queue %s has no deserved resource", queue)
	}
	if got := res.Get(dim); math.Abs(got-want) > 0.5 {
		t.Fatalf("queue %s deserved %s = %v, want %v (deserved: %v)", queue, dim, got, want, res)
	}
}

const (
	gi = float64(1 << 30)
	// the cpu dimension is tracked in milli cores
	core = float64(1000)
)

func TestDeservedWithGuarantee(t *testing.T) {
	full := func(cpu, mem string) apiv1.ResourceList {
		return mergeLists(cpuList(cpu), memList(mem))
	}
	tests := []struct {
		name   string
		node   apiv1.ResourceList
		queues []deservedQueue
		// want[queue][dimension] is the expected deserved value
		want map[string]map[apiv1.ResourceName]float64
	}{
		{
			name: "cpu-only guarantee does not change how memory is shared",
			node: api.BuildResourceList("12", "30Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), request: full("12", "30Gi")},
				{name: "gq2", weight: 1, request: full("12", "30Gi")},
				{name: "gq3", weight: 1, request: full("12", "30Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 8 * core, apiv1.ResourceMemory: 10 * gi},
				"gq2": {apiv1.ResourceCPU: 2 * core, apiv1.ResourceMemory: 10 * gi},
				"gq3": {apiv1.ResourceCPU: 2 * core, apiv1.ResourceMemory: 10 * gi},
			},
		},
		{
			name: "memory-only guarantee does not change how cpu is shared",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: memList("8Gi"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("12", "12Gi")},
				{name: "gq3", weight: 1, request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 4 * core, apiv1.ResourceMemory: 8 * gi},
				"gq2": {apiv1.ResourceCPU: 4 * core, apiv1.ResourceMemory: 2 * gi},
				"gq3": {apiv1.ResourceCPU: 4 * core, apiv1.ResourceMemory: 2 * gi},
			},
		},
		{
			name: "queue pinned at its guarantee still takes what capped peers leave unused",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("1", "1Gi")},
				{name: "gq3", weight: 1, request: full("1", "1Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 10 * core},
				"gq2": {apiv1.ResourceCPU: 1 * core},
				"gq3": {apiv1.ResourceCPU: 1 * core},
			},
		},
		{
			name: "guarantee is not pinned when peers are capped and the real share is higher",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("5"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("1", "1Gi")},
				{name: "gq3", weight: 1, request: full("1", "1Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 10 * core},
			},
		},
		{
			name: "guaranteed queue takes the whole cluster when its peers have no demand",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("0", "0")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 12 * core},
				"gq2": {apiv1.ResourceCPU: 0},
			},
		},
		{
			name: "capability is reachable above the guarantee",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), capability: cpuList("9"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("1", "1Gi")},
				{name: "gq3", weight: 1, request: full("1", "1Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 9 * core},
			},
		},
		{
			name: "each dimension is evaluated on its own",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				// cpu guarantee is above the weighted share, memory guarantee is below it
				{name: "gq1", weight: 1, guarantee: full("8", "2Gi"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("12", "12Gi")},
				{name: "gq3", weight: 1, request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 8 * core, apiv1.ResourceMemory: 4 * gi},
				"gq2": {apiv1.ResourceCPU: 2 * core, apiv1.ResourceMemory: 4 * gi},
				"gq3": {apiv1.ResourceCPU: 2 * core, apiv1.ResourceMemory: 4 * gi},
			},
		},
		{
			name: "already covered guarantee keeps the weight based share",
			node: api.BuildResourceList("30", "30Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("5"), request: full("30", "30Gi")},
				{name: "gq2", weight: 1, request: full("30", "30Gi")},
				{name: "gq3", weight: 1, request: full("30", "30Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 10 * core},
				"gq2": {apiv1.ResourceCPU: 10 * core},
				"gq3": {apiv1.ResourceCPU: 10 * core},
			},
		},
		{
			name: "weights are respected on top of the guarantee",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("4"), request: full("12", "12Gi")},
				{name: "gq2", weight: 2, request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 4 * core},
				"gq2": {apiv1.ResourceCPU: 8 * core},
			},
		},
		{
			name: "guarantees above the cluster capacity are scaled down",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, guarantee: cpuList("8"), request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 6 * core},
				"gq2": {apiv1.ResourceCPU: 6 * core},
			},
		},
		{
			name: "guarantee of a queue without jobs stays reserved",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gidle", weight: 1, guarantee: cpuList("8")},
				{name: "gq1", weight: 1, request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 2 * core},
				"gq2": {apiv1.ResourceCPU: 2 * core},
			},
		},
		{
			name: "guarantee is limited by the capability in the same way as the share",
			node: api.BuildResourceList("12", "12Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: cpuList("8"), capability: cpuList("4"), request: full("12", "12Gi")},
				{name: "gq2", weight: 1, request: full("12", "12Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 4 * core},
			},
		},
		{
			name: "gpu-only guarantee keeps cpu and memory of the queue",
			node: mergeLists(api.BuildResourceList("32", "32Gi"), gpuList("12")),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: gpuList("8"), request: mergeLists(full("32", "32Gi"), gpuList("12"))},
				{name: "gq2", weight: 1, request: mergeLists(full("32", "32Gi"), gpuList("12"))},
				{name: "gq3", weight: 1, request: mergeLists(full("32", "32Gi"), gpuList("12"))},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 32 * core / 3, apiv1.ResourceMemory: 32 * gi / 3, gpuResource: 8 * core},
				"gq2": {apiv1.ResourceCPU: 32 * core / 3, apiv1.ResourceMemory: 32 * gi / 3, gpuResource: 2 * core},
				"gq3": {apiv1.ResourceCPU: 32 * core / 3, apiv1.ResourceMemory: 32 * gi / 3, gpuResource: 2 * core},
			},
		},
		{
			name: "guarantee on a resource the cluster does not have does not zero the queue",
			node: api.BuildResourceList("32", "32Gi"),
			queues: []deservedQueue{
				{name: "gq1", weight: 1, guarantee: gpuList("4"), request: full("32", "32Gi")},
				{name: "gq2", weight: 1, request: full("32", "32Gi")},
			},
			want: map[string]map[apiv1.ResourceName]float64{
				"gq1": {apiv1.ResourceCPU: 16 * core, apiv1.ResourceMemory: 16 * gi},
				"gq2": {apiv1.ResourceCPU: 16 * core, apiv1.ResourceMemory: 16 * gi},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deserved := deservedOf(t, tt.node, tt.queues)
			for queue, dims := range tt.want {
				for dim, want := range dims {
					requireDeserved(t, deserved, queue, dim, want)
				}
			}
			// The deserved resources never over-subscribe the cluster.
			totals := api.NewResource(tt.node)
			for _, dim := range []apiv1.ResourceName{apiv1.ResourceCPU, apiv1.ResourceMemory} {
				sum := 0.0
				for _, res := range deserved {
					sum += res.Get(dim)
				}
				if sum > totals.Get(dim)+1 {
					t.Fatalf("deserved %s adds up to %v, more than the cluster capacity %v", dim, sum, totals.Get(dim))
				}
			}
		})
	}
}

func guaranteeFixture(t *testing.T, node apiv1.ResourceList, pods []*apiv1.Pod, podGroups []*schedulingv1beta1.PodGroup, queues []*schedulingv1beta1.Queue) uthelper.TestCommonStruct {
	t.Helper()
	return uthelper.TestCommonStruct{
		Plugins:   map[string]framework.PluginBuilder{PluginName: New},
		Pods:      pods,
		Nodes:     []*apiv1.Node{util.BuildNode("n1", node, nil)},
		PodGroups: podGroups,
		Queues:    queues,
	}
}

func guaranteeTiers() []conf.Tier {
	enabled := true
	return []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledQueueOrder:  &enabled,
		EnabledAllocatable: &enabled,
		EnabledReclaimable: &enabled,
		EnabledOverused:    &enabled,
	}}}}
}

// A queue guaranteed only on one resource must still be able to run pods that
// request other resources too.
func TestGuaranteedQueueCanAllocatePodsWithGPUOnlyGuarantee(t *testing.T) {
	node := mergeLists(api.BuildResourceList("32", "32Gi", api.ScalarResource{Name: "pods", Value: "20"}), gpuList("12"))
	gpus := func(n string) []api.ScalarResource {
		return []api.ScalarResource{{Name: string(gpuResource), Value: n}}
	}
	guaranteed := util.BuildQueue("gq1", 1, nil)
	guaranteed.Spec.Guarantee.Resource = gpuList("8")
	cleanupQueueMetrics(t, "gq1", "gq2", "gq3")

	test := guaranteeFixture(t, node,
		[]*apiv1.Pod{
			util.BuildPod("ns1", "p1", "", apiv1.PodPending, api.BuildResourceList("4", "4Gi", gpus("4")...), "pg1", nil, nil),
			util.BuildPod("ns1", "p2", "", apiv1.PodPending, api.BuildResourceList("4", "4Gi", gpus("2")...), "pg2", nil, nil),
			util.BuildPod("ns1", "p3", "", apiv1.PodPending, api.BuildResourceList("4", "4Gi", gpus("2")...), "pg3", nil, nil),
		},
		[]*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg1", "ns1", "gq1", 1, nil, schedulingv1beta1.PodGroupInqueue),
			util.BuildPodGroup("pg2", "ns1", "gq2", 1, nil, schedulingv1beta1.PodGroupInqueue),
			util.BuildPodGroup("pg3", "ns1", "gq3", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		[]*schedulingv1beta1.Queue{guaranteed, util.BuildQueue("gq2", 1, nil), util.BuildQueue("gq3", 1, nil)})
	test.ExpectBindMap = map[string]string{"ns1/p1": "n1", "ns1/p2": "n1", "ns1/p3": "n1"}
	test.ExpectBindsNum = 3

	test.RegisterSession(guaranteeTiers(), nil)
	defer test.Close()
	test.Run([]framework.Action{allocate.New()})
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}

// Pods of a queue that runs within its guarantee must not be offered as victims
// to other queues.
func TestGuaranteedQueueWithinGuaranteeIsNotReclaimed(t *testing.T) {
	node := mergeLists(api.BuildResourceList("32", "32Gi", api.ScalarResource{Name: "pods", Value: "20"}), gpuList("4"))
	gpus := func(n string) []api.ScalarResource {
		return []api.ScalarResource{{Name: string(gpuResource), Value: n}}
	}
	guaranteed := util.BuildQueue("gq1", 1, nil)
	guaranteed.Spec.Guarantee.Resource = gpuList("3")
	cleanupQueueMetrics(t, "gq1", "gq2")

	test := guaranteeFixture(t, node,
		[]*apiv1.Pod{
			util.BuildPod("ns1", "p1", "n1", apiv1.PodRunning, api.BuildResourceList("2", "2Gi", gpus("3")...), "pg1", nil, nil),
			util.BuildPod("ns1", "p2", "", apiv1.PodPending, api.BuildResourceList("2", "2Gi", gpus("1")...), "pg2", nil, nil),
		},
		[]*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg1", "ns1", "gq1", 1, nil, schedulingv1beta1.PodGroupRunning),
			util.BuildPodGroup("pg2", "ns1", "gq2", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		[]*schedulingv1beta1.Queue{guaranteed, util.BuildQueue("gq2", 1, nil)})

	ssn := test.RegisterSession(guaranteeTiers(), nil)
	defer test.Close()

	var runner, reclaimer *api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			switch task.Name {
			case "p1":
				runner = task
			case "p2":
				reclaimer = task
			}
		}
	}
	if runner == nil || reclaimer == nil {
		t.Fatalf("tasks not found in session: runner=%v reclaimer=%v", runner, reclaimer)
	}
	if victims := ssn.Reclaimable(reclaimer, []*api.TaskInfo{runner}); len(victims) != 0 {
		t.Fatalf("queue running within its guarantee must not be reclaimed, got victims %v", victims)
	}
}
