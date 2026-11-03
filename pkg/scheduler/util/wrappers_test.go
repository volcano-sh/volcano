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
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestPodWrapperMatchesBuildPod checks that the wrapper produces the same pod as
// the BuildPod family it is meant to replace, so a test can switch to the
// wrapper without changing what the scheduler sees.
func TestPodWrapperMatchesBuildPod(t *testing.T) {
	labels := map[string]string{"role": "worker"}
	selector := map[string]string{"zone": "zone1"}
	req := api.BuildResourceList("2", "4Gi")
	priority := int32(10)
	affinity := &v1.Affinity{}
	tolerations := []v1.Toleration{{Key: "dedicated", Operator: v1.TolerationOpExists}}
	constraints := []v1.TopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "zone"}}

	tests := []struct {
		name string
		got  *v1.Pod
		want *v1.Pod
	}{
		{
			name: "plain pod",
			got:  MakePod("ns", "p").Phase(v1.PodPending).Obj(),
			want: BuildPod("ns", "p", "", v1.PodPending, nil, "", nil, nil),
		},
		{
			name: "all common fields",
			got: MakePod("ns", "p").
				Node("node1").
				Phase(v1.PodRunning).
				Resource(req).
				PodGroup("pg").
				Labels(labels).
				NodeSelector(selector).
				Obj(),
			want: BuildPod("ns", "p", "node1", v1.PodRunning, req, "pg", labels, selector),
		},
		{
			name: "priority",
			got:  MakePod("ns", "p").Phase(v1.PodPending).Priority(&priority).Obj(),
			want: BuildPodWithPriority("ns", "p", "", v1.PodPending, nil, "", nil, nil, &priority),
		},
		{
			name: "affinity",
			got:  MakePod("ns", "p").Phase(v1.PodPending).Affinity(affinity).Obj(),
			want: BuildPodWithAffinity("ns", "p", "", v1.PodPending, nil, "", nil, nil, affinity),
		},
		{
			name: "tolerations",
			got:  MakePod("ns", "p").Phase(v1.PodPending).Tolerations(tolerations).Obj(),
			want: BuildPodWithTolerations("ns", "p", "", v1.PodPending, nil, "", nil, nil, tolerations),
		},
		{
			name: "topology spread constraints",
			got:  MakePod("ns", "p").Phase(v1.PodPending).TopologySpreadConstraints(constraints).Obj(),
			want: BuildPodWithTopologySpreadConstraints("ns", "p", "", v1.PodPending, nil, "", nil, nil, constraints),
		},
		{
			name: "preemption policy",
			got:  MakePod("ns", "p").Phase(v1.PodPending).PreemptionPolicy(v1.PreemptNever).Obj(),
			want: BuildPodWithPreemptionPolicy("ns", "p", "", v1.PodPending, nil, "", nil, nil, v1.PreemptNever),
		},
		{
			name: "nominated node",
			got:  MakePod("ns", "p").Phase(v1.PodPending).NominatedNodeName("node1").Obj(),
			want: BuildPodWithNominatedNodeName("ns", "p", "", v1.PodPending, nil, "", "node1", nil, nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.got)
		})
	}
}

func TestPodWrapperAnnotationOnEmptyMap(t *testing.T) {
	pod := MakePod("ns", "p").Annotations(nil).Annotation("key", "value").Obj()
	assert.Equal(t, map[string]string{"key": "value"}, pod.Annotations)
}

func TestPodGroupWrapperMatchesBuildPodGroup(t *testing.T) {
	minTaskMember := map[string]int32{"worker": 2}
	minResources := api.BuildResourceList("4", "8Gi")

	tests := []struct {
		name string
		got  *schedulingv1beta1.PodGroup
		want *schedulingv1beta1.PodGroup
	}{
		{
			name: "plain pod group",
			got: MakePodGroup("ns", "pg").
				Queue("q1").
				MinMember(2).
				MinTaskMember(minTaskMember).
				Phase(schedulingv1beta1.PodGroupInqueue).
				Obj(),
			want: BuildPodGroup("pg", "ns", "q1", 2, minTaskMember, schedulingv1beta1.PodGroupInqueue),
		},
		{
			name: "with priority class",
			got: MakePodGroup("ns", "pg").
				Queue("q1").
				MinMember(1).
				Phase(schedulingv1beta1.PodGroupRunning).
				PriorityClassName("high").
				Obj(),
			want: BuildPodGroupWithPrio("pg", "ns", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "high"),
		},
		{
			name: "with min resources",
			got: MakePodGroup("ns", "pg").
				Queue("q1").
				MinMember(1).
				MinResources(minResources).
				Phase(schedulingv1beta1.PodGroupInqueue).
				Obj(),
			want: BuildPodGroupWithMinResources("pg", "ns", "q1", 1, nil, minResources, schedulingv1beta1.PodGroupInqueue),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.got)
		})
	}
}
