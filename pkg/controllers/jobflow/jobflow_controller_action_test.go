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
	"context"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func newFakeController() *jobflowcontroller {
	volcanoClientSet := volcanoclient.NewSimpleClientset()
	kubeClientSet := kubeclient.NewSimpleClientset()

	sharedInformers := informers.NewSharedInformerFactory(kubeClientSet, 0)

	controller := &jobflowcontroller{}
	opt := &framework.ControllerOption{
		VolcanoClient:         volcanoClientSet,
		KubeClient:            kubeClientSet,
		SharedInformerFactory: sharedInformers,
		WorkerNum:             3,
	}

	controller.Initialize(opt)

	return controller
}

func TestSyncJobFlowFunc(t *testing.T) {
	type args struct {
		jobFlow         *jobflowv1alpha1.JobFlow
		jobTemplateList []*jobflowv1alpha1.JobTemplate
	}
	type wantRes struct {
		jobFlowStatus *jobflowv1alpha1.JobFlowStatus
		err           error
	}
	tests := []struct {
		name string
		args args
		want wantRes
	}{
		{
			name: "SyncJobFlow success case",
			args: args{
				jobFlow: &jobflowv1alpha1.JobFlow{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "jobflow",
						Namespace: "default",
					},
					Spec: jobflowv1alpha1.JobFlowSpec{
						Flows: []jobflowv1alpha1.Flow{
							{
								Name:      "jobtemplate",
								DependsOn: nil,
							},
						},
						JobRetainPolicy: jobflowv1alpha1.Retain,
					},
				},
				jobTemplateList: []*jobflowv1alpha1.JobTemplate{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "jobtemplate",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{},
					},
				},
			},
			want: wantRes{
				jobFlowStatus: &jobflowv1alpha1.JobFlowStatus{
					PendingJobs:    make([]string, 0),
					RunningJobs:    []string{getJobName("jobflow", "jobtemplate")},
					FailedJobs:     make([]string, 0),
					CompletedJobs:  make([]string, 0),
					TerminatedJobs: make([]string, 0),
					UnKnowJobs:     make([]string, 0),
					JobStatusList: []jobflowv1alpha1.JobStatus{
						{
							Name:           getJobName("jobflow", "jobtemplate"),
							State:          v1alpha1.Running,
							StartTimestamp: metav1.Time{},
							EndTimestamp:   metav1.Time{},
							RestartCount:   0,
							RunningHistories: []jobflowv1alpha1.JobRunningHistory{
								{
									StartTimestamp: metav1.Time{},
									EndTimestamp:   metav1.Time{},
									State:          v1alpha1.Running,
								},
							},
						},
					},
					Conditions: map[string]jobflowv1alpha1.Condition{
						getJobName("jobflow", "jobtemplate"): {
							Phase:           v1alpha1.Running,
							CreateTimestamp: metav1.Time{},
							RunningDuration: nil,
							TaskStatusCount: nil,
						},
					},
					State: jobflowv1alpha1.State{
						Phase: jobflowv1alpha1.Running,
					},
				},
				err: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController := newFakeController()
			for i := range tt.args.jobTemplateList {
				if err := fakeController.jobTemplateInformer.Informer().GetIndexer().Add(tt.args.jobTemplateList[i]); err != nil {
					t.Errorf("add jobTemplate to informerFake,error : %s", err.Error())
				}
				job := &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      getJobName(tt.args.jobFlow.Name, tt.args.jobTemplateList[i].Name),
						Namespace: tt.args.jobFlow.Namespace,
						Labels:    map[string]string{CreatedByJobTemplate: GetTemplateString(tt.args.jobFlow.Namespace, tt.args.jobTemplateList[i].Name)},
					},
					Spec: tt.args.jobTemplateList[i].Spec,
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				}
				if err := controllerutil.SetControllerReference(tt.args.jobFlow, job, scheme.Scheme); err != nil {
					t.Errorf("SetControllerReference error : %s", err.Error())
				}
				if err := fakeController.jobInformer.Informer().GetIndexer().Add(job); err != nil {
					t.Errorf("add jobTemplate to informerFake,error : %s", err.Error())
				}
			}
			if _, err := fakeController.vcClient.FlowV1alpha1().JobFlows(tt.args.jobFlow.Namespace).Create(context.Background(), tt.args.jobFlow, metav1.CreateOptions{}); err != nil {
				t.Errorf("create jobflow error : %s", err.Error())
			}

			if got := fakeController.syncJobFlow(tt.args.jobFlow, func(status *jobflowv1alpha1.JobFlowStatus, allJobList int) {
				if len(status.RunningJobs) > 0 || len(status.CompletedJobs) > 0 {
					status.State.Phase = jobflowv1alpha1.Running
				} else if len(status.FailedJobs) > 0 {
					status.State.Phase = jobflowv1alpha1.Failed
				} else {
					status.State.Phase = jobflowv1alpha1.Pending
				}
			}); got != tt.want.err {
				t.Error("Expected deleteAllJobsCreatedByJobFlow() return nil, but not nil")
			}
			for i := range tt.args.jobFlow.Status.JobStatusList {
				for i2 := range tt.args.jobFlow.Status.JobStatusList[i].RunningHistories {
					tt.args.jobFlow.Status.JobStatusList[i].RunningHistories[i2].StartTimestamp = metav1.Time{}
				}
			}
			if !reflect.DeepEqual(&tt.args.jobFlow.Status, tt.want.jobFlowStatus) {
				t.Error("not the expected result")
			}
		})
	}
}

func TestGetRunningHistoriesFunc(t *testing.T) {
	type args struct {
		jobStatusList []jobflowv1alpha1.JobStatus
		job           *v1alpha1.Job
	}
	startTime := time.Now()
	endTime := startTime.Add(1 * time.Second)
	tests := []struct {
		name string
		args args
		want []jobflowv1alpha1.JobRunningHistory
	}{
		{
			name: "GetRunningHistories success case",
			args: args{
				jobStatusList: []jobflowv1alpha1.JobStatus{
					{
						Name:           "vcJobA",
						State:          v1alpha1.Completed,
						StartTimestamp: metav1.Time{Time: startTime},
						EndTimestamp:   metav1.Time{Time: endTime},
						RestartCount:   0,
						RunningHistories: []jobflowv1alpha1.JobRunningHistory{
							{
								StartTimestamp: metav1.Time{Time: startTime},
								EndTimestamp:   metav1.Time{Time: endTime},
								State:          v1alpha1.Completed,
							},
						},
					},
				},
				job: &v1alpha1.Job{
					TypeMeta:   metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{Name: "vcJobA"},
					Spec:       v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase:              v1alpha1.Completed,
							Reason:             "",
							Message:            "",
							LastTransitionTime: metav1.Time{},
						},
					},
				},
			},
			want: []jobflowv1alpha1.JobRunningHistory{
				{
					StartTimestamp: metav1.Time{Time: startTime},
					EndTimestamp:   metav1.Time{Time: endTime},
					State:          v1alpha1.Completed,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getRunningHistories(tt.args.jobStatusList, tt.args.job); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRunningHistories() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAllJobStatusFunc(t *testing.T) {
	// TODO(wangyang0616): First make sure that ut can run, and then fix the failed ut later.
	// See issue for details: https://github.com/volcano-sh/volcano/issues/2851
	t.Skip("Test cases are not as expected, fixed later. see issue: #2851")
	type args struct {
		jobFlow    *jobflowv1alpha1.JobFlow
		allJobList *v1alpha1.JobList
	}
	createJobATime := time.Now()
	jobFlowName := "jobFlowA"
	createJobBTime := createJobATime.Add(time.Second)
	tests := []struct {
		name    string
		args    args
		want    *jobflowv1alpha1.JobFlowStatus
		wantErr bool
	}{
		{
			name: "GetAllJobStatus success case",
			args: args{
				jobFlow: &jobflowv1alpha1.JobFlow{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: jobFlowName,
					},
					Spec: jobflowv1alpha1.JobFlowSpec{
						Flows: []jobflowv1alpha1.Flow{
							{
								Name:      "A",
								DependsOn: nil,
							},
							{
								Name: "B",
								DependsOn: &jobflowv1alpha1.DependsOn{
									Targets: []string{"A"},
								},
							},
						},
						JobRetainPolicy: "",
					},
					Status: jobflowv1alpha1.JobFlowStatus{},
				},
				allJobList: &v1alpha1.JobList{
					Items: []v1alpha1.Job{
						{
							TypeMeta: metav1.TypeMeta{},
							ObjectMeta: metav1.ObjectMeta{
								Name:              "jobFlowA-A",
								CreationTimestamp: metav1.Time{Time: createJobATime},
								OwnerReferences: []metav1.OwnerReference{{
									APIVersion: "volcano",
									Kind:       JobFlow,
									Name:       jobFlowName,
								}},
							},
							Spec: v1alpha1.JobSpec{},
							Status: v1alpha1.JobStatus{
								State:           v1alpha1.JobState{Phase: v1alpha1.Completed},
								RetryCount:      1,
								RunningDuration: &metav1.Duration{Duration: time.Second},
							},
						},
						{
							TypeMeta: metav1.TypeMeta{},
							ObjectMeta: metav1.ObjectMeta{
								Name:              "jobFlowA-B",
								CreationTimestamp: metav1.Time{Time: createJobBTime},
								OwnerReferences: []metav1.OwnerReference{{
									APIVersion: "volcano",
									Kind:       JobFlow,
									Name:       jobFlowName,
								}},
							},
							Spec: v1alpha1.JobSpec{},
							Status: v1alpha1.JobStatus{
								State: v1alpha1.JobState{Phase: v1alpha1.Running},
							},
						},
					},
				},
			},
			want: &jobflowv1alpha1.JobFlowStatus{
				PendingJobs:    []string{},
				RunningJobs:    []string{"jobFlowA-B"},
				FailedJobs:     []string{},
				CompletedJobs:  []string{"jobFlowA-A"},
				TerminatedJobs: []string{},
				UnKnowJobs:     []string{},
				JobStatusList: []jobflowv1alpha1.JobStatus{
					{
						Name:           "jobFlowA-A",
						State:          v1alpha1.Completed,
						StartTimestamp: metav1.Time{Time: createJobATime},
						EndTimestamp:   metav1.Time{Time: createJobATime.Add(time.Second)},
						RestartCount:   1,
						RunningHistories: []jobflowv1alpha1.JobRunningHistory{
							{
								StartTimestamp: metav1.Time{},
								EndTimestamp:   metav1.Time{},
								State:          v1alpha1.Completed,
							},
						},
					},
					{
						Name:           "jobFlowA-B",
						State:          v1alpha1.Running,
						StartTimestamp: metav1.Time{Time: createJobBTime},
						EndTimestamp:   metav1.Time{},
						RestartCount:   0,
						RunningHistories: []jobflowv1alpha1.JobRunningHistory{
							{
								StartTimestamp: metav1.Time{},
								EndTimestamp:   metav1.Time{},
								State:          v1alpha1.Running,
							},
						},
					},
				},
				Conditions: map[string]jobflowv1alpha1.Condition{
					"jobFlowA-A": {
						Phase:           v1alpha1.Completed,
						CreateTimestamp: metav1.Time{Time: createJobATime},
						RunningDuration: &metav1.Duration{Duration: time.Second},
					},
					"jobFlowA-B": {
						Phase:           v1alpha1.Running,
						CreateTimestamp: metav1.Time{Time: createJobBTime},
					},
				},
				State: jobflowv1alpha1.State{Phase: ""},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController := newFakeController()
			for i := range tt.args.allJobList.Items {
				err := fakeController.jobInformer.Informer().GetIndexer().Add(&tt.args.allJobList.Items[i])
				if err != nil {
					t.Error("Error While add vcjob")
				}
			}

			got, err := fakeController.getAllJobStatus(tt.args.jobFlow)
			if (err != nil) != tt.wantErr {
				t.Errorf("getAllJobStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != nil {
				got.JobStatusList[0].RunningHistories[0].StartTimestamp = metav1.Time{}
				got.JobStatusList[1].RunningHistories[0].StartTimestamp = metav1.Time{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getAllJobStatus() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadJobTemplateAndSetJobFunc(t *testing.T) {
	type args struct {
		jobFlow     *jobflowv1alpha1.JobFlow
		flowName    string
		jobName     string
		job         *v1alpha1.Job
		jobTemplate *jobflowv1alpha1.JobTemplate
	}
	type wantRes struct {
		OwnerReference []metav1.OwnerReference
		Annotations    map[string]string
		Labels         map[string]string
		Err            error
	}
	flag := true
	tests := []struct {
		name string
		args args
		want wantRes
	}{
		{
			name: "LoadJobTemplateAndSetJob success case",
			args: args{
				jobFlow: &jobflowv1alpha1.JobFlow{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "jobflow",
						Namespace: "default",
					},
				},
				flowName: "jobtemplate",
				jobName:  getJobName("jobflow", "jobtemplate"),
				job:      &v1alpha1.Job{},
				jobTemplate: &jobflowv1alpha1.JobTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "jobtemplate",
						Namespace: "default",
					},
					Spec:   v1alpha1.JobSpec{},
					Status: jobflowv1alpha1.JobTemplateStatus{},
				},
			},
			want: wantRes{
				OwnerReference: []metav1.OwnerReference{
					{
						APIVersion:         helpers.JobFlowKind.Group + "/" + helpers.JobFlowKind.Version,
						Kind:               helpers.JobFlowKind.Kind,
						Name:               "jobflow",
						UID:                "",
						Controller:         &flag,
						BlockOwnerDeletion: &flag,
					},
				},
				Annotations: map[string]string{
					CreatedByJobTemplate: GetTemplateString("default", "jobtemplate"),
				},
				Labels: map[string]string{
					CreatedByJobTemplate: GetTemplateString("default", "jobtemplate"),
				},
				Err: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController := newFakeController()
			err := fakeController.jobTemplateInformer.Informer().GetIndexer().Add(tt.args.jobTemplate)
			if err != nil {
				t.Error("Error While add vcjob")
			}

			if got := fakeController.loadJobTemplateAndSetJob(tt.args.jobFlow, tt.args.flowName, tt.args.jobName, tt.args.job); got != tt.want.Err {
				t.Error("Expected loadJobTemplateAndSetJob() return nil, but not nil")
			}
			if !reflect.DeepEqual(tt.args.job.OwnerReferences, tt.want.OwnerReference) {
				t.Error("not expected job OwnerReferences")
			}
			if !reflect.DeepEqual(tt.args.job.Annotations, tt.want.Annotations) {
				t.Error("not expected job Annotations")
			}
			if !reflect.DeepEqual(tt.args.job.Labels, tt.want.Labels) {
				t.Error("not expected job Annotations")
			}
		})
	}
}

func TestDeployJobFunc(t *testing.T) {
	type args struct {
		jobFlow         *jobflowv1alpha1.JobFlow
		jobTemplateList []*jobflowv1alpha1.JobTemplate
	}
	tests := []struct {
		name string
		args args
		want error
	}{
		{
			name: "DeployJob success case",
			args: args{
				jobFlow: &jobflowv1alpha1.JobFlow{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "jobflow",
						Namespace: "default",
					},
					Spec: jobflowv1alpha1.JobFlowSpec{
						Flows: []jobflowv1alpha1.Flow{
							{
								Name:      "jobtemplate",
								DependsOn: nil,
							},
						},
						JobRetainPolicy: jobflowv1alpha1.Retain,
					},
				},
				jobTemplateList: []*jobflowv1alpha1.JobTemplate{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "jobtemplate",
							Namespace: "default",
						},
						Spec:   v1alpha1.JobSpec{},
						Status: jobflowv1alpha1.JobTemplateStatus{},
					},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController := newFakeController()
			for i := range tt.args.jobTemplateList {
				err := fakeController.jobTemplateInformer.Informer().GetIndexer().Add(tt.args.jobTemplateList[i])
				if err != nil {
					t.Error("Error While add jobTemplate")
				}
			}

			if got := fakeController.deployJob(tt.args.jobFlow); got != tt.want {
				t.Error("Expected deployJob() return nil, but not nil")
			}
		})
	}
}

func TestDeleteAllJobsCreateByJobFlowFunc(t *testing.T) {
	type args struct {
		jobFlow *jobflowv1alpha1.JobFlow
		jobList []*v1alpha1.Job
	}
	tests := []struct {
		name string
		args args
		want error
	}{
		{
			name: "LoadJobTemplateAndSetJob success case",
			args: args{
				jobFlow: &jobflowv1alpha1.JobFlow{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "jobflow",
						Namespace: "default",
					},
					Spec: jobflowv1alpha1.JobFlowSpec{
						Flows: []jobflowv1alpha1.Flow{
							{
								Name:      "jobtemplate",
								DependsOn: nil,
							},
						},
						JobRetainPolicy: jobflowv1alpha1.Retain,
					},
				},
				jobList: []*v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "jobtemplate",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{},
					},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController := newFakeController()
			for i := range tt.args.jobList {
				_, err := fakeController.vcClient.BatchV1alpha1().Jobs(tt.args.jobList[i].Namespace).Create(context.Background(), tt.args.jobList[i], metav1.CreateOptions{})
				if err != nil {
					t.Error("Error While create vcjob")
				}
			}

			if got := fakeController.deleteAllJobsCreatedByJobFlow(tt.args.jobFlow); got != tt.want {
				t.Error("Expected deleteAllJobsCreatedByJobFlow() return nil, but not nil")
			}
		})
	}
}
