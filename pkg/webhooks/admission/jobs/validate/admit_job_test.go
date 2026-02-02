/*
Copyright 2019 The Volcano Authors.

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

package validate

import (
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1beta2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestValidateJobCreate(t *testing.T) {
	var policyExitCode int32 = -1
	var minAvailable int32 = 4
	namespace := "test"
	privileged := true
	highestTierAllowed := 1

	testCases := []struct {
		Name                     string
		PodLevelResourcesEnabled bool
		Job                      v1alpha1.Job
		ExpectErr                bool
		reviewResponse           admissionv1.AdmissionResponse
		ret                      string
	}{
		{
			Name:                     "simple-valid-job",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-valid-job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "task-1",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name: "simple-valid-job-with-policy",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-valid-job-with-policy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.PodEvictedEvent,
							Action: busv1alpha1.RestartTaskAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "validate valid-job with pod level resources",
			PodLevelResourcesEnabled: true,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-job-with-pod-level-resources",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Resources: &v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("4"),
											v1.ResourceMemory:                  resource.MustParse("8Gi"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("4"),
											v1.ResourceMemory:                  resource.MustParse("8Gi"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
										},
									},
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU:                     resource.MustParse("2"),
													v1.ResourceMemory:                  resource.MustParse("4Gi"),
													v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("1Gi"),
												},
												Limits: v1.ResourceList{
													v1.ResourceCPU:                     resource.MustParse("2"),
													v1.ResourceMemory:                  resource.MustParse("4Gi"),
													v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("1Gi"),
												},
											},
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.PodEvictedEvent,
							Action: busv1alpha1.RestartTaskAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// duplicate task name
		{
			Name:                     "duplicate-task-job",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "duplicate-task-job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "duplicated-task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
						{
							Name:     "duplicated-task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "duplicated task name duplicated-task-1",
			ExpectErr:      true,
		},
		// Duplicated Policy Event
		{
			Name:                     "job-policy-duplicated",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-policy-duplicated",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.PodFailedEvent,
							Action: busv1alpha1.AbortJobAction,
						},
						{
							Event:  busv1alpha1.PodFailedEvent,
							Action: busv1alpha1.RestartJobAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "duplicate",
			ExpectErr:      true,
		},
		// Min Available illegal
		{
			Name:                     "Min Available illegal",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-min-illegal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 2,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "job 'minAvailable' should not be greater than total replicas in tasks",
			ExpectErr:      true,
		},
		// Job Plugin illegal
		{
			Name:                     "Job Plugin illegal",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-plugin-illegal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Plugins: map[string][]string{
						"big_plugin": {},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "unable to find job plugin: big_plugin",
			ExpectErr:      true,
		},
		// Note: Test cases for the following validations have been removed:
		// - TTLSecondsAfterFinished < 0
		// - job.MinAvailable < 0
		// - job.MaxRetry < 0
		// - task.Replicas < 0
		// - task.MinAvailable < 0
		// - task name DNS1123 format
		// These validations are now enforced by CRD schema validation and VAP (ValidatingAdmissionPolicy).
		// no task specified in the job
		{
			Name:                     "no-task",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-task",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks:        []v1alpha1.TaskSpec{},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "No task specified in job spec",
			ExpectErr:      true,
		},
		// replica set less than zero
		{
			Name:                     "replica-lessThanZero",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replica-lessThanZero",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: -1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "job 'minAvailable' should not be greater than total replicas in tasks",
			ExpectErr:      true,
		},
		// Policy Event with exit code
		{
			Name:                     "job-policy-withExitCode",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-policy-withExitCode",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:    busv1alpha1.PodFailedEvent,
							Action:   busv1alpha1.AbortJobAction,
							ExitCode: &policyExitCode,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "must not specify event and exitCode simultaneously",
			ExpectErr:      true,
		},
		// Both policy event and exit code are nil
		{
			Name:                     "policy-noEvent-noExCode",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy-noEvent-noExCode",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action: busv1alpha1.AbortJobAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "either event and exitCode should be specified",
			ExpectErr:      true,
		},
		// invalid policy event
		{
			Name:                     "invalid-policy-event",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-policy-event",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.Event("someFakeEvent"),
							Action: busv1alpha1.AbortJobAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "invalid policy event",
			ExpectErr:      true,
		},
		// invalid policy action
		{
			Name:                     "invalid-policy-action",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-policy-action",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.PodEvictedEvent,
							Action: busv1alpha1.Action("someFakeAction"),
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "invalid policy action",
			ExpectErr:      true,
		},
		// policy exit-code zero
		{
			Name:                     "policy-extcode-zero",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy-extcode-zero",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action: busv1alpha1.AbortJobAction,
							ExitCode: func(i int32) *int32 {
								return &i
							}(int32(0)),
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "0 is not a valid error code",
			ExpectErr:      true,
		},
		// duplicate policy exit-code
		{
			Name:                     "duplicate-exitcode",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "duplicate-exitcode",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							ExitCode: func(i int32) *int32 {
								return &i
							}(int32(1)),
						},
						{
							ExitCode: func(i int32) *int32 {
								return &i
							}(int32(1)),
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "duplicate exitCode 1",
			ExpectErr:      true,
		},
		// Policy with any event and other events
		{
			Name:                     "job-policy-withExitCode",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-policy-withExitCode",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.AnyEvent,
							Action: busv1alpha1.AbortJobAction,
						},
						{
							Event:  busv1alpha1.PodFailedEvent,
							Action: busv1alpha1.RestartJobAction,
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "if there's * here, no other policy should be here",
			ExpectErr:      true,
		},
		// invalid mount volume
		{
			Name:                     "invalid-mount-volume",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-mount-volume",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.AnyEvent,
							Action: busv1alpha1.AbortJobAction,
						},
					},
					Volumes: []v1alpha1.VolumeSpec{
						{
							MountPath: "",
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            " mountPath is required;",
			ExpectErr:      true,
		},
		// duplicate mount volume
		{
			Name:                     "duplicate-mount-volume",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "duplicate-mount-volume",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.AnyEvent,
							Action: busv1alpha1.AbortJobAction,
						},
					},
					Volumes: []v1alpha1.VolumeSpec{
						{
							MountPath:       "/var",
							VolumeClaimName: "pvc1",
						},
						{
							MountPath:       "/var",
							VolumeClaimName: "pvc2",
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            " duplicated mountPath: /var;",
			ExpectErr:      true,
		},
		{
			Name:                     "volume without VolumeClaimName and VolumeClaim",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-volume",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Event:  busv1alpha1.AnyEvent,
							Action: busv1alpha1.AbortJobAction,
						},
					},
					Volumes: []v1alpha1.VolumeSpec{
						{
							MountPath: "/var",
						},
						{
							MountPath: "/var",
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            " either VolumeClaim or VolumeClaimName must be specified;",
			ExpectErr:      true,
		},
		// task Policy with any event and other events
		{
			Name:                     "taskpolicy-withAnyandOthrEvent",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "taskpolicy-withAnyandOthrEvent",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
							Policies: []v1alpha1.LifecyclePolicy{
								{
									Event:  busv1alpha1.AnyEvent,
									Action: busv1alpha1.AbortJobAction,
								},
								{
									Event:  busv1alpha1.PodFailedEvent,
									Action: busv1alpha1.RestartJobAction,
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "if there's * here, no other policy should be here",
			ExpectErr:      true,
		},
		// job with no queue created
		{
			Name:                     "job-with-noQueue",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-noQueue",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "jobQueue",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "unable to find job queue",
			ExpectErr:      true,
		},
		{
			Name:                     "job with privileged init container",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									InitContainers: []v1.Container{
										{
											Name:  "init-fake-name",
											Image: "busybox:1.24",
											SecurityContext: &v1.SecurityContext{
												Privileged: &privileged,
											},
										},
									},
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job with privileged container",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-job",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
											SecurityContext: &v1.SecurityContext{
												Privileged: &privileged,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job with valid task depends on",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-valid-task-depends-on",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "t1",
							Replicas: 1,
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2"},
							},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
						{
							Name:      "t2",
							Replicas:  1,
							DependsOn: nil,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job with invalid task depends on",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-invalid-task-depends-on",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "t1",
							Replicas: 1,
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t3"},
							},
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
						{
							Name:      "t2",
							Replicas:  1,
							DependsOn: nil,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "job has dependencies between tasks, but doesn't form a directed acyclic graph(DAG)",
			ExpectErr:      true,
		},
		// task PartitionSize set less than zero
		{
			Name:                     "PartitionSize-lessThanZero",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "PartitionSize-lessThanZero",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   -1,
								TotalPartitions: 8,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "'PartitionSize' must be greater than 0 in task: task-1, job: PartitionSize-lessThanZero",
			ExpectErr:      true,
		},
		// task TotalPartitions set less than zero
		{
			Name:                     "TotalPartitions-lessThanZero",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "TotalPartitions-lessThanZero",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   8,
								TotalPartitions: -1,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "'TotalPartitions' must be greater than 0 in task: task-1, job: TotalPartitions-lessThanZero",
			ExpectErr:      true,
		},
		// task-with-invalid-PartitionPolicy: replicas not equal to TotalPartitions*PartitionSize
		{
			Name:                     "task-with-invalid-PartitionPolicy",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-invalid-PartitionPolicy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   3,
								TotalPartitions: 2,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "'Replicas' are not equal to TotalPartitions*PartitionSize in task: task-1, job: task-with-invalid-PartitionPolicy",
			ExpectErr:      true,
		},
		// task-with-valid-PartitionPolicy: replicas equal to TotalPartitions*PartitionSize
		{
			Name:                     "task-with-valid-PartitionPolicy",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-valid-PartitionPolicy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "task-with-valid-NetworkTopology-and-PartitionPolicy",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-valid-NetworkTopology-and-PartitionPolicy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					NetworkTopology: &v1alpha1.NetworkTopologySpec{
						Mode:            v1alpha1.HardNetworkTopologyMode,
						HighestTierName: "volcano.sh/hypernode",
					},
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
								NetworkTopology: &v1alpha1.NetworkTopologySpec{
									Mode:            v1alpha1.HardNetworkTopologyMode,
									HighestTierName: "volcano.sh/hypernode",
								},
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job-with-invalid-NetworkTopology-and-PartitionPolicy",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-invalid-NetworkTopology-and-PartitionPolicy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					NetworkTopology: &v1alpha1.NetworkTopologySpec{
						Mode:               v1alpha1.HardNetworkTopologyMode,
						HighestTierAllowed: &highestTierAllowed,
						HighestTierName:    "volcano.sh/hypernode",
					},
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
								NetworkTopology: &v1alpha1.NetworkTopologySpec{
									Mode:               v1alpha1.HardNetworkTopologyMode,
									HighestTierAllowed: &highestTierAllowed,
									HighestTierName:    "volcano.sh/hypernode",
								},
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "must not specify 'highestTierAllowed' and 'highestTierName' in networkTopology simultaneously",
			ExpectErr:      true,
		},
		{
			Name:                     "job-with-invalid-NetworkTopology",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-invalid-NetworkTopology",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					NetworkTopology: &v1alpha1.NetworkTopologySpec{
						Mode:               v1alpha1.HardNetworkTopologyMode,
						HighestTierAllowed: &highestTierAllowed,
						HighestTierName:    "volcano.sh/hypernode",
					},
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "must not specify 'highestTierAllowed' and 'highestTierName' in networkTopology simultaneously",
			ExpectErr:      true,
		},
		{
			Name:                     "task-with-invalid-PartitionPolicy",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-invalid-PartitionPolicy",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
								NetworkTopology: &v1alpha1.NetworkTopologySpec{
									Mode:               v1alpha1.HardNetworkTopologyMode,
									HighestTierAllowed: &highestTierAllowed,
									HighestTierName:    "volcano.sh/hypernode",
								},
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "must not specify 'highestTierAllowed' and 'highestTierName' in networkTopology simultaneously",
			ExpectErr:      true,
		},
		{
			Name:                     "task-with-valid-PartitionPolicy-MinPartitions",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-valid-PartitionPolicy-MinPartitions",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   4,
								TotalPartitions: 2,
								MinPartitions:   1,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "task-with-invalid-PartitionPolicy-MinPartitions-not-equal",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-invalid-PartitionPolicy-MinPartitions",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:         "task-1",
							MinAvailable: &minAvailable,
							Replicas:     8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   2,
								TotalPartitions: 4,
								MinPartitions:   1,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "'MinAvailable' is not equal to MinPartitions*PartitionSize in task: task-1, job: task-with-invalid-PartitionPolicy-MinPartitions",
			ExpectErr:      true,
		},
		{
			Name:                     "task-with-valid-PartitionPolicy-MinPartitions-equal",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-with-valid-PartitionPolicy-MinPartitions-equal",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:         "task-1",
							MinAvailable: &minAvailable,
							Replicas:     8,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize:   2,
								TotalPartitions: 4,
								MinPartitions:   2,
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		// Test MPI plugin validation
		{
			Name:                     "job-with-mpi-plugin-valid-master",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-mpi-plugin-valid-master",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "launcher",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "launcher",
											Image: "mpi:latest",
										},
									},
								},
							},
						},
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "worker",
											Image: "mpi:latest",
										},
									},
								},
							},
						},
					},
					Plugins: map[string][]string{
						"mpi": {"--master=launcher"},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job-with-mpi-plugin-valid-master-without-worker",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-mpi-plugin-valid-master",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "launcher",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "launcher",
											Image: "mpi:latest",
										},
									},
								},
							},
						},
					},
					Plugins: map[string][]string{
						"mpi": {"--master=launcher"},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "job-with-mpi-plugin-invalid-master",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-mpi-plugin-invalid-master",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "worker",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "worker",
											Image: "mpi:latest",
										},
									},
								},
							},
						},
					},
					Plugins: map[string][]string{
						"mpi": {"--master=launcher"},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "The specified mpi master task was not found",
			ExpectErr:      true,
		},
		{
			Name:                     "validate invalid-job with pod level resources less than aggregate container resources",
			PodLevelResourcesEnabled: true,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-job-with-pod-level-resources",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Resources: &v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("2"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
											v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("2"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
											v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
										},
									},
									InitContainers: []v1.Container{
										{
											Name:  "fake-name-0",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
									},
									Containers: []v1.Container{
										{
											Name:  "fake-name-1",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
										{
											Name:  "fake-name-2",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "spec.task[0].template.spec.resources.requests[cpu]: Invalid value: \"2\": must be greater than or equal to aggregate container requests of 4",
			ExpectErr:      true,
		},
		{
			Name:                     "validate valid-job with pod level resources less than aggregate container resources, but PodLevelResources disabled",
			PodLevelResourcesEnabled: false,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-job-with-pod-level-resources",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Resources: &v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("2"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
											v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU:                     resource.MustParse("2"),
											v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
											v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
										},
									},
									InitContainers: []v1.Container{
										{
											Name:  "fake-name-0",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
									},
									Containers: []v1.Container{
										{
											Name:  "fake-name-1",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
										{
											Name:  "fake-name-2",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name:                     "validate invalid-job with pod level resource requests greater than limits",
			PodLevelResourcesEnabled: true,
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-job-with-pod-level-resources",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "default",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Resources: &v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("6"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("4"),
										},
									},
									Containers: []v1.Container{
										{
											Name:  "fake-name-1",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
												Limits: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
										{
											Name:  "fake-name-2",
											Image: "busybox:1.24",
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
												Limits: v1.ResourceList{
													v1.ResourceCPU: resource.MustParse("2"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: false},
			ret:            "spec.task[0].template.spec.resources.requests: Invalid value: \"6\": must be less than or equal to cpu limit of 4",
			ExpectErr:      true,
		},
	}

	defaultqueue := &schedulingv1beta2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: schedulingv1beta2.QueueSpec{
			Weight: 1,
		},
		Status: schedulingv1beta2.QueueStatus{
			State: schedulingv1beta2.QueueStateOpen,
		},
	}

	// create fake volcano clientset
	config.VolcanoClient = fakeclient.NewSimpleClientset(defaultqueue)
	informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)
	queueInformer := informerFactory.Scheduling().V1beta1().Queues()
	config.QueueLister = queueInformer.Lister()

	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	for informerType, ok := range informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			panic(fmt.Errorf("failed to sync cache: %v", informerType))
		}
	}
	defer close(stopCh)

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.PodLevelResources, testCase.PodLevelResourcesEnabled)
			ret := validateJobCreate(&testCase.Job, &testCase.reviewResponse)
			//fmt.Printf("test-case name:%s, ret:%v  testCase.reviewResponse:%v \n", testCase.Name, ret, testCase.reviewResponse)
			if testCase.ExpectErr == true && ret == "" {
				t.Errorf("Expect error msg :%s, but got nil.", testCase.ret)
			}
			if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
				t.Errorf("Expect Allowed as false but got true.")
			}
			if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
				t.Errorf("Expect error msg :%s, but got diff error %v", testCase.ret, ret)
			}

			if testCase.ExpectErr == false && ret != "" {
				t.Errorf("Expect no error, but got error %v", ret)
			}
			if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
				t.Errorf("Expect Allowed as true but got false. %v", testCase.reviewResponse)
			}
		})
	}
}

func TestValidateHierarchyCreate(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name           string
		Job            v1alpha1.Job
		ExpectErr      bool
		reviewResponse admissionv1.AdmissionResponse
		ret            string
	}{
		// job with root queue created
		{
			Name: "job-with-rootQueue",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-noQueue",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "root",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "can not submit job to root queue",
			ExpectErr:      true,
		},
		// job with non leaf queue created
		{
			Name: "job-with-parentQueue",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-parentQueue",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "parentQueue",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "can only submit job to leaf queue",
			ExpectErr:      true,
		},
		// job with leaf queue created
		{
			Name: "job-with-leafQueue",
			Job: v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-with-leafQueue",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					MinAvailable: 1,
					Queue:        "childQueue",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task-1",
							Replicas: 1,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"name": "test"},
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "fake-name",
											Image: "busybox:1.24",
										},
									},
								},
							},
						},
					},
				},
			},
			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
	}

	rootQueue := &schedulingv1beta2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "root",
		},
		Spec: schedulingv1beta2.QueueSpec{},
		Status: schedulingv1beta2.QueueStatus{
			State: schedulingv1beta2.QueueStateOpen,
		},
	}
	parentQueue := &schedulingv1beta2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parentQueue",
		},
		Spec: schedulingv1beta2.QueueSpec{
			Parent: "root",
		},
		Status: schedulingv1beta2.QueueStatus{
			State: schedulingv1beta2.QueueStateOpen,
		},
	}

	childQueue := &schedulingv1beta2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "childQueue",
		},
		Spec: schedulingv1beta2.QueueSpec{
			Parent: "parentQueue",
		},
		Status: schedulingv1beta2.QueueStatus{
			State: schedulingv1beta2.QueueStateOpen,
		},
	}

	// create fake volcano clientset
	config.VolcanoClient = fakeclient.NewSimpleClientset(rootQueue, parentQueue, childQueue)
	informerFactory := informers.NewSharedInformerFactory(config.VolcanoClient, 0)
	queueInformer := informerFactory.Scheduling().V1beta1().Queues()
	config.QueueLister = queueInformer.Lister()

	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)
	for informerType, ok := range informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			panic(fmt.Errorf("failed to sync cache: %v", informerType))
		}
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {

			ret := validateJobCreate(&testCase.Job, &testCase.reviewResponse)

			if testCase.ExpectErr == true && ret == "" {
				t.Errorf("Expect error msg :%s, but got nil.", testCase.ret)
			}
			if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
				t.Errorf("Expect Allowed as false but got true.")
			}
			if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
				t.Errorf("Expect error msg :%s, but got diff error %v", testCase.ret, ret)
			}

			if testCase.ExpectErr == false && ret != "" {
				t.Errorf("Expect no error, but got error %v", ret)
			}
			if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
				t.Errorf("Expect Allowed as true but got false. %v", testCase.reviewResponse)
			}
		})
	}
}

func TestValidateJobUpdate(t *testing.T) {
	testCases := []struct {
		name           string
		replicas       int32
		minAvailable   int32
		addTask        bool
		mutateTaskName bool
		mutateSpec     bool
		expectErr      bool
	}{
		{
			name:           "scale up",
			replicas:       6,
			minAvailable:   5,
			addTask:        false,
			mutateTaskName: false,
			mutateSpec:     false,
			expectErr:      false,
		},
		{
			name:           "invalid scale down with replicas less than minAvailable",
			replicas:       4,
			minAvailable:   5,
			addTask:        false,
			mutateTaskName: false,
			mutateSpec:     false,
			expectErr:      true,
		},
		{
			name:           "scale down",
			replicas:       4,
			minAvailable:   3,
			addTask:        false,
			mutateTaskName: false,
			mutateSpec:     false,
			expectErr:      false,
		},
		// Note: Test case for invalid minAvailable (< 0) has been removed.
		// This validation is now enforced by CRD schema validation and VAP (ValidatingAdmissionPolicy).
		{
			name:           "invalid add task",
			replicas:       4,
			minAvailable:   5,
			addTask:        true,
			mutateTaskName: false,
			mutateSpec:     false,
			expectErr:      true,
		},
		{
			name:           "invalid mutate task's fields other than replicas",
			replicas:       5,
			minAvailable:   5,
			addTask:        false,
			mutateTaskName: true,
			mutateSpec:     false,
			expectErr:      true,
		},
		{
			name:           "invalid mutate job's spec other than minAvailable",
			replicas:       5,
			minAvailable:   5,
			addTask:        false,
			mutateTaskName: false,
			mutateSpec:     true,
			expectErr:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			old := newJob()
			new := newJob()
			new.ResourceVersion = "502593"
			new.Status.Succeeded = 2

			new.Spec.MinAvailable = tc.minAvailable
			new.Spec.Tasks[0].Replicas = tc.replicas

			if tc.addTask {
				new.Spec.Tasks = append(new.Spec.Tasks, v1alpha1.TaskSpec{
					Name:     "task-2",
					Replicas: 5,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "test"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "fake-name",
									Image: "busybox:1.24",
								},
							},
						},
					},
				})
			}
			if tc.mutateTaskName {
				new.Spec.Tasks[0].Name = "mutated-name"
			}
			if tc.mutateSpec {
				new.Spec.Queue = "mutated-queue"
			}

			err := validateJobUpdate(old, new)
			if err != nil && !tc.expectErr {
				t.Errorf("Expected no error, but got: %v", err)
			}
			if err == nil && tc.expectErr {
				t.Errorf("Expected error, but got none")
			}
		})
	}

}

func newJob() *v1alpha1.Job {
	return &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-job",
			Namespace: "default",
		},
		Spec: v1alpha1.JobSpec{
			MinAvailable: 5,
			Queue:        "default",
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:     "task-1",
					Replicas: 5,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "test"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "fake-name",
									Image: "busybox:1.24",
								},
							},
						},
					},
				},
			},
			Policies: []v1alpha1.LifecyclePolicy{
				{
					Event:  busv1alpha1.PodEvictedEvent,
					Action: busv1alpha1.RestartTaskAction,
				},
			},
		},
	}
}

func TestValidateTaskTopoPolicy(t *testing.T) {
	testCases := []struct {
		name     string
		taskSpec v1alpha1.TaskSpec
		expect   string
	}{
		{
			name: "test-1",
			taskSpec: v1alpha1.TaskSpec{
				Name:           "task-1",
				Replicas:       5,
				TopologyPolicy: v1alpha1.Restricted,
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"name": "test"},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										v1.ResourceCPU:    *resource.NewQuantity(1, ""),
										v1.ResourceMemory: *resource.NewQuantity(2000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			expect: "",
		},
		{
			name: "test-2",
			taskSpec: v1alpha1.TaskSpec{
				Name:           "task-2",
				TopologyPolicy: v1alpha1.Restricted,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										v1.ResourceCPU:    *resource.NewMilliQuantity(500, resource.DecimalSI),
										v1.ResourceMemory: *resource.NewQuantity(2000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			expect: "the cpu request isn't  an integer",
		},
	}

	for _, testcase := range testCases {
		msg := validateTaskTopoPolicy(testcase.taskSpec, 0)
		if !strings.Contains(msg, testcase.expect) {
			t.Errorf("%s failed.", testcase.name)
		}
	}
}
