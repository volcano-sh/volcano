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

package framework

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// listerTask is one pod in a synthetic session. taskNode and podNode are
// independent so a case can describe a task whose scheduled node differs
// from the pod spec.
type listerTask struct {
	name     string
	job      string
	status   api.TaskStatus
	taskNode string
	podNode  string
	onNode   string
	affinity bool
	label    string
}

// listerStep is one allocate, evict, or UpdateTask operation. Field changes
// are applied before UpdateTask, matching Statement: the event handler runs
// while the task still has its previous node name.
type listerStep struct {
	name       string
	task       string
	status     *api.TaskStatus
	taskNode   *string
	podNode    *string
	updateNode string
	doUpdate   bool
}

type listerCase struct {
	name  string
	tasks []listerTask
	steps []listerStep
}

func TestPodListerGetPodListMatchesEagerCopy(t *testing.T) {
	keep := labels.SelectorFromSet(labels.Set{"app": "keep"})
	constructors := []struct {
		name  string
		build func(*Session) *PodLister
		eager func(*Session) *PodLister
	}{
		{name: "NewPodLister", build: NewPodLister, eager: newPodListerEagerCopy},
		{name: "NewPodListerFromNode", build: NewPodListerFromNode, eager: newPodListerFromNodeEagerCopy},
	}

	cases := []listerCase{
		{
			name: "empty",
			steps: []listerStep{
				{name: "initial"},
			},
		},
		{
			name:  "task-states",
			tasks: podListerStateTasks(),
			steps: []listerStep{
				{name: "initial"},
			},
		},
		{
			name: "allocate-evict-update",
			tasks: []listerTask{
				{name: "pending", status: api.Pending, label: "keep"},
				{name: "running", status: api.Running, taskNode: "n1", podNode: "n1", onNode: "n1", affinity: true, label: "keep"},
				{name: "allocated-unbound", status: api.Allocated, taskNode: "n2", onNode: "n2", label: "other"},
				{name: "releasing", status: api.Releasing, taskNode: "n1", podNode: "n1", onNode: "n1", affinity: true, label: "keep"},
			},
			steps: []listerStep{
				{name: "initial"},
				// Statement.Allocate writes the pod spec, status, and task node, then UpdateTask.
				{name: "allocate-pending", task: "pending", status: statusPtr(api.Allocated), taskNode: strPtr("n2"), podNode: strPtr("n2"), updateNode: "n2", doUpdate: true},
				// Evict marks the victim Releasing and UpdateTask clears the cached node. The task node stays.
				{name: "evict-running", task: "running", status: statusPtr(api.Releasing), updateNode: "", doUpdate: true},
				// Unevict restores Running and UpdateTask puts the original node back.
				{name: "unevict-running", task: "running", status: statusPtr(api.Running), updateNode: "n1", doUpdate: true},
				// Unallocate calls UpdateTask before it clears the task node.
				{name: "unallocate-pending", task: "pending", status: statusPtr(api.Pending), updateNode: "", doUpdate: true},
				{name: "clear-node-after-unallocate", task: "pending", taskNode: strPtr("")},
				// An allocated task whose pod spec was empty is moved with UpdateTask.
				{name: "move-allocated-unbound", task: "allocated-unbound", taskNode: strPtr("n4"), updateNode: "n4", doUpdate: true},
				{name: "evict-allocated-unbound", task: "allocated-unbound", status: statusPtr(api.Releasing), updateNode: "", doUpdate: true},
				// A non-allocated task is copied by UpdateTask and stays out of Tasks.
				{name: "place-releasing", task: "releasing", status: statusPtr(api.Pipelined), taskNode: strPtr("n3"), updateNode: "n3", doUpdate: true},
			},
		},
	}

	for _, tc := range cases {
		for _, ctor := range constructors {
			t.Run(tc.name+"/"+ctor.name, func(t *testing.T) {
				ssn, tasks := buildPodListerSession(tc.tasks)
				eager := ctor.eager(ssn)
				got := ctor.build(ssn)
				all := taskSlice(tasks)

				for i, step := range tc.steps {
					t.Run(step.name, func(t *testing.T) {
						if step.task != "" {
							task, ok := tasks[step.task]
							if !ok {
								t.Fatalf("unknown task %q", step.task)
							}
							if step.status != nil {
								task.Status = *step.status
							}
							if step.taskNode != nil {
								task.NodeName = *step.taskNode
							}
							if step.podNode != nil {
								task.Pod.Spec.NodeName = *step.podNode
							}
						}

						var eagerUpdated, gotUpdated string
						if step.doUpdate {
							task := tasks[step.task]
							eagerUpdated = eager.UpdateTask(task, step.updateNode).Spec.NodeName
							gotUpdated = got.UpdateTask(task, step.updateNode).Spec.NodeName
							if eagerUpdated != gotUpdated {
								t.Errorf("UpdateTask node = %q, eager copy = %q", gotUpdated, eagerUpdated)
							}
						}

						comparePodNodes(t, "GetPod", podNodesByTask(got, all), podNodesByTask(eager, all))
						comparePodNodes(t, "List", podNodesFromList(t, got, labels.Everything()), podNodesFromList(t, eager, labels.Everything()))
						comparePodNodes(t, "List app=keep", podNodesFromList(t, got, keep), podNodesFromList(t, eager, keep))
						compareTaskSet(t, "Tasks", got.Tasks, eager.Tasks)
						compareTaskSet(t, "TaskWithAffinity", got.TaskWithAffinity, eager.TaskWithAffinity)
						assertGetPodReturnsOriginalWhenNodeMatches(t, got, all)
						assertNoUnexpectedGetPodCopy(t, got)

						if i == 0 {
							assertCachedOnlyWhenNodeDiffers(t, got, all, ctor.name)
						}
					})
				}
			})
		}
	}
}

func podListerStateTasks() []listerTask {
	return []listerTask{
		{name: "pending", job: "job-a", status: api.Pending, label: "keep"},
		{name: "pending-nominated", job: "job-a", status: api.Pending, taskNode: "n1", label: "keep"},
		{name: "allocated", job: "job-a", status: api.Allocated, taskNode: "n1", podNode: "n1", onNode: "n1", label: "keep"},
		{name: "allocated-unbound", job: "job-a", status: api.Allocated, taskNode: "n2", onNode: "n2", label: "other"},
		{name: "binding", job: "job-a", status: api.Binding, taskNode: "n1", podNode: "n1", onNode: "n1", label: "keep"},
		{name: "binding-unbound", job: "job-a", status: api.Binding, taskNode: "n2", podNode: "n1", onNode: "n2", label: "other"},
		{name: "bound", job: "job-b", status: api.Bound, taskNode: "n1", podNode: "n1", onNode: "n1", label: "keep"},
		{name: "running", job: "job-b", status: api.Running, taskNode: "n1", podNode: "n1", onNode: "n1", affinity: true, label: "keep"},
		{name: "releasing", job: "job-b", status: api.Releasing, taskNode: "n1", podNode: "n1", onNode: "n1", label: "keep"},
		{name: "releasing-moved", job: "job-b", status: api.Releasing, taskNode: "n2", podNode: "n1", onNode: "n1", affinity: true, label: "other"},
		{name: "pipelined", job: "job-b", status: api.Pipelined, taskNode: "n1", onNode: "n1", label: "keep"},
		{name: "succeeded", job: "job-b", status: api.Succeeded, taskNode: "n1", podNode: "n1", onNode: "n1", label: "keep"},
		{name: "failed", job: "job-b", status: api.Failed, label: "other"},
	}
}

func buildPodListerSession(specs []listerTask) (*Session, map[string]*api.TaskInfo) {
	jobs := map[api.JobID]*api.JobInfo{}
	nodes := map[string]*api.NodeInfo{}
	tasks := make(map[string]*api.TaskInfo, len(specs))

	for _, spec := range specs {
		jobName := spec.job
		if jobName == "" {
			jobName = "job-a"
		}
		jobID := api.JobID("ns/" + jobName)
		job, found := jobs[jobID]
		if !found {
			job = api.NewJobInfo(jobID)
			job.Name = jobName
			job.Namespace = "ns"
			jobs[jobID] = job
		}

		label := spec.label
		if label == "" {
			label = "keep"
		}
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spec.name,
				Namespace: "ns",
				UID:       types.UID(spec.name),
				Labels:    map[string]string{"app": label},
			},
			Spec: v1.PodSpec{
				NodeName:   spec.podNode,
				Containers: []v1.Container{{Name: "c"}},
			},
		}
		if spec.affinity {
			pod.Spec.Affinity = &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{{
							MatchExpressions: []v1.NodeSelectorRequirement{{
								Key:      "disk",
								Operator: v1.NodeSelectorOpExists,
							}},
						}},
					},
				},
			}
		}

		task := api.NewTaskInfo(pod)
		task.Status = spec.status
		task.NodeName = spec.taskNode
		task.Job = jobID
		job.AddTaskInfo(task)
		tasks[spec.name] = task

		if spec.onNode == "" {
			continue
		}
		node, found := nodes[spec.onNode]
		if !found {
			node = api.NewNodeInfo(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: spec.onNode}})
			nodes[spec.onNode] = node
		}
		node.Tasks[task.UID] = task
	}

	return &Session{Jobs: jobs, Nodes: nodes}, tasks
}

func taskSlice(tasks map[string]*api.TaskInfo) []*api.TaskInfo {
	out := make([]*api.TaskInfo, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task)
	}
	return out
}

func podNodesByTask(pl *PodLister, tasks []*api.TaskInfo) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, task := range tasks {
		pod := pl.GetPod(task)
		out[task.Namespace+"/"+task.Name] = pod.Spec.NodeName
	}
	return out
}

func podNodesFromList(t *testing.T, pl *PodLister, selector labels.Selector) map[string]string {
	t.Helper()
	pods, err := pl.List(selector)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	out := make(map[string]string, len(pods))
	for _, pod := range pods {
		key := pod.Namespace + "/" + pod.Name
		if _, dup := out[key]; dup {
			t.Fatalf("List returned duplicate pod %s", key)
		}
		out[key] = pod.Spec.NodeName
	}
	return out
}

func comparePodNodes(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s pod node names mismatch (-eager +got):\n%s", what, diff)
	}
}

func compareTaskSet(t *testing.T, what string, got, want map[api.TaskID]*api.TaskInfo) {
	t.Helper()
	if diff := cmp.Diff(taskNameSet(want), taskNameSet(got)); diff != "" {
		t.Errorf("%s mismatch (-eager +got):\n%s", what, diff)
	}
}

func taskNameSet(tasks map[api.TaskID]*api.TaskInfo) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, task := range tasks {
		out[task.Namespace+"/"+task.Name] = task.Status.String()
	}
	return out
}

func assertGetPodReturnsOriginalWhenNodeMatches(t *testing.T, pl *PodLister, tasks []*api.TaskInfo) {
	t.Helper()
	for _, task := range tasks {
		if task.NodeName != task.Pod.Spec.NodeName {
			continue
		}
		if got := pl.GetPod(task); got != task.Pod {
			t.Errorf("GetPod(%s) = copied pod, want the original pod when node names match", task.Name)
		}
	}
}

// assertNoUnexpectedGetPodCopy locks the condition that logs
// "DeepCopy for pod ... at PodLister.GetPod is unexpected": a task indexed
// by the lister whose node names differ must already be in CachedPods.
func assertNoUnexpectedGetPodCopy(t *testing.T, pl *PodLister) {
	t.Helper()
	for _, task := range pl.Tasks {
		if task.NodeName == task.Pod.Spec.NodeName {
			continue
		}
		if _, found := pl.CachedPods[task.UID]; !found {
			t.Errorf("task %s is listed with a different node and is not cached; GetPod would deep-copy", task.Name)
		}
	}
}

// assertCachedOnlyWhenNodeDiffers checks the initial cache. Tasks the
// constructor indexes are copied only when the scheduled node differs from
// the pod spec. UpdateTask may cache more pods later, so this is only
// checked before any step mutates the lister.
func assertCachedOnlyWhenNodeDiffers(t *testing.T, pl *PodLister, tasks []*api.TaskInfo, ctor string) {
	t.Helper()
	indexed := make(map[api.TaskID]bool, len(pl.Tasks))
	for id := range pl.Tasks {
		indexed[id] = true
	}
	for _, task := range tasks {
		_, cached := pl.CachedPods[task.UID]
		differs := task.NodeName != task.Pod.Spec.NodeName
		switch {
		case !indexed[task.UID] && cached:
			t.Errorf("%s cached %s, which is not in Tasks", ctor, task.Name)
		case indexed[task.UID] && differs && !cached:
			t.Errorf("%s did not cache %s, whose node names differ", ctor, task.Name)
		case !differs && cached:
			t.Errorf("%s cached %s even though node names match", ctor, task.Name)
		}
	}
}

func statusPtr(status api.TaskStatus) *api.TaskStatus {
	return &status
}

func strPtr(v string) *string {
	return &v
}

// newPodListerEagerCopy reproduces NewPodLister before CachedPods were
// limited to tasks whose node name differs from the pod spec.
func newPodListerEagerCopy(ssn *Session) *PodLister {
	pl := &PodLister{
		Session:          ssn,
		CachedPods:       make(map[api.TaskID]*v1.Pod),
		Tasks:            make(map[api.TaskID]*api.TaskInfo),
		TaskWithAffinity: make(map[api.TaskID]*api.TaskInfo),
	}

	for _, job := range pl.Session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}
			for _, task := range tasks {
				pl.Tasks[task.UID] = task
				pl.CachedPods[task.UID] = pl.copyTaskPod(task)
				if HaveAffinity(task.Pod) {
					pl.TaskWithAffinity[task.UID] = task
				}
			}
		}
	}

	return pl
}

// newPodListerFromNodeEagerCopy reproduces NewPodListerFromNode before the
// same CachedPods change. Releasing tasks on a node are still indexed.
func newPodListerFromNodeEagerCopy(ssn *Session) *PodLister {
	pl := &PodLister{
		Session:          ssn,
		CachedPods:       make(map[api.TaskID]*v1.Pod),
		Tasks:            make(map[api.TaskID]*api.TaskInfo),
		TaskWithAffinity: make(map[api.TaskID]*api.TaskInfo),
	}

	for _, node := range pl.Session.Nodes {
		for _, task := range node.Tasks {
			if !api.AllocatedStatus(task.Status) && task.Status != api.Releasing {
				continue
			}
			pl.Tasks[task.UID] = task
			pl.CachedPods[task.UID] = pl.copyTaskPod(task)
			if HaveAffinity(task.Pod) {
				pl.TaskWithAffinity[task.UID] = task
			}
		}
	}

	return pl
}

func BenchmarkNewPodLister(b *testing.B) {
	const n = 10000
	job := api.NewJobInfo("ns/bench")
	job.Name = "bench"
	job.Namespace = "ns"
	for i := 0; i < n; i++ {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-" + strconv.Itoa(i),
				Namespace: "ns",
				UID:       types.UID(fmt.Sprintf("uid-%d", i)),
				Labels:    map[string]string{"app": "bench"},
				Annotations: map[string]string{
					"volcano.sh/task-spec": "worker",
				},
			},
			Spec: v1.PodSpec{
				NodeName: "node-0",
				Containers: []v1.Container{{
					Name: "c",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("100m"),
							v1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				}},
			},
			Status: v1.PodStatus{Phase: v1.PodRunning},
		}
		task := api.NewTaskInfo(pod)
		task.Job = job.UID
		job.AddTaskInfo(task)
	}
	ssn := &Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job}}

	b.ReportAllocs()
	b.ResetTimer()
	var listed int
	for i := 0; i < b.N; i++ {
		pl := NewPodLister(ssn)
		listed += len(pl.Tasks)
	}
	if listed != n*b.N {
		b.Fatalf("NewPodLister listed %d tasks, want %d", listed, n*b.N)
	}
}
