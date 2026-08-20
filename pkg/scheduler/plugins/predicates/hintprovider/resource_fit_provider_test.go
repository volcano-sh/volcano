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

package hintprovider

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestResourceFitPodHint(t *testing.T) {
	targetPod := podWithCPU("target", "2")
	task := api.NewTaskInfo(targetPod)
	job := api.NewJobInfo("job", task)
	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}}

	tests := []struct {
		name     string
		oldPod   *v1.Pod
		newPod   *v1.Pod
		expected api.HintResult
	}{
		{
			name:     "scheduled pod deletion frees resources",
			oldPod:   scheduledPodWithCPU("deleted", "1"),
			expected: api.HintWakeup,
		},
		{
			name:     "unscheduled pod deletion does not free node resources",
			oldPod:   podWithCPU("deleted", "1"),
			expected: api.HintSkip,
		},
		{
			name:     "rejected pending pod deletion changes the job",
			oldPod:   podWithCPU("target", "2"),
			expected: api.HintWakeup,
		},
		{
			name:     "rejected pending pod request decreases",
			oldPod:   podWithCPU("target", "2"),
			newPod:   podWithCPU("target", "1"),
			expected: api.HintWakeup,
		},
		{
			name:     "scheduled pod request decreases",
			oldPod:   scheduledPodWithCPU("resized", "2"),
			newPod:   scheduledPodWithCPU("resized", "1"),
			expected: api.HintWakeup,
		},
		{
			name:     "scheduled pod update does not change resources",
			oldPod:   scheduledPodWithCPU("updated", "1"),
			newPod:   scheduledPodWithCPU("updated", "1"),
			expected: api.HintSkip,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var newObj any
			if test.newPod != nil {
				newObj = test.newPod
			}
			result, err := resourceFitPodHint(job, rejection, test.oldPod, newObj)
			if err != nil {
				t.Fatalf("resourceFitPodHint() error = %v", err)
			}
			if result != test.expected {
				t.Fatalf("resourceFitPodHint() = %v, want %v", result, test.expected)
			}
		})
	}
}

func TestResourceFitNodeHint(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)
	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}}

	oldNode := nodeWithResources("1", "1Gi")
	tests := []struct {
		name   string
		oldObj any
		newObj any
		want   api.HintResult
	}{
		{
			name:   "CPU increase makes rejected task fit",
			oldObj: oldNode,
			newObj: nodeWithResources("2", "1Gi"),
			want:   api.HintWakeup,
		},
		{
			name:   "CPU increase remains insufficient",
			oldObj: oldNode,
			newObj: nodeWithResources("1500m", "1Gi"),
			want:   api.HintSkip,
		},
		{
			name:   "unrequested resource increase is skipped",
			oldObj: oldNode,
			newObj: nodeWithResources("1", "2Gi"),
			want:   api.HintSkip,
		},
		{
			name:   "new Node cannot fit rejected task",
			newObj: oldNode,
			want:   api.HintSkip,
		},
		{
			name:   "new Node can fit rejected task",
			newObj: nodeWithResources("2", "1Gi"),
			want:   api.HintWakeup,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resourceFitNodeHint(job, rejection, test.oldObj, test.newObj)
			if err != nil {
				t.Fatalf("resourceFitNodeHint() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("resourceFitNodeHint() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestResourceFitHintsWakeWhenRejectedTaskIsMissing(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "3"))
	job := api.NewJobInfo("job", task)
	oneCPU := api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")})
	twoCPUs := api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")})
	hints := []struct {
		name string
		hint func(api.Rejection) api.HintResult
	}{
		{
			name: "new Node",
			hint: func(rejection api.Rejection) api.HintResult {
				return newNodeFitHint(job, rejection, twoCPUs)
			},
		},
		{
			name: "Pod resource release",
			hint: func(rejection api.Rejection) api.HintResult {
				return resourceReleaseHint(job, rejection, twoCPUs, oneCPU)
			},
		},
		{
			name: "Node allocatable increase",
			hint: func(rejection api.Rejection) api.HintResult {
				return allocatableIncreaseHint(job, rejection, oneCPU, twoCPUs)
			},
		},
	}

	rejections := []struct {
		name  string
		tasks []api.TaskID
	}{
		{name: "missing task first", tasks: []api.TaskID{"missing-task", task.UID}},
		{name: "missing task last", tasks: []api.TaskID{task.UID, "missing-task"}},
	}
	for _, hint := range hints {
		for _, rejection := range rejections {
			t.Run(hint.name+"/"+rejection.name, func(t *testing.T) {
				got := hint.hint(api.Rejection{Tasks: rejection.tasks})
				if got != api.HintWakeup {
					t.Fatalf("hint result = %v, want %v for missing rejected task", got, api.HintWakeup)
				}
			})
		}
	}
}

func podWithCPU(name, cpu string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("uid-" + name)},
		Spec: v1.PodSpec{Containers: []v1.Container{{
			Name: "container",
			Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse(cpu),
			}},
		}}},
	}
}

func scheduledPodWithCPU(name, cpu string) *v1.Pod {
	pod := podWithCPU(name, cpu)
	pod.Spec.NodeName = "node"
	return pod
}

func nodeWithResources(cpu, memory string) *v1.Node {
	return &v1.Node{Status: v1.NodeStatus{Allocatable: v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourcePods:   resource.MustParse("100"),
	}}}
}
