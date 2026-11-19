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

package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	vcv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestSyncTask_TOCTOU_UIDGuard verifies the cache-level TOCTOU regression fix
// introduced in PR #6024. When a pod resync (syncTask) reads a stale pod (U1)
// from the errTasks queue but the API server returns a newer pod (U2) with the
// same namespace/name but a different UID, the UID guard in NodeInfo.RemoveTask
// must prevent the stale U1 task from removing the current U2 task from the
// node's task map.
func TestSyncTask_TOCTOU_UIDGuard(t *testing.T) {
	sc := newMockSchedulerCache("volcano")

	// Node with 2000m CPU, 2Gi memory
	nodeAlloc := api.BuildResourceList("2000m", "2Gi")
	if err := sc.AddOrUpdateNode(buildNode("n1", nodeAlloc)); err != nil {
		t.Fatal(err)
	}

	// U1 pod: same namespace/name as U2, but different UID
	const (
		testNS   = "test"
		testName = "p1"
		testNode = "n1"
		uid1     = "uid-u1"
		uid2     = "uid-u2"
	)

	u1Pod := buildPod(testNS, testName, testNode, v1.PodRunning,
		api.BuildResourceList("1000m", "1Gi"),
		[]metav1.OwnerReference{buildOwnerReference("j1")}, nil)
	u1Pod.UID = types.UID(uid1) // CRITICAL: override buildPod's UID
	u1Pod.Annotations = map[string]string{
		vcv1beta1.KubeGroupNameAnnotationKey: "my-job",
	}

	u2Pod := buildPod(testNS, testName, testNode, v1.PodRunning,
		api.BuildResourceList("1000m", "1Gi"),
		[]metav1.OwnerReference{buildOwnerReference("j1")}, nil)
	u2Pod.UID = types.UID(uid2) // CRITICAL: override buildPod's UID
	u2Pod.Annotations = map[string]string{
		vcv1beta1.KubeGroupNameAnnotationKey: "my-job",
	}

	// Add U1 to cache
	sc.AddPod(u1Pod)

	// Pre-transition state capture
	jobID := api.JobID("test/my-job")
	u1Task := sc.Jobs[jobID].Tasks[api.TaskID(uid1)]
	assert.NotNil(t, u1Task, "U1 must be in the cache")
	expectedUsed := api.NewResource(api.BuildResourceList("1000m", "1Gi", api.ScalarResource{Name: string(v1.ResourcePods), Value: "1"}))

	// Resync Trigger (U1 enqueued)
	sc.resyncTask(u1Task)
	errTaskKey := sc.generateErrTaskKey(u1Task)
	assert.Equal(t, 1, sc.errTasks.Len(), "U1 must be enqueued")

	// Get Barrier (deterministic, channel-based)
	getStarted := make(chan struct{})
	releaseGet := make(chan struct{})

	fakeClient := sc.kubeClient.(*kubefake.Clientset)
	fakeClient.PrependReactor("get", "pods",
		func(action kubetesting.Action) (bool, runtime.Object, error) {
			close(getStarted)
			<-releaseGet
			return true, u2Pod, nil // returns U2 pod -> success path
		})

	// processResyncTask Execution
	done := make(chan struct{})
	go func() {
		sc.processResyncTask()
		close(done)
	}()

	select {
	case <-getStarted:
		// syncTask is blocked at Get (event_handlers.go:309), Mutex NOT held
	case <-time.After(5 * time.Second):
		t.Fatal("syncTask never reached the API Get call")
	}

	// Real U1->U2 Cache Transition (lock held by test, lock-free window)
	sc.Mutex.Lock()
	err := sc.updatePod(u1Pod, u2Pod)
	assert.NoError(t, err)

	taskAfterTransition := sc.Nodes[testNode].Tasks[api.PodKey(u2Pod)]
	assert.NotNil(t, taskAfterTransition, "U2 must be in node after transition")
	assert.Equal(t, api.TaskID(uid2), taskAfterTransition.UID, "U2 must be current after transition")
	sc.Mutex.Unlock()

	// Resume Get
	close(releaseGet)

	// Wait for syncTask
	select {
	case <-done:
		// processResyncTask completed
	case <-time.After(5 * time.Second):
		t.Fatal("processResyncTask did not complete")
	}

	// Assertions (7 required properties)
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	n := sc.Nodes[testNode]
	key := api.PodKey(u2Pod) // "test/p1" -- shared by U1 and U2

	taskAfterSyncTask := n.Tasks[key]

	// Property 1+2: U1 stale, U2 current (verified above before releaseGet)

	// Property 3+4: U1 reached RemoveTask; UID guard prevented U2 removal
	// (pointer identity: with guard, RemoveTask skips, AddTask fails -> same clone pointer)
	assert.Same(t, taskAfterTransition, taskAfterSyncTask,
		"node task pointer must be unchanged: UID guard prevented RemoveTask "+
			"from deleting U2, and AddTask double-add error confirms no re-addition")

	// Property 5: U2 intact
	assert.NotNil(t, taskAfterSyncTask, "U2 must survive syncTask")
	assert.Equal(t, api.TaskID(uid2), taskAfterSyncTask.UID,
		"U2's UID must be intact")
	u2JobTask, found := sc.Jobs[jobID].Tasks[api.TaskID(uid2)]
	assert.True(t, found, "U2 must still be in job")
	assert.NotNil(t, u2JobTask)
	_, u1InJob := sc.Jobs[jobID].Tasks[api.TaskID(uid1)]
	assert.False(t, u1InJob, "U1 must not be re-added to job")

	// Property 6: Resource accounting intact
	assert.True(t, n.Used.Equal(expectedUsed, api.Zero),
		"node Used must equal U2's request (not zeroed, not doubled)")
	assert.True(t, sc.Jobs[jobID].TotalRequest.Equal(expectedUsed, api.Zero),
		"job TotalRequest must equal U2's request only")

	// Property 5 (secondary): syncTask returned error (double-add -> retryResyncTask)
	assert.Greater(t, sc.errTasks.NumRequeues(errTaskKey), 0,
		"syncTask must error (double-add from U2 already present) "+
			"because UID guard prevented U2 removal")
}
