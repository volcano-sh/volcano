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

package tensorflow

import (
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestTensorflow(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "train-123"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "ps",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "chief",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}
	testcases := []struct {
		Name string
		Job  *batch.Job
		Pod  *v1.Pod
	}{
		{
			Name: "ps case",
			Job:  testjob,
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "train-123-ps-0",
					Annotations: map[string]string{
						batch.TaskSpecKey: "ps",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "main",
						},
					},
				},
			},
		},
		{
			Name: "worker case",
			Job:  testjob,
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "train-123-worker-0",
					Annotations: map[string]string{
						batch.TaskSpecKey: "worker",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "main",
						},
					},
				},
			},
		},
		{
			Name: "chief case",
			Job:  testjob,
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "train-123-chief-0",
					Annotations: map[string]string{
						batch.TaskSpecKey: "chief",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "main",
						},
					},
				},
			},
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			tp := New(pluginsinterface.PluginClientset{}, []string{"--port=5000"})
			if err := tp.OnPodCreate(testcase.Pod, testcase.Job); err != nil {
				t.Errorf("Case %d (%s): expect no error, but got error %v", i, testcase.Name, err)
			}
			if testcase.Pod.Spec.Containers[0].Env[0].Name != "TF_CONFIG" {
				t.Errorf("Case %d (%s): wrong env name, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Env[0].Name)
			}

			switch {
			case strings.Contains(testcase.Pod.Name, "ps"):
				if testcase.Pod.Spec.Containers[0].Env[0].Value != `{"cluster":{"ps":["train-123-ps-0.train-123:5000","train-123-ps-1.train-123:5000"],"worker":["train-123-worker-0.train-123:5000","train-123-worker-1.train-123:5000"],"chief":["train-123-chief-0.train-123:5000"]},"task":{"type":"ps","index":0}}` {
					t.Errorf("Case %d (%s): wrong env value, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Env[0].Value)
				}
			case strings.Contains(testcase.Pod.Name, "worker"):
				if testcase.Pod.Spec.Containers[0].Env[0].Value != `{"cluster":{"ps":["train-123-ps-0.train-123:5000","train-123-ps-1.train-123:5000"],"worker":["train-123-worker-0.train-123:5000","train-123-worker-1.train-123:5000"],"chief":["train-123-chief-0.train-123:5000"]},"task":{"type":"worker","index":0}}` {
					t.Errorf("Case %d (%s): wrong env value, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Env[0].Value)
				}
			case strings.Contains(testcase.Pod.Name, "chief"):
				if testcase.Pod.Spec.Containers[0].Env[0].Value != `{"cluster":{"ps":["train-123-ps-0.train-123:5000","train-123-ps-1.train-123:5000"],"worker":["train-123-worker-0.train-123:5000","train-123-worker-1.train-123:5000"],"chief":["train-123-chief-0.train-123:5000"]},"task":{"type":"chief","index":0}}` {
					t.Errorf("Case %d (%s): wrong env value, got %s", i, testcase.Name, testcase.Pod.Spec.Containers[0].Env[0].Value)
				}
			}
		})
	}
}

func TestTensorflowMultipleWorkers(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-multi"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "ps",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker-0",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}

	testcases := []struct {
		Name     string
		PodName  string
		TaskSpec string
		Expected string
	}{
		{
			Name:     "second worker task pod keeps global worker index",
			PodName:  "tf-multi-worker-0-1",
			TaskSpec: "worker-0",
			Expected: `{"cluster":{"ps":["tf-multi-ps-0.tf-multi:2222"],"worker":["tf-multi-worker-0.tf-multi:2222","tf-multi-worker-1.tf-multi:2222","tf-multi-worker-0-0.tf-multi:2222","tf-multi-worker-0-1.tf-multi:2222"]},"task":{"type":"worker","index":3}}`,
		},
		{
			Name:     "first worker task pod index is unchanged",
			PodName:  "tf-multi-worker-0",
			TaskSpec: "worker",
			Expected: `{"cluster":{"ps":["tf-multi-ps-0.tf-multi:2222"],"worker":["tf-multi-worker-0.tf-multi:2222","tf-multi-worker-1.tf-multi:2222","tf-multi-worker-0-0.tf-multi:2222","tf-multi-worker-0-1.tf-multi:2222"]},"task":{"type":"worker","index":0}}`,
		},
		{
			Name:     "ps pod is unaffected",
			PodName:  "tf-multi-ps-0",
			TaskSpec: "ps",
			Expected: `{"cluster":{"ps":["tf-multi-ps-0.tf-multi:2222"],"worker":["tf-multi-worker-0.tf-multi:2222","tf-multi-worker-1.tf-multi:2222","tf-multi-worker-0-0.tf-multi:2222","tf-multi-worker-0-1.tf-multi:2222"]},"task":{"type":"ps","index":0}}`,
		},
	}

	for index, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testcase.PodName,
					Annotations: map[string]string{batch.TaskSpecKey: testcase.TaskSpec},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "main"}},
				},
			}
			tp := New(pluginsinterface.PluginClientset{}, []string{"--worker=worker,worker-0"})
			if err := tp.OnPodCreate(pod, testjob); err != nil {
				t.Fatalf("Case %d (%s): expect no error, but got error %v", index, testcase.Name, err)
			}
			if value := pod.Spec.Containers[0].Env[0].Value; value != testcase.Expected {
				t.Errorf("Case %d (%s): wrong env value, got %s", index, testcase.Name, value)
			}
		})
	}
}

// TestTensorflowSingleWorkerNameCompatibility verifies that a single custom
// worker task name keeps exactly the same TF_CONFIG as before multi-worker.
func TestTensorflowSingleWorkerNameCompatibility(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-compat"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "ps",
					Replicas: 1,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "mlworker",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tf-compat-mlworker-1",
			Annotations: map[string]string{batch.TaskSpecKey: "mlworker"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "main"}},
		},
	}

	tp := New(pluginsinterface.PluginClientset{}, []string{"--worker=mlworker"})
	if err := tp.OnPodCreate(pod, testjob); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}

	expected := `{"cluster":{"ps":["tf-compat-ps-0.tf-compat:2222"],"worker":["tf-compat-mlworker-0.tf-compat:2222","tf-compat-mlworker-1.tf-compat:2222"]},"task":{"type":"worker","index":1}}`
	if value := pod.Spec.Containers[0].Env[0].Value; value != expected {
		t.Errorf("TF_CONFIG mismatch, expected '%s', got '%s'", expected, value)
	}
}

// TestTensorflowWorkerOrderFollowsWorkerArgument pins that the worker argument
// order defines both the cluster worker host list order and the global worker
// index: worker-0 listed first occupies the first host positions and index
// range, regardless of the task definition order in the job.
func TestTensorflowWorkerOrderFollowsWorkerArgument(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-order"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name:     "worker",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
				{
					Name:     "worker-0",
					Replicas: 2,
					Template: v1.PodTemplateSpec{},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tf-order-worker-0-1",
			Annotations: map[string]string{batch.TaskSpecKey: "worker-0"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "main"}},
		},
	}

	// worker-0 listed first in the argument, although defined second in the job
	tp := New(pluginsinterface.PluginClientset{}, []string{"--worker=worker-0,worker"})
	if err := tp.OnPodCreate(pod, testjob); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}

	expected := `{"cluster":{"worker":["tf-order-worker-0-0.tf-order:2222","tf-order-worker-0-1.tf-order:2222","tf-order-worker-0.tf-order:2222","tf-order-worker-1.tf-order:2222"]},"task":{"type":"worker","index":1}}`
	if value := pod.Spec.Containers[0].Env[0].Value; value != expected {
		t.Errorf("TF_CONFIG mismatch, expected '%s', got '%s'", expected, value)
	}
}

// TestTensorflowWorkersFlag verifies the dedicated multi-worker flag drives
// the same global worker index and host ordering.
func TestTensorflowWorkersFlag(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-workers"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{Name: "ps", Replicas: 1, Template: v1.PodTemplateSpec{}},
				{Name: "worker", Replicas: 2, Template: v1.PodTemplateSpec{}},
				{Name: "worker-0", Replicas: 2, Template: v1.PodTemplateSpec{}},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tf-workers-worker-0-1",
			Annotations: map[string]string{batch.TaskSpecKey: "worker-0"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}

	tp := New(pluginsinterface.PluginClientset{}, []string{"--workers=worker,worker-0"})
	if err := tp.OnPodCreate(pod, testjob); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	expected := `{"cluster":{"ps":["tf-workers-ps-0.tf-workers:2222"],"worker":["tf-workers-worker-0.tf-workers:2222","tf-workers-worker-1.tf-workers:2222","tf-workers-worker-0-0.tf-workers:2222","tf-workers-worker-0-1.tf-workers:2222"]},"task":{"type":"worker","index":3}}`
	if value := pod.Spec.Containers[0].Env[0].Value; value != expected {
		t.Errorf("TF_CONFIG mismatch, expected '%s', got '%s'", expected, value)
	}
}

// TestTensorflowWorkersFlagDedup verifies duplicated worker names produce a
// unique host list and contiguous indices.
func TestTensorflowWorkersFlagDedup(t *testing.T) {
	testjob := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-dedup"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{Name: "a", Replicas: 2, Template: v1.PodTemplateSpec{}},
				{Name: "b", Replicas: 1, Template: v1.PodTemplateSpec{}},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tf-dedup-b-0",
			Annotations: map[string]string{batch.TaskSpecKey: "b"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}

	tp := New(pluginsinterface.PluginClientset{}, []string{"--workers=a, a,b,a"})
	if err := tp.OnPodCreate(pod, testjob); err != nil {
		t.Fatalf("expect no error, but got error %v", err)
	}
	// task a hosts appear once; task b index is 2 (= a's replicas), not 4
	expected := `{"cluster":{"worker":["tf-dedup-a-0.tf-dedup:2222","tf-dedup-a-1.tf-dedup:2222","tf-dedup-b-0.tf-dedup:2222"]},"task":{"type":"worker","index":2}}`
	if value := pod.Spec.Containers[0].Env[0].Value; value != expected {
		t.Errorf("TF_CONFIG mismatch, expected '%s', got '%s'", expected, value)
	}
}
