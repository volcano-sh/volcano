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

package tasktopology

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// newTestTask builds a TaskInfo with the given UID, task-spec annotation, and
// (optional) node binding. The container requests 1 CPU / 1Gi memory.
func newTestTask(uid, taskSpec, nodeName string) *api.TaskInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:         types.UID(uid),
			Name:        "pod-" + uid,
			Namespace:   "default",
			Annotations: map[string]string{v1alpha1.TaskSpecKey: taskSpec},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "c",
				Image: "busybox",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			}},
		},
	}
	t := api.NewTaskInfo(pod)
	t.NodeName = nodeName
	t.Job = api.JobID("job-test")
	return t
}

// snapshot of bucket state we care about for round-trip equality.
type bucketState struct {
	tasks       map[types.UID]*api.TaskInfo
	taskNameSet map[string]int
	reqScore    float64
	requestCPU  float64
	requestMem  float64
	boundTask   int
	node        map[string]int
}

func snapshotBucket(b *Bucket) bucketState {
	tasks := make(map[types.UID]*api.TaskInfo, len(b.tasks))
	for k, v := range b.tasks {
		tasks[k] = v
	}
	tns := make(map[string]int, len(b.taskNameSet))
	for k, v := range b.taskNameSet {
		tns[k] = v
	}
	node := make(map[string]int, len(b.node))
	for k, v := range b.node {
		node[k] = v
	}
	return bucketState{
		tasks:       tasks,
		taskNameSet: tns,
		reqScore:    b.reqScore,
		requestCPU:  b.request.MilliCPU,
		requestMem:  b.request.Memory,
		boundTask:   b.boundTask,
		node:        node,
	}
}

// TestBucket_TaskUnbound_IsInverseOfTaskBound verifies that calling
// TaskUnbound after TaskBound restores the bucket to its pre-bind state.
//
// Without this, a Statement.Discard() leaves the bucket with phantom
// boundTask++ / node[N]++ counts and a missing entry in tasks, so
// subsequent NodeOrderFn scores in the same session compute against
// stale state. See volcano-sh/volcano#4003.
func TestBucket_TaskUnbound_IsInverseOfTaskBound(t *testing.T) {
	b := NewBucket()
	task := newTestTask("u1", "ps", "")

	// Stage: task added to bucket as pending (no NodeName yet).
	b.AddTask("ps", task)

	before := snapshotBucket(b)

	// Bind: scheduler allocates the task to node-a.
	task.NodeName = "node-a"
	b.TaskBound(task)

	// Sanity-check that TaskBound mutated state.
	if b.boundTask != before.boundTask+1 {
		t.Fatalf("after TaskBound: boundTask=%d, want %d", b.boundTask, before.boundTask+1)
	}
	if b.node["node-a"] != 1 {
		t.Fatalf("after TaskBound: node[node-a]=%d, want 1", b.node["node-a"])
	}
	if _, ok := b.tasks[task.Pod.UID]; ok {
		t.Fatal("after TaskBound: tasks still contains uid; want delete")
	}

	// Discard: scheduler rolls back the allocation.
	b.TaskUnbound(task)

	after := snapshotBucket(b)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("TaskUnbound did not restore bucket state\n  before=%#v\n  after=%#v", before, after)
	}
}

// TestJobManager_TaskUnbound_IsInverseOfTaskBound verifies the same
// invariant at the JobManager level — including the nodeTaskSet
// bookkeeping that drives inter-task affinity scoring.
func TestJobManager_TaskUnbound_IsInverseOfTaskBound(t *testing.T) {
	jm := NewJobManager(api.JobID("job-test"))
	bucket := jm.NewBucket()

	task := newTestTask("u1", "ps", "")
	jm.AddTaskToBucket(bucket.index, "ps", task)

	beforeBucket := snapshotBucket(bucket)
	beforeNodeTaskSet := copyNodeTaskSet(jm.nodeTaskSet)

	// Bind
	task.NodeName = "node-a"
	jm.TaskBound(task)
	if jm.nodeTaskSet["node-a"]["ps"] != 1 {
		t.Fatalf("after TaskBound: nodeTaskSet[node-a][ps]=%d, want 1",
			jm.nodeTaskSet["node-a"]["ps"])
	}

	// Discard
	jm.TaskUnbound(task)

	afterBucket := snapshotBucket(bucket)
	afterNodeTaskSet := copyNodeTaskSet(jm.nodeTaskSet)

	if !reflect.DeepEqual(beforeBucket, afterBucket) {
		t.Fatalf("TaskUnbound did not restore bucket state\n  before=%#v\n  after=%#v",
			beforeBucket, afterBucket)
	}
	if !reflect.DeepEqual(beforeNodeTaskSet, afterNodeTaskSet) {
		t.Fatalf("TaskUnbound did not clean up nodeTaskSet\n  before=%#v\n  after=%#v",
			beforeNodeTaskSet, afterNodeTaskSet)
	}
}

func copyNodeTaskSet(m map[string]map[string]int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(m))
	for k, inner := range m {
		copied := make(map[string]int, len(inner))
		for ik, iv := range inner {
			copied[ik] = iv
		}
		out[k] = copied
	}
	return out
}
