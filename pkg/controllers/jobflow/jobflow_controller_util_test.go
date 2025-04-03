/*
Copyright 2022 The Volcano Authors.

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

package jobflow

import (
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestGetJobNameFunc(t *testing.T) {
	type args struct {
		jobFlowName     string
		jobTemplateName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "GetJobName success case",
			args: args{
				jobFlowName:     "jobFlowA",
				jobTemplateName: "jobTemplateA",
			},
			want: "jobFlowA-jobTemplateA",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobName(tt.args.jobFlowName, tt.args.jobTemplateName); got != tt.want {
				t.Errorf("getJobName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetConnectionOfJobAndJobTemplate(t *testing.T) {
	type args struct {
		namespace string
		name      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "TestGetConnectionOfJobAndJobTemplate",
			args: args{
				namespace: "default",
				name:      "flow",
			},
			want: "default.flow",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetTemplateString(tt.args.namespace, tt.args.name); got != tt.want {
				t.Errorf("GetTemplateString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetJobFlowNameByJob(t *testing.T) {
	type args struct {
		job *batch.Job
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "TestGetConnectionOfJobAndJobTemplate",
			args: args{
				job: &batch.Job{
					TypeMeta: v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{
						OwnerReferences: []v1.OwnerReference{
							{
								APIVersion:         "flow.volcano.sh/v1alpha1",
								Kind:               JobFlow,
								Name:               "jobflowtest",
								UID:                "",
								Controller:         nil,
								BlockOwnerDeletion: nil,
							},
						},
					},
					Spec:   batch.JobSpec{},
					Status: batch.JobStatus{},
				},
			},
			want: "jobflowtest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobFlowNameByJob(tt.args.job); got != tt.want {
				t.Errorf("getJobFlowNameByJob() = %v, want %v", got, tt.want)
			}
		})
	}
}
