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

package capacity

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func Test_capacityPlugin_DRA(t *testing.T) {
	// Enable DRA feature gate
	utilfeature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=true", kubefeatures.DynamicResourceAllocation))

	plugins := map[string]framework.PluginBuilder{PluginName: New, predicates.PluginName: predicates.New, gang.PluginName: gang.New}
	trueValue := true
	actions := []framework.Action{enqueue.New(), allocate.New()}

	// Helper to build Queue with DRA
	buildDRAQueue := func(name string, capability map[string]int64) *schedulingv1beta1.Queue {
		q := util.BuildQueueWithResourcesQuantity(name, nil, nil)
		if capability != nil {
			q.Spec.Capability = make(v1.ResourceList, len(capability))
			for class, count := range capability {
				q.Spec.Capability[v1.ResourceName(DeviceClassCountPrefix+class)] = resource.MustParse(fmt.Sprintf("%d", count))
			}
		}
		return q
	}

	// Helper to build Pod and Claim for DRA
	buildDRAPodAndClaim := func(ns, name, queue, deviceClass string, count int64) (*v1.Pod, *resourcev1.ResourceClaim, *schedulingv1beta1.PodGroup) {
		claimName := name + "-claim"
		claim := &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: ns,
			},
			Spec: resourcev1.ResourceClaimSpec{
				Devices: resourcev1.DeviceClaim{
					Requests: []resourcev1.DeviceRequest{
						{
							Name: "req-1",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: deviceClass,
								Count:           count,
							},
						},
					},
				},
			},
		}

		pod := util.BuildPod(ns, name, "", v1.PodPending, api.BuildResourceList("1", "1Gi"), name+"-pg", nil, nil)
		pod.Spec.ResourceClaims = []v1.PodResourceClaim{
			{
				Name:              "claim-1",
				ResourceClaimName: &claimName,
			},
		}
		pod.Annotations[batchv1alpha1.TaskSpecKey] = "worker"

		pg := util.BuildPodGroup(name+"-pg", ns, queue, 1, nil, schedulingv1beta1.PodGroupInqueue)
		pg.Spec.MinTaskMember = map[string]int32{"worker": 1}

		return pod, claim, pg
	}

	// Node (needs to be capable, but capacity plugin doesn't check node capacity for DRA in queue check,
	// only queue quota. Predicates might check node capacity, but we use fake binder/evictor anyway.)
	// However, we need a node to bind to.
	n1 := util.BuildNode("n1", api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

	// Case 1: Simple Success
	q1 := buildDRAQueue("q1", map[string]int64{"gpu.com": 2})
	p1, c1, pg1 := buildDRAPodAndClaim("ns1", "p1", "q1", "gpu.com", 1)

	// Case 2: Quota Exceeded
	q2 := buildDRAQueue("q2", map[string]int64{"gpu.com": 1})
	p2, c2, pg2 := buildDRAPodAndClaim("ns1", "p2", "q2", "gpu.com", 2)

	// Case 3: Multiple Device Classes
	q3 := buildDRAQueue("q3", map[string]int64{"gpu.com": 1, "nic.com": 1})
	p3_1, c3_1, pg3_1 := buildDRAPodAndClaim("ns1", "p3-1", "q3", "gpu.com", 1)
	p3_2, c3_2, pg3_2 := buildDRAPodAndClaim("ns1", "p3-2", "q3", "nic.com", 1)
	p3_3, c3_3, pg3_3 := buildDRAPodAndClaim("ns1", "p3-3", "q3", "gpu.com", 1) // Should fail (total gpu=2 > 1)

	// Case 4: Pass-through for unconfigured DeviceClass
	q4 := buildDRAQueue("q4", map[string]int64{"gpu.com": 1}) // nic.com is not configured
	p4_1, c4_1, pg4_1 := buildDRAPodAndClaim("ns1", "p4-1", "q4", "gpu.com", 1)
	p4_2, c4_2, pg4_2 := buildDRAPodAndClaim("ns1", "p4-2", "q4", "nic.com", 100) // Should pass-through because nic.com is not in q4 capability

	// Case 5: Job Enqueue DRA Check
	q5 := buildDRAQueue("q5", map[string]int64{"gpu.com": 1})
	p5_1, c5_1, pg5_1 := buildDRAPodAndClaim("ns1", "p5-1", "q5", "gpu.com", 2) // Should be rejected at enqueue stage because job requires 2 which is > q5 capability(1)

	tests := []uthelper.TestCommonStruct{
		{
			Name:           "Case 1: DRA Request within quota",
			Plugins:        plugins,
			Pods:           []*v1.Pod{p1},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg1},
			Queues:         []*schedulingv1beta1.Queue{q1},
			Nodes:          []*v1.Node{n1},
			ResourceClaims: []*resourcev1.ResourceClaim{c1},
			ExpectBindsNum: 1,
			ExpectBindMap:  map[string]string{"ns1/p1": "n1"},
		},
		{
			Name:           "Case 2: DRA Request exceeds quota",
			Plugins:        plugins,
			Pods:           []*v1.Pod{p2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg2},
			Queues:         []*schedulingv1beta1.Queue{q2},
			Nodes:          []*v1.Node{n1},
			ResourceClaims: []*resourcev1.ResourceClaim{c2},
			ExpectBindsNum: 0,
		},
		{
			Name:           "Case 3: Multiple Device Classes and Mixed Success/Failure",
			Plugins:        plugins,
			Pods:           []*v1.Pod{p3_1, p3_2, p3_3},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg3_1, pg3_2, pg3_3},
			Queues:         []*schedulingv1beta1.Queue{q3},
			Nodes:          []*v1.Node{n1},
			ResourceClaims: []*resourcev1.ResourceClaim{c3_1, c3_2, c3_3},
			ExpectBindsNum: 2, // p3_1 and p3_2 should bind, p3_3 fail
			ExpectBindMap: map[string]string{
				"ns1/p3-1": "n1",
				"ns1/p3-2": "n1",
			},
		},
		{
			Name:           "Case 4: Pass-through for unconfigured DeviceClass",
			Plugins:        plugins,
			Pods:           []*v1.Pod{p4_1, p4_2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg4_1, pg4_2},
			Queues:         []*schedulingv1beta1.Queue{q4},
			Nodes:          []*v1.Node{n1},
			ResourceClaims: []*resourcev1.ResourceClaim{c4_1, c4_2},
			ExpectBindsNum: 2, // p4_1 uses gpu.com (1 <= 1 limit), p4_2 uses nic.com (unconfigured, so no limit)
			ExpectBindMap: map[string]string{
				"ns1/p4-1": "n1",
				"ns1/p4-2": "n1",
			},
		},
		{
			Name:           "Case 5: Job Enqueue DRA Check rejects oversize job",
			Plugins:        plugins,
			Pods:           []*v1.Pod{p5_1},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg5_1},
			Queues:         []*schedulingv1beta1.Queue{q5},
			Nodes:          []*v1.Node{n1},
			ResourceClaims: []*resourcev1.ResourceClaim{c5_1},
			ExpectBindsNum: 0,
		},
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnabledJobEnqueued: &trueValue,
					Arguments: framework.Arguments{
						DynamicResourceAllocationEnable: true,
					},
				},
				{
					Name:               gang.PluginName,
					EnabledJobStarving: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSharedResourceClaimIsChargedOnce(t *testing.T) {
	attr := &queueAttr{
		allocated:      api.EmptyResource(),
		realCapability: api.InfiniteResource(),
		dra: &draQuotaAttr{
			allocated: make(map[string]*api.DRAResource),
			capability: map[string]*api.DRAResource{
				"gpu.com": {Count: 1},
			},
		},
		resourceClaimRefs: make(map[string]int),
	}
	taskA := &api.TaskInfo{
		Resreq:            api.EmptyResource(),
		ResourceClaimKeys: []string{"ns1/shared"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"ns1/shared": {
				"gpu.com": {Count: 1},
			},
		},
		DRAResreq: map[string]*api.DRAResource{
			"gpu.com": {Count: 1},
		},
	}
	taskB := &api.TaskInfo{
		Resreq:            api.EmptyResource(),
		ResourceClaimKeys: []string{"ns1/shared"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"ns1/shared": {
				"gpu.com": {Count: 1},
			},
		},
		DRAResreq: map[string]*api.DRAResource{
			"gpu.com": {Count: 1},
		},
	}

	addTaskDRAAllocated(attr, taskA)
	addTaskDRAAllocated(attr, taskB)

	if got := attr.dra.allocated["gpu.com"].Count; got != 1 {
		t.Fatalf("shared claim should be counted once after two additions, got %d", got)
	}

	queue := &api.QueueInfo{Name: "q1"}
	if !queueAllocatable(attr, taskA, queue, true, false) {
		t.Fatalf("shared claim should not consume additional DRA quota when the claim is already referenced by the queue")
	}

	distinctTask := &api.TaskInfo{
		Name:              "distinct",
		Resreq:            api.EmptyResource(),
		ResourceClaimKeys: []string{"ns1/distinct"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"ns1/distinct": {
				"gpu.com": {Count: 1},
			},
		},
		DRAResreq: map[string]*api.DRAResource{
			"gpu.com": {Count: 1},
		},
	}
	if queueAllocatable(attr, distinctTask, queue, true, false) {
		t.Fatalf("distinct claim should still be rejected when it exceeds remaining DRA quota")
	}

	removeTaskDRAAllocated(attr, taskA)
	if got := attr.dra.allocated["gpu.com"].Count; got != 1 {
		t.Fatalf("shared claim should remain allocated until last reference is removed, got %d", got)
	}

	removeTaskDRAAllocated(attr, taskB)
	if got := attr.dra.allocated["gpu.com"].Count; got != 0 {
		t.Fatalf("shared claim should be released after last reference is removed, got %d", got)
	}
}

func TestJobEnqueueableChecksDRAWithoutMinResources(t *testing.T) {
	queueID := api.QueueID("q1")
	cp := &capacityPlugin{
		queueOpts: map[api.QueueID]*queueAttr{
			queueID: {
				allocated:         api.EmptyResource(),
				inqueue:           api.EmptyResource(),
				elastic:           api.EmptyResource(),
				realCapability:    api.InfiniteResource(),
				resourceClaimRefs: make(map[string]int),
				dra: newDRAQuotaAttr(v1.ResourceList{
					v1.ResourceName(DeviceClassCountPrefix + "gpu.com"): resource.MustParse("1"),
				}, nil, nil),
			},
		},
		dynamicResourceAllocationEnable: true,
	}
	queue := &api.QueueInfo{
		UID:  queueID,
		Name: "q1",
		Queue: &scheduling.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "q1"},
			Spec:       scheduling.QueueSpec{},
			Status:     scheduling.QueueStatus{State: scheduling.QueueStateOpen},
		},
	}
	job := api.NewJobInfo(api.JobID("ns1/job1"))
	job.Name = "job1"
	job.Namespace = "ns1"
	job.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "job1", Namespace: "ns1"},
			Spec:       scheduling.PodGroupSpec{},
		},
	}
	job.TaskMinAvailable["worker"] = 1
	job.Tasks[api.TaskID("task1")] = &api.TaskInfo{
		TaskRole: "worker",
		DRAResreq: map[string]*api.DRAResource{
			"gpu.com": {Count: 2},
		},
	}

	enqueueable, reasons := cp.jobEnqueueable(queue, job)
	if enqueueable {
		t.Fatalf("expected DRA-only job to be rejected without MinResources when queue capability is insufficient")
	}
	if len(reasons) == 0 || reasons[len(reasons)-1] != "dra-resource-exceeded" {
		t.Fatalf("expected dra-resource-exceeded reason, got %v", reasons)
	}
}
