/*
Copyright 2025 The Volcano Authors.

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

package inqueuetimeout

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
)

func TestInqueueTimeoutPlugin(t *testing.T) {
	tests := []struct {
		name              string
		arguments         framework.Arguments
		job               *api.JobInfo
		expectedDequeue   bool
	}{
		{
			name: "job inqueue with expired timeout should be dequeued",
			arguments: framework.Arguments{
				InqueueTimeout: "1m",
			},
			job: &api.JobInfo{
				Name:      "job1",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
						Status: scheduling.PodGroupStatus{
							Phase: scheduling.PodGroupInqueue,
							Conditions: []scheduling.PodGroupCondition{
								{
									Type:               scheduling.PodGroupInqueuedType,
									Status:             v1.ConditionTrue,
									LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
								},
							},
						},
					},
				},
			},
			expectedDequeue: true,
		},
		{
			name: "job inqueue with unexpired timeout should not be dequeued",
			arguments: framework.Arguments{
				InqueueTimeout: "10m",
			},
			job: &api.JobInfo{
				Name:      "job2",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
						Status: scheduling.PodGroupStatus{
							Phase: scheduling.PodGroupInqueue,
							Conditions: []scheduling.PodGroupCondition{
								{
									Type:               scheduling.PodGroupInqueuedType,
									Status:             v1.ConditionTrue,
									LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
								},
							},
						},
					},
				},
			},
			expectedDequeue: false,
		},
		{
			name: "per-PodGroup annotation overrides global timeout",
			arguments: framework.Arguments{
				InqueueTimeout: "10m",
			},
			job: &api.JobInfo{
				Name:      "job3",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								AnnotationInqueueTimeout: "30s",
							},
						},
						Status: scheduling.PodGroupStatus{
							Phase: scheduling.PodGroupInqueue,
							Conditions: []scheduling.PodGroupCondition{
								{
									Type:               scheduling.PodGroupInqueuedType,
									Status:             v1.ConditionTrue,
									LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
								},
							},
						},
					},
				},
			},
			expectedDequeue: true,
		},
		{
			name:      "no timeout configured should not dequeue",
			arguments: framework.Arguments{},
			job: &api.JobInfo{
				Name:      "job4",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
						Status: scheduling.PodGroupStatus{
							Phase: scheduling.PodGroupInqueue,
							Conditions: []scheduling.PodGroupCondition{
								{
									Type:               scheduling.PodGroupInqueuedType,
									Status:             v1.ConditionTrue,
									LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
								},
							},
						},
					},
				},
			},
			expectedDequeue: false,
		},
		{
			name: "job without Inqueued condition should not be dequeued",
			arguments: framework.Arguments{
				InqueueTimeout: "1m",
			},
			job: &api.JobInfo{
				Name:      "job5",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
						Status: scheduling.PodGroupStatus{
							Phase:      scheduling.PodGroupInqueue,
							Conditions: []scheduling.PodGroupCondition{},
						},
					},
				},
			},
			expectedDequeue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               PluginName,
							EnabledJobEnqueued: &trueValue,
							Arguments:          tt.arguments,
						},
					},
				},
			}
			tcs := uthelper.TestCommonStruct{
				Name:    tt.name,
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			}
			ssn := tcs.RegisterSession(tiers, nil)
			defer tcs.Close()

			got := ssn.JobDequeueable(tt.job)
			if got != tt.expectedDequeue {
				t.Errorf("case %q: expected dequeueable=%v, got %v", tt.name, tt.expectedDequeue, got)
			}
		})
	}
}

func TestGetInqueueTimestamp(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		job      *api.JobInfo
		expected time.Time
	}{
		{
			name: "returns timestamp from Inqueued condition",
			job: &api.JobInfo{
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						Status: scheduling.PodGroupStatus{
							Conditions: []scheduling.PodGroupCondition{
								{
									Type:               scheduling.PodGroupInqueuedType,
									LastTransitionTime: metav1.NewTime(now),
								},
							},
						},
					},
				},
			},
			expected: now,
		},
		{
			name: "returns zero time when no Inqueued condition",
			job: &api.JobInfo{
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						Status: scheduling.PodGroupStatus{
							Conditions: []scheduling.PodGroupCondition{},
						},
					},
				},
			},
			expected: time.Time{},
		},
		{
			name:     "returns zero time when PodGroup is nil",
			job:      &api.JobInfo{},
			expected: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getInqueueTimestamp(tt.job)
			if !got.Equal(tt.expected) {
				t.Errorf("case %q: expected %v, got %v", tt.name, tt.expected, got)
			}
		})
	}
}
