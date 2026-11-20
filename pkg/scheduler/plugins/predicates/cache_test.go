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

package predicates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// buildTemplatePod builds a pending pod of a Volcano job task, annotated the way the
// job controller does: template-uid is "<job name>-<task name>".
func buildTemplatePod(namespace, name, podGroup, templateUID string, selector map[string]string) *apiv1.Pod {
	pod := util.BuildPod(namespace, name, "", apiv1.PodPending, api.BuildResourceList("1", "1Gi"), podGroup, nil, selector)
	if templateUID != "" {
		pod.Annotations[batch.PodTemplateKey] = templateUID
	}
	return pod
}

func TestPredicateWithCache(t *testing.T) {
	tests := []struct {
		name      string
		cachedPod *apiv1.Pod
		pod       *apiv1.Pod
		wantHit   bool
	}{
		{
			name:      "pod of the same task reuses the cached result",
			cachedPod: buildTemplatePod("ns1", "train-worker-0", "train-uid1", "train-worker", nil),
			pod:       buildTemplatePod("ns1", "train-worker-1", "train-uid1", "train-worker", nil),
			wantHit:   true,
		},
		{
			name:      "job with the same name in another namespace does not reuse the result",
			cachedPod: buildTemplatePod("ns1", "train-worker-0", "train-uid1", "train-worker", nil),
			pod:       buildTemplatePod("ns2", "train-worker-0", "train-uid2", "train-worker", nil),
			wantHit:   false,
		},
		{
			name:      "another job with the same template-uid in the same namespace does not reuse the result",
			cachedPod: buildTemplatePod("ns1", "a-b-c-0", "a-b-uid1", "a-b-c", nil),
			pod:       buildTemplatePod("ns1", "a-b-c-0", "a-uid2", "a-b-c", nil),
			wantHit:   false,
		},
		{
			name:      "pod without template-uid is not cached",
			cachedPod: buildTemplatePod("ns1", "pod-0", "pg1", "", nil),
			pod:       buildTemplatePod("ns1", "pod-0", "pg1", "", nil),
			wantHit:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := predicateCacheNew()
			pc.UpdateCache("n1", tt.cachedPod, true)

			fit, err := pc.PredicateWithCache("n1", tt.pod)
			if tt.wantHit {
				require.NoError(t, err)
				assert.True(t, fit)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestPredicateFnWithCache(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}
	hostname := "kubernetes.io/hostname"

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{hostname: "n1"})
	// n2 is dedicated: only pods that tolerate the taint may run there.
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{hostname: "n2"})
	n2.Spec.Taints = []apiv1.Taint{{Key: "dedicated", Value: "gpu", Effect: apiv1.TaintEffectNoSchedule}}
	toleration := apiv1.Toleration{Key: "dedicated", Operator: apiv1.TolerationOpEqual, Value: "gpu", Effect: apiv1.TaintEffectNoSchedule}

	// buildPods returns a pod pinned to n2 that tolerates its taint, and a pod with
	// neither, which can only run on n1. Both have the same template-uid.
	buildPods := func(dedicatedNS, dedicatedPG, otherNS, otherPG, templateUID string) []*apiv1.Pod {
		dedicated := buildTemplatePod(dedicatedNS, templateUID+"-0", dedicatedPG, templateUID, map[string]string{hostname: "n2"})
		dedicated.Spec.Tolerations = []apiv1.Toleration{toleration}
		other := buildTemplatePod(otherNS, templateUID+"-0", otherPG, templateUID, nil)
		return []*apiv1.Pod{dedicated, other}
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "jobs with the same name in different namespaces",
			Pods: buildPods("ns-a", "train-uid1", "ns-b", "train-uid2", "train-worker"),
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("train-uid1", "ns-a", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("train-uid2", "ns-b", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue),
			},
			ExpectBindMap: map[string]string{
				"ns-a/train-worker-0": "n2",
				"ns-b/train-worker-0": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "jobs with the same template-uid in the same namespace",
			Pods: []*apiv1.Pod{
				func() *apiv1.Pod {
					p := buildTemplatePod("ns1", "a-b-c-0", "a-b-uid1", "a-b-c", map[string]string{hostname: "n2"})
					p.Spec.Tolerations = []apiv1.Toleration{toleration}
					return p
				}(),
				buildTemplatePod("ns1", "a-b-c-1", "a-uid2", "a-b-c", nil),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("a-b-uid1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("a-uid2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue),
			},
			ExpectBindMap: map[string]string{
				"ns1/a-b-c-0": "n2",
				"ns1/a-b-c-1": "n1",
			},
			ExpectBindsNum: 2,
		},
	}

	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:             PluginName,
		EnabledPredicate: &trueValue,
		Arguments:        map[string]interface{}{CachePredicate: true},
	}}}}

	for i, test := range tests {
		test.Plugins = plugins
		test.Nodes = []*apiv1.Node{n1.DeepCopy(), n2.DeepCopy()}
		test.Queues = []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, nil)}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{allocate.New()})
			require.NoError(t, test.CheckAll(i))
		})
	}
}
