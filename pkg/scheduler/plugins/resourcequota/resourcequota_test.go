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

package resourcequota

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestResourceQuotaPlugin(t *testing.T) {

	hugeResource := api.BuildResourceList("20000m", "20G")
	normalResource := api.BuildResourceList("2000m", "2G")

	// pg that requires normal resources
	pg1 := util.BuildPodGroup("pg1", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg1.Spec.MinResources = &normalResource
	// pg that requires small resources
	pg2 := util.BuildPodGroup("pg2", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg2.Spec.MinResources = &hugeResource
	// pg that no set requires
	pg3 := util.BuildPodGroup("pg3", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))

	queue1 := util.BuildQueue("c1", 1, nil)
	rq1 := util.BuildResourceQuota("test", "default", normalResource)

	tests := []struct {
		uthelper.TestCommonStruct
		expectedEnqueueAble bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "ResourceQuota capacity can match the needs of this pg",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg1},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "ResourceQuota capacity can't match the needs of this pg",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg2},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
			},
			expectedEnqueueAble: false,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "No ResourceQuota resource object",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg2},
				Queues:    []*schedulingv1.Queue{queue1},
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "MinResources field of pg is not set",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg3},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
			},
			expectedEnqueueAble: true,
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               PluginName,
							EnabledJobEnqueued: &trueValue,
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			for _, job := range ssn.Jobs {
				isEnqueue := ssn.JobEnqueueable(job)
				if !equality.Semantic.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
					t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectedEnqueueAble, isEnqueue)
				}
			}
		})
	}
}
