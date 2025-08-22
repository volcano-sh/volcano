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

package validate

import (
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1beta2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informers "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestValidateCronjobSpec(t *testing.T) {
	testCases := []struct {
		Name      string
		CronJob   v1alpha1.CronJob
		ExpectErr bool
		ret       string
	}{
		{
			Name: "validate valid-cronjob",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("1a2b3c"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "* * * * *",
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: false,
			ret:       "",
		},
		{
			Name: "validate non-standard scheduled",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "@every 1m",
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: false,
			ret:       "",
		},
		{
			Name: "validate correct timeZone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("Pacific/Honolulu"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: false,
			ret:       "",
		},
		{
			Name: "validate contain TZ schedule",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "TZ=UTC 0 20 * * *",
					TimeZone:          ptr.To("Pacific/Honolulu"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " schedule should not contain TZ or CRON_TZ, TZ should only be set in the timeZone field",
		},
		{
			Name: "validate error schedule",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "error schedule",
					TimeZone:          ptr.To("Pacific/Honolulu"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       "schedule is not a valid cron expression",
		},
		{
			Name: "validate empty schedule",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "",
					TimeZone:          ptr.To("Pacific/Honolulu"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " schedule is required, but got empty",
		},
		{
			Name: "validate unknown . timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("Asia/./Shanghai"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " unknown timeZone",
		},
		{
			Name: "validate unknown _ timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("America/-NewYork"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " unknown timeZone",
		},
		{
			Name: "validate unknown space timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("New York"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " unknown timeZone",
		},
		{
			Name: "validate unknown ! timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("UTC!"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " unknown timeZone",
		},
		{
			Name: "validate Local timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "*/5 * * * *",
					TimeZone:          ptr.To("Local"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " timeZone can't be Local, and must be defined in https://www.iana.org/time-zones",
		},
		{
			Name: "validate empty timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "",
					TimeZone:          ptr.To(""),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " timeZone must be nil or non-empty string, got empty string",
		},
		{
			Name: "validate error timezone",
			CronJob: v1alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cronvcjob",
					Namespace: metav1.NamespaceDefault,
					UID:       types.UID("123456"),
				},
				Spec: v1alpha1.CronJobSpec{
					Schedule:          "",
					TimeZone:          ptr.To("Amerrica/New_York"),
					ConcurrencyPolicy: v1alpha1.AllowConcurrent,
					JobTemplate: v1alpha1.JobTemplateSpec{
						Spec: getJobTemplate(),
					},
				},
			},
			ExpectErr: true,
			ret:       " invalid timeZone",
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
			ret := validateCronjobSpec(&testCase.CronJob.Spec, testCase.CronJob.Namespace)
			//fmt.Printf("test-case name:%s, ret:%v  testCase.reviewResponse:%v \n", testCase.Name, ret,testCase.reviewResponse)
			if testCase.ExpectErr == true && ret == "" {
				t.Errorf("Expect error msg :%s, but got nil.", testCase.ret)
			}
			if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
				t.Errorf("Expect error msg :%s, but got diff error %v", testCase.ret, ret)
			}
			if testCase.ExpectErr == false && ret != "" {
				t.Errorf("Expect no error, but got error %v", ret)
			}
		})
	}
}
func getJobTemplate() v1alpha1.JobSpec {
	return v1alpha1.JobSpec{
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
	}
}
func TestValidateCronJobName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid name",
			input:    "my-cronjob",
			expected: "",
		},
		{
			name:     "exactly max length",
			input:    strings.Repeat("a", 52),
			expected: "",
		},
		{
			name:     "too long name",
			input:    strings.Repeat("a", 65),
			expected: "must be no more than 52 characters",
		},
		{
			name:     "invalid chars",
			input:    "-UPPERCASE",
			expected: "invalid cronJob name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validateCronJobName(tt.input)
			if tt.expected == "" && msg != "" {
				t.Errorf("Unexpected error: %q", msg)
			} else if tt.expected != "" && !strings.Contains(msg, tt.expected) {
				t.Errorf("Expected %q in error, got %q", tt.expected, msg)
			}
		})
	}
}
