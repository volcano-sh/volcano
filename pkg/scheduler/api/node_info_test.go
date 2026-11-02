/*
 Copyright 2021 The Volcano Authors.

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
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/cpuset"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/mindcluster/ascend310p/vnpu"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
)

func nodeInfoEqual(l, r *NodeInfo) bool {
	return reflect.DeepEqual(l, r)
}

func makeNodeOthers(nodeName string, node *v1.Node) map[string]interface{} {
	others := make(map[string]interface{})

	if d := gpushare.NewGPUDevices(nodeName, node); d != nil {
		others[gpushare.DeviceName] = d
	}
	if d := vgpu.NewGPUDevices(nodeName, node); d != nil {
		others[vgpu.DeviceName] = d
	}
	if d := vnpu.NewNPUDevices(nodeName, node); d != nil {
		others[vnpu.DeviceName] = d
	}
	return others
}

func TestNodeInfo_AddPod(t *testing.T) {
	// case1
	case01Node := buildNode("n1", nil, BuildResourceList("8000m", "10G", []ScalarResource{{Name: "pods", Value: "20"}}...))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, BuildResourceList("2000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	// case2
	case02Node := buildNode("n2", nil, BuildResourceList("2000m", "1G", []ScalarResource{{Name: "pods", Value: "20"}}...))
	case02Pod1 := buildPod("c2", "p1", "n2", v1.PodUnknown, BuildResourceList("1000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name            string
		node            *v1.Node
		pods            []*v1.Pod
		expected        *NodeInfo
		expectedFailure bool
	}{
		{
			name: "add 2 running non-owner pod",
			node: case01Node,
			pods: []*v1.Pod{case01Pod1, case01Pod2},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node,
				Idle:                     buildResource("5000m", "7G", map[string]string{"pods": "18"}, 20),
				Used:                     buildResource("3000m", "3G", map[string]string{"pods": "2"}, 0),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				OversubscriptionResource: EmptyResource(),
				Allocatable:              buildResource("8000m", "10G", map[string]string{"pods": "20"}, 20),
				Capacity:                 buildResource("8000m", "10G", map[string]string{"pods": "20"}, 20),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
				},
				Others:      makeNodeOthers("n1", case01Node),
				ImageStates: make(map[string]*fwk.ImageStateSummary),
			},
		},
		{
			name: "add 1 unknown pod and pod memory req > idle",
			node: case02Node,
			pods: []*v1.Pod{case02Pod1},
			expected: &NodeInfo{
				Name:                     "n2",
				Node:                     case02Node,
				Idle:                     buildResource("1000m", "-1G", map[string]string{"pods": "19"}, 20),
				Used:                     buildResource("1000m", "2G", map[string]string{"pods": "1"}, 0),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				OversubscriptionResource: EmptyResource(),
				Allocatable:              buildResource("2000m", "1G", map[string]string{"pods": "20"}, 20),
				Capacity:                 buildResource("2000m", "1G", map[string]string{"pods": "20"}, 20),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c2/p1": NewTaskInfo(case02Pod1),
				},
				Others:      makeNodeOthers("n2", case02Node),
				ImageStates: make(map[string]*fwk.ImageStateSummary),
			},
			expectedFailure: false,
		},
	}

	for i, test := range tests {
		gpushare.GpuSharingEnable = true
		vgpu.VGPUEnable = true
		vnpu.AscendMindClusterVNPUEnable = true
		ni := NewNodeInfo(test.node)
		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			err := ni.AddTask(pi)
			if err != nil && !test.expectedFailure {
				t.Errorf("node info %d: \n expected success, \n but got err %v \n", i, err)
			}
			if err == nil && test.expectedFailure {
				t.Errorf("node info %d: \n expected failure, \n but got success \n", i)
			}
		}

		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected %v, \n got %v \n",
				i, test.expected, ni)
		}
	}
}

func TestNodeInfo_RemovePod(t *testing.T) {
	// case1
	case01Node := buildNode("n1", nil, BuildResourceList("8000m", "10G", []ScalarResource{{Name: "pods", Value: "10"}}...))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, BuildResourceList("2000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, BuildResourceList("3000m", "3G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name     string
		node     *v1.Node
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *NodeInfo
	}{
		{
			name:   "add 3 running non-owner pod, remove 1 running non-owner pod",
			node:   case01Node,
			pods:   []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			rmPods: []*v1.Pod{case01Pod2},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node,
				Idle:                     buildResource("4000m", "6G", map[string]string{"pods": "8"}, 10),
				Used:                     buildResource("4000m", "4G", map[string]string{"pods": "2"}, 0),
				OversubscriptionResource: EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				Allocatable:              buildResource("8000m", "10G", map[string]string{"pods": "10"}, 10),
				Capacity:                 buildResource("8000m", "10G", map[string]string{"pods": "10"}, 10),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				Others:      makeNodeOthers("n1", case01Node),
				ImageStates: make(map[string]*fwk.ImageStateSummary),
			},
		},
	}

	for i, test := range tests {
		ni := NewNodeInfo(test.node)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ni.AddTask(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewTaskInfo(pod)
			ni.RemoveTask(pi)
		}

		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected %v, \n got %v \n",
				i, test.expected, ni)
		}
	}
}

func TestNodeInfo_SetNode(t *testing.T) {
	// case1
	case01Node1 := buildNode("n1", nil, BuildResourceList("10", "10G", []ScalarResource{{Name: "pods", Value: "15"}}...))
	case01Node2 := buildNode("n1", nil, BuildResourceList("8", "8G", []ScalarResource{{Name: "pods", Value: "10"}}...))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, BuildResourceList("1", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, BuildResourceList("2", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, BuildResourceList("6", "6G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name      string
		node      *v1.Node
		updated   *v1.Node
		pods      []*v1.Pod
		expected  *NodeInfo
		expected2 *NodeInfo
	}{
		{
			name:    "add 3 running non-owner pod",
			node:    case01Node1,
			updated: case01Node2,
			pods:    []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node2,
				Idle:                     buildResource("-1", "-1G", map[string]string{"pods": "7"}, 10),
				Used:                     buildResource("9", "9G", map[string]string{"pods": "3"}, 0),
				OversubscriptionResource: EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				Allocatable:              buildResource("8", "8G", map[string]string{"pods": "10"}, 10),
				Capacity:                 buildResource("8", "8G", map[string]string{"pods": "10"}, 10),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready, Reason: ""},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				Others:      makeNodeOthers("n1", case01Node1),
				ImageStates: make(map[string]*fwk.ImageStateSummary),
			},
			expected2: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node1,
				Idle:                     buildResource("1", "1G", map[string]string{"pods": "12"}, 15),
				Used:                     buildResource("9", "9G", map[string]string{"pods": "3"}, 0),
				OversubscriptionResource: EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				Allocatable:              buildResource("10", "10G", map[string]string{"pods": "15"}, 15),
				Capacity:                 buildResource("10", "10G", map[string]string{"pods": "15"}, 15),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready, Reason: ""},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				Others:      makeNodeOthers("n1", case01Node1),
				ImageStates: make(map[string]*fwk.ImageStateSummary),
			},
		},
	}

	for i, test := range tests {
		ni := NewNodeInfo(test.node)
		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ni.AddTask(pi)
			ni.Name = pod.Spec.NodeName
		}

		// OutOfSync. e.g.: nvidia-device-plugin is down causes gpus turn from 8 to 0 (node.status.allocatable."nvidia.com/gpu": 0)
		ni.SetNode(test.updated)
		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected\t%v, \n got\t\t%v \n",
				i, test.expected, ni)
		}

		// Recover. e.g.: nvidia-device-plugin is restarted successfully
		ni.SetNode(test.node)
		if !nodeInfoEqual(ni, test.expected2) {
			t.Errorf("recovered %d: \n expected\t%v, \n got\t\t%v \n",
				i, test.expected2, ni)
		}
	}
}

func BenchmarkSetNode(b *testing.B) {
	node1 := buildNode("n1", nil, BuildResourceList("16", "32G", []ScalarResource{{Name: "pods", Value: "110"}}...))
	node2 := buildNode("n1", nil, BuildResourceList("16", "32G", []ScalarResource{{Name: "pods", Value: "110"}}...))

	pods := make([]*v1.Pod, 20)
	for i := range pods {
		pods[i] = buildPod("default", fmt.Sprintf("p%d", i), "n1", v1.PodRunning,
			BuildResourceList("100m", "128Mi"), []metav1.OwnerReference{}, make(map[string]string))
	}

	ni := NewNodeInfo(node1)
	for _, pod := range pods {
		_ = ni.AddTask(NewTaskInfo(pod))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			ni.SetNode(node2)
		} else {
			ni.SetNode(node1)
		}
	}
}

// BenchmarkNodeInfoClone measures the per-call cost of Clone() to show what
// SetNode was paying on every invocation before this optimization.
func BenchmarkNodeInfoClone(b *testing.B) {
	node := buildNode("n1", nil, BuildResourceList("16", "32G", []ScalarResource{{Name: "pods", Value: "110"}}...))

	pods := make([]*v1.Pod, 20)
	for i := range pods {
		pods[i] = buildPod("default", fmt.Sprintf("p%d", i), "n1", v1.PodRunning,
			BuildResourceList("100m", "128Mi"), []metav1.OwnerReference{}, make(map[string]string))
	}

	ni := NewNodeInfo(node)
	for _, pod := range pods {
		_ = ni.AddTask(NewTaskInfo(pod))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ni.Clone()
	}
}

func TestCloneOthersPreservesTypedNilDevices(t *testing.T) {
	ni := &NodeInfo{
		Name: "test-node",
		Others: map[string]interface{}{
			vgpu.DeviceName:     (*vgpu.GPUDevices)(nil),
			gpushare.DeviceName: (*gpushare.GPUDevices)(nil),
			vnpu.DeviceName:     (*vnpu.NPUDevices)(nil),
		},
	}

	cloned := ni.CloneOthers()

	for k := range ni.Others {
		cv, present := cloned[k]
		if !present {
			t.Errorf("cloned map missing key %q", k)
			continue
		}
		d, ok := cv.(Devices)
		if !ok {
			t.Errorf("cloned[%q] does not satisfy Devices interface (typed-nil was lost)", k)
			continue
		}
		if !IsNilDevice(d) {
			t.Errorf("cloned[%q] should be a typed-nil pointer", k)
		}
	}
}

// buildNodeInfoWithNuma constructs a NodeInfo whose NumaInfo/UnassignedNumaPods are
// set up for RefreshNumaSchedulerInfoByCrd testing.
func buildNodeInfoWithNuma(numaInfo *NumatopoInfo, unassigned map[PodMeta]ResNumaSets) *NodeInfo {
	ni := NewNodeInfo(buildNode("n1", nil, BuildResourceList("4000m", "4G")))
	ni.NumaInfo = numaInfo
	ni.UnassignedNumaPods = unassigned
	return ni
}

func TestRefreshNumaSchedulerInfoByCrd_NilNumaInfo(t *testing.T) {
	ni := NewNodeInfo(buildNode("n1", nil, BuildResourceList("4000m", "4G")))
	ni.NumaInfo = nil
	ni.NumaSchedulerInfo = newNumatopoInfoForTest(nil)

	ni.RefreshNumaSchedulerInfoByCrd()

	if ni.NumaSchedulerInfo != nil {
		t.Fatalf("expected NumaSchedulerInfo to be nil, got %+v", ni.NumaSchedulerInfo)
	}
}

func TestRefreshNumaSchedulerInfoByCrd_AllAssignedClearsUnassigned(t *testing.T) {
	numaInfo := newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{
		{UID: "uid-1", Name: "p1", Namespace: "ns1"},
	})
	assigned := PodMeta{UID: "uid-1", Name: "p1", Namespace: "ns1"}
	// Even though the pod is in UnassignedNumaPods, it has already been assigned by kubelet,
	// so RefreshNumaSchedulerInfoByCrd must clear the pre-occupy map and NOT subtract the resource.
	unassigned := map[PodMeta]ResNumaSets{assigned: {"cpu": cpuset.New(0, 1)}}

	ni := buildNodeInfoWithNuma(numaInfo, unassigned)
	ni.RefreshNumaSchedulerInfoByCrd()

	if ni.UnassignedNumaPods != nil {
		t.Errorf("expected UnassignedNumaPods to be cleared, got %+v", ni.UnassignedNumaPods)
	}
	got := ni.NumaSchedulerInfo.NumaResMap["cpu"].Allocatable
	want := cpuset.New(0, 1, 2, 3)
	if !got.Equals(want) {
		t.Errorf("Allocatable should remain unchanged when all pods are assigned: got %v, want %v", got, want)
	}
}

func TestRefreshNumaSchedulerInfoByCrd_UnassignedPodPreOccupiesResources(t *testing.T) {
	numaInfo := newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{
		{UID: "uid-1", Name: "p1", Namespace: "ns1"},
	})
	unassignedPod := PodMeta{UID: "uid-2", Name: "p2", Namespace: "ns1"}
	// p2 is scheduled by volcano but not yet assigned NUMA resources by kubelet.
	// Its allocation decision {cpu:[0,1]} must be subtracted to keep the scheduler cache accurate.
	unassigned := map[PodMeta]ResNumaSets{unassignedPod: {"cpu": cpuset.New(0, 1)}}

	ni := buildNodeInfoWithNuma(numaInfo, unassigned)
	ni.RefreshNumaSchedulerInfoByCrd()

	if ni.UnassignedNumaPods == nil {
		t.Fatalf("expected UnassignedNumaPods to be preserved when pod is still unassigned")
	}
	if _, ok := ni.UnassignedNumaPods[unassignedPod]; !ok {
		t.Errorf("expected entry for %v to remain in UnassignedNumaPods", unassignedPod)
	}
	got := ni.NumaSchedulerInfo.NumaResMap["cpu"].Allocatable
	want := cpuset.New(2, 3)
	if !got.Equals(want) {
		t.Errorf("Allocatable should reflect pre-occupied subtract: got %v, want %v", got, want)
	}
}

func TestRefreshNumaSchedulerInfoByCrd_MixedAssignedAndUnassigned(t *testing.T) {
	numaInfo := newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{
		{UID: "uid-1", Name: "p1", Namespace: "ns1"},
	})
	// p1 already assigned by kubelet. p2 not yet.
	// Per commit message: as long as ANY entry is still unassigned, the entire
	// UnassignedNumaPods map is preserved (including already-assigned pods) and
	// ALL its entries pre-occupy resources. This conservative policy prevents
	// volcano from losing allocation info and dispatching a pod that kubelet
	// rejects with TopologyAffinityError.
	assignedPod := PodMeta{UID: "uid-1", Name: "p1", Namespace: "ns1"}
	unassignedPod := PodMeta{UID: "uid-2", Name: "p2", Namespace: "ns1"}
	unassigned := map[PodMeta]ResNumaSets{
		assignedPod:   {"cpu": cpuset.New(3)},
		unassignedPod: {"cpu": cpuset.New(0, 1)},
	}

	ni := buildNodeInfoWithNuma(numaInfo, unassigned)
	ni.RefreshNumaSchedulerInfoByCrd()

	if ni.UnassignedNumaPods == nil {
		t.Fatalf("expected UnassignedNumaPods to be preserved when any pod is still unassigned")
	}
	if len(ni.UnassignedNumaPods) != 2 {
		t.Errorf("expected both entries to remain, got %v", ni.UnassignedNumaPods)
	}
	if _, ok := ni.UnassignedNumaPods[unassignedPod]; !ok {
		t.Errorf("expected %v to remain in UnassignedNumaPods", unassignedPod)
	}
	if _, ok := ni.UnassignedNumaPods[assignedPod]; !ok {
		t.Errorf("expected %v to also remain in UnassignedNumaPods (conservative policy)", assignedPod)
	}
	got := ni.NumaSchedulerInfo.NumaResMap["cpu"].Allocatable
	// Original {0,1,2,3} - p1's decision {3} - p2's decision {0,1} = {2}.
	// Both entries' decisions are subtracted because the conservative policy
	// keeps them all pre-occupied until every pod has been assigned.
	want := cpuset.New(2)
	if !got.Equals(want) {
		t.Errorf("Allocatable with mixed assignment: got %v, want %v", got, want)
	}
}

func TestNodeInfoClonePreservesUnassignedNumaPods(t *testing.T) {
	numaInfo := newNumatopoInfoForTest([]nodeinfov1alpha1.PodAllocation{})
	unassigned := map[PodMeta]ResNumaSets{
		{UID: "uid-2", Name: "p2", Namespace: "ns1"}: {"cpu": cpuset.New(0, 1)},
	}
	ni := buildNodeInfoWithNuma(numaInfo, unassigned)
	ni.NumaSchedulerInfo = numaInfo.DeepCopy()

	clone := ni.Clone()

	if clone.UnassignedNumaPods == nil {
		t.Fatalf("Clone did not preserve UnassignedNumaPods")
	}
	got, ok := clone.UnassignedNumaPods[PodMeta{UID: "uid-2", Name: "p2", Namespace: "ns1"}]
	if !ok {
		t.Fatalf("Clone lost the unassigned pod entry")
	}
	if !got["cpu"].Equals(cpuset.New(0, 1)) {
		t.Errorf("Cloned CPUSet mismatch: got %v", got["cpu"])
	}

	// Mutating the clone must not affect the original.
	clone.UnassignedNumaPods[PodMeta{UID: "uid-2", Name: "p2", Namespace: "ns1"}] = ResNumaSets{"cpu": cpuset.New(9)}
	if !ni.UnassignedNumaPods[PodMeta{UID: "uid-2", Name: "p2", Namespace: "ns1"}]["cpu"].Equals(cpuset.New(0, 1)) {
		t.Errorf("Clone mutation leaked back into original: %v", ni.UnassignedNumaPods)
	}
}

// TestNodeInfo_AddTask_UntrackedResourceDefersToPredicate covers the final, most fundamental
// occurrence of the #4863-class bug: AddTask's Binding-status check is the last gate every task
// passes through before actually binding to the API server, across every scheduling action. A
// task requesting a resource dimension the node never advertises in Allocatable at all (like
// volcano.sh/vgpu-memory-percentage, tracked entirely by a device plugin's own ledger) must not
// be rejected here, since nothing downstream double-checks it again.
func TestNodeInfo_AddTask_UntrackedResourceDefersToPredicate(t *testing.T) {
	node := buildNode("n1", nil, BuildResourceList("2000m", "2G", []ScalarResource{{Name: "pods", Value: "10"}}...))
	pod := buildPod("c1", "p1", "", v1.PodPending,
		BuildResourceList("1000m", "1G", []ScalarResource{{Name: "volcano.sh/vgpu-memory-percentage", Value: "50"}}...),
		[]metav1.OwnerReference{}, make(map[string]string))

	ni := NewNodeInfo(node)
	task := NewTaskInfo(pod)
	task.Status = Binding

	if err := ni.AddTask(task); err != nil {
		t.Fatalf("expected AddTask to succeed for a resource untracked in node.Allocatable, got error: %v", err)
	}
}

// TestNodeInfo_AddTask_GenuineShortfallStillRejected is the companion regression: a real,
// tracked-resource shortfall (cpu here) must still be rejected at bind time. The fallback above
// is only for dimensions Allocatable has no entry for at all, not a blanket bypass.
func TestNodeInfo_AddTask_GenuineShortfallStillRejected(t *testing.T) {
	node := buildNode("n1", nil, BuildResourceList("1000m", "2G", []ScalarResource{{Name: "pods", Value: "10"}}...))
	pod := buildPod("c1", "p1", "", v1.PodPending, BuildResourceList("2000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))

	ni := NewNodeInfo(node)
	task := NewTaskInfo(pod)
	task.Status = Binding

	if err := ni.AddTask(task); err == nil {
		t.Fatal("expected AddTask to reject a genuine cpu shortfall, got success")
	}
}
