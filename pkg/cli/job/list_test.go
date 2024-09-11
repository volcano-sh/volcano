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

package job

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

func TestListJob(t *testing.T) {

	var (
		namespaceFilter = "default"
		schedulerFilter = "volcano"
		queueFilter     = "test-queue"
		selectorFilter  = "test-job"
	)

	testCases := []struct {
		Name           string
		Response       interface{}
		AllNamespace   bool
		Scheduler      string
		Selector       string
		QueueName      string
		Namespace      string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			Name: "Normal Case",
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue: "default",
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Name       Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
test-job   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
		{
			Name:      "Normal Case with queueName filter",
			QueueName: queueFilter,
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue: "default",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-queue",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue: queueFilter,
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Name         Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
test-queue   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
		{
			Name:      "Normal Case with namespace filter",
			Namespace: namespaceFilter,
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: namespaceFilter,
						},
						Spec: v1alpha1.JobSpec{
							Queue: "default",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-queue",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue: "test-queue",
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Name         Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
test-job     0001-01-01                 Batch       0           0     0         0         0           0         0           0         
test-queue   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
		{
			Name:         "Normal Case with all namespace filter",
			AllNamespace: true,
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "kube-sysyem",
						},
						Spec: v1alpha1.JobSpec{
							Queue: "default",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-queue",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue: "test-queue",
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Namespace     Name         Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
kube-sysyem   test-job     0001-01-01                 Batch       0           0     0         0         0           0         0           0         
default       test-queue   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
		{
			Name:      "Normal Case with scheduler filter",
			Scheduler: schedulerFilter,
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "kube-sysyem",
						},
						Spec: v1alpha1.JobSpec{
							Queue:         "default",
							SchedulerName: "test-scheduler",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-queue",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue:         "test-queue",
							SchedulerName: schedulerFilter,
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Name         Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
test-queue   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
		{
			Name:     "Normal Case with selector filter",
			Selector: selectorFilter,
			Response: &v1alpha1.JobList{
				Items: []v1alpha1.Job{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      selectorFilter,
							Namespace: "kube-sysyem",
						},
						Spec: v1alpha1.JobSpec{
							Queue:         "default",
							SchedulerName: "test-scheduler",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-queue",
							Namespace: "default",
						},
						Spec: v1alpha1.JobSpec{
							Queue:         "test-queue",
							SchedulerName: schedulerFilter,
						},
					},
				},
			},
			ExpectedErr: nil,
			ExpectedOutput: `Name       Creation       Phase       JobType     Replicas    Min   Pending   Running   Succeeded   Failed    Unknown     RetryCount
test-job   0001-01-01                 Batch       0           0     0         0         0           0         0           0`,
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {

			server := util.CreateTestServer(testcase.Response)
			defer server.Close()

			listJobFlags = &listFlags{
				CommonFlags: util.CommonFlags{
					Master: server.URL,
				},
				Namespace:     testcase.Namespace,
				allNamespace:  testcase.AllNamespace,
				selector:      testcase.Selector,
				SchedulerName: testcase.Scheduler,
				QueueName:     testcase.QueueName,
			}
			r, oldStdout := util.RedirectStdout()
			defer r.Close()
			err := ListJobs(context.TODO())
			gotOutput := util.CaptureOutput(r, oldStdout)
			if !reflect.DeepEqual(err, testcase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testcase.Name, err, testcase.ExpectedErr)
			}
			if gotOutput != testcase.ExpectedOutput {
				fmt.Println(gotOutput)
				t.Errorf("test case: %s failed: got: %s, want: %s", testcase.Name, gotOutput, testcase.ExpectedOutput)
			}
		})
	}
}

func TestInitListFlags(t *testing.T) {
	var cmd cobra.Command
	InitListFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("scheduler") == nil {
		t.Errorf("Could not find the flag scheduler")
	}
	if cmd.Flag("all-namespaces") == nil {
		t.Errorf("Could not find the flag all-namespaces")
	}
	if cmd.Flag("selector") == nil {
		t.Errorf("Could not find the flag selector")
	}
	if cmd.Flag("queue") == nil {
		t.Errorf("Could not find the flag queue")
	}
}
