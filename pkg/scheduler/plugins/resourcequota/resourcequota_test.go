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

package resourcequota

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"testing"
	scheduling "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestResourceQuotaPlugin(t *testing.T) {
	tests := []struct {
		desc            string
		expectedOutcome bool
		job             *api.JobInfo
	}{

		{
			desc:            "requested_resource_within_limits",
			expectedOutcome: true,
			job: &api.JobInfo{
				UID:       "1",
				Namespace: "namespace1",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("3"),
								v1.ResourceMemory: resource.MustParse("100Gi"),
							},
						},
					},
				},
			},
		},
		{
			desc:            "requested_resource_over_limits",
			expectedOutcome: false,
			job: &api.JobInfo{
				UID:       "1",
				Namespace: "namespace1",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("5"),
								v1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
		{
			desc:            "requesting_resource_of_non_existing_namespace",
			expectedOutcome: true,
			job: &api.JobInfo{
				UID:       "1",
				Namespace: "some_random_namespace",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("3"),
								v1.ResourceMemory: resource.MustParse("100Gi"),
							},
						},
					},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			trueValue := true
			schedulerCache := &cache.SchedulerCache{
				Jobs: make(map[api.JobID]*api.JobInfo),

				Recorder: record.NewFakeRecorder(100),
			}
			framework.RegisterPluginBuilder(PluginName, New)
			defer framework.CleanupPluginBuilders()
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name: PluginName,
							//Arguments: test.arguments,
						},
						{
							Name:               "resourcequota",
							EnabledJobEnqueued: &trueValue,
						},
					},
				},
			}, nil)
			ssn.NamespaceInfo = make(map[api.NamespaceName]*api.NamespaceInfo)
			quota1 := make(map[string]v1.ResourceQuotaStatus)
			quota1["quota1"] = v1.ResourceQuotaStatus{
				Hard: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("200Gi"),
				},
				Used: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("0"),
					v1.ResourceMemory: resource.MustParse("0Gi"),
				},
			}
			ssn.NamespaceInfo["namespace1"] = &api.NamespaceInfo{
				Name:        "namespace1",
				QuotaStatus: quota1,
			}
			defer framework.CloseSession(ssn)
			plugin := &resourceQuotaPlugin{}
			plugin.OnSessionOpen(ssn)
			result := ssn.JobEnqueueable(test.job)
			if result != test.expectedOutcome {
				t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
					i, test.desc, test.expectedOutcome, result)
			}

		})
	}
}
