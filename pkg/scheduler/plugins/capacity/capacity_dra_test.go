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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubefeatures "k8s.io/kubernetes/pkg/features"

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
			q.Spec.DRA = &schedulingv1beta1.DRAQuota{
				Capability: make(map[string]schedulingv1beta1.DRAResourceQuota),
			}
			for class, count := range capability {
				q.Spec.DRA.Capability[class] = schedulingv1beta1.DRAResourceQuota{Count: count}
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

		pg := util.BuildPodGroup(name+"-pg", ns, queue, 1, nil, schedulingv1beta1.PodGroupInqueue)

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
	pg5_1.Spec.MinMember = 1                                                    // Set MinMember so GetMinDRAResources() can calculate it requires 2

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
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				// pg5_1 exceeds queue gpu limits so it should remain in pending/failed to enqueue
				"ns1/p5-1-pg": scheduling.PodGroupPending,
			},
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
