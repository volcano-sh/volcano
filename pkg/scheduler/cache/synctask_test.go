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

package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func resyncTestPod(gated bool) *v1.Pod {
	pod := buildPod("test", "worker", "", v1.PodPending,
		schedulingapi.BuildResourceList("1", "1Gi"), nil, nil)
	pod.ResourceVersion = "1"
	pod.Annotations = map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: "workers"}
	pod.Spec.SchedulerName = "volcano"
	if gated {
		pod.Spec.SchedulingGates = []v1.PodSchedulingGate{{Name: "kueue.x-k8s.io/admission"}}
	}
	return pod
}

func cachedResyncTask(sc *SchedulerCache, original *schedulingapi.TaskInfo) *schedulingapi.TaskInfo {
	if job := sc.Jobs[original.Job]; job != nil {
		return job.Tasks[original.UID]
	}
	return nil
}

func resyncTestPodGroup() *schedulingv1beta1.PodGroup {
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "workers", Namespace: "test", UID: "workers-group", ResourceVersion: "1",
		},
		Spec: schedulingv1beta1.PodGroupSpec{MinMember: 1},
	}
}

func TestSchedulerCache_SyncTask(t *testing.T) {
	clearGates := func(t *testing.T, sc *SchedulerCache, pod *v1.Pod, _ *schedulingapi.TaskInfo) {
		updated := pod.DeepCopy()
		updated.ResourceVersion = "2"
		updated.Spec.SchedulingGates = nil
		sc.UpdatePod(pod, updated)
	}
	tests := []struct {
		name            string
		initiallyGated  bool
		responseVersion string
		responseNewUID  bool
		getErr          error
		beforeReading   func(*testing.T, *SchedulerCache, *v1.Pod, *schedulingapi.TaskInfo)
		whileReading    func(*testing.T, *SchedulerCache, *v1.Pod, *schedulingapi.TaskInfo)
		skipGET         bool
		wantErr         bool
		wantTask        bool
		wantVersion     string
		wantGated       bool
		wantStatus      schedulingapi.TaskStatus
		wantNode        string
		wantSameTask    bool
	}{
		{
			name:           "task replaced before resync is retried without an API read",
			initiallyGated: true,
			beforeReading:  clearGates,
			skipGET:        true,
			wantErr:        true,
			wantTask:       true,
			wantVersion:    "2",
			wantStatus:     schedulingapi.Pending,
		},
		{
			name:           "task deleted before resync is not fetched again",
			initiallyGated: true,
			beforeReading: func(t *testing.T, sc *SchedulerCache, pod *v1.Pod, _ *schedulingapi.TaskInfo) {
				sc.DeletePod(pod)
			},
			skipGET: true,
		},
		{
			name:           "gate removal while GET is in flight is preserved",
			initiallyGated: true,
			whileReading:   clearGates,
			wantErr:        true,
			wantTask:       true,
			wantVersion:    "2",
			wantStatus:     schedulingapi.Pending,
		},
		{
			name:           "NotFound cannot delete a concurrent informer update",
			initiallyGated: true,
			getErr:         apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "worker"),
			whileReading:   clearGates,
			wantErr:        true,
			wantTask:       true,
			wantVersion:    "2",
			wantStatus:     schedulingapi.Pending,
		},
		{
			name:           "deleted task is not resurrected by an old GET result",
			initiallyGated: true,
			whileReading: func(t *testing.T, sc *SchedulerCache, pod *v1.Pod, _ *schedulingapi.TaskInfo) {
				sc.DeletePod(pod)
			},
		},
		{
			name:           "PodGroup replacement while GET is in flight is preserved",
			initiallyGated: true,
			whileReading: func(t *testing.T, sc *SchedulerCache, _ *v1.Pod, task *schedulingapi.TaskInfo) {
				original := resyncTestPodGroup()
				sc.DeletePodGroupV1beta1(original)
				replacement := original.DeepCopy()
				replacement.UID = "replacement-group"
				replacement.ResourceVersion = "2"
				sc.AddPodGroupV1beta1(replacement)
				require.Same(t, task, cachedResyncTask(sc, task))
				require.Equal(t, replacement.UID, sc.Jobs[task.Job].PgUID)
			},
			wantErr:      true,
			wantTask:     true,
			wantVersion:  "1",
			wantGated:    true,
			wantStatus:   schedulingapi.Pending,
			wantSameTask: true,
		},
		{
			name: "binding changes on the same task pointer are preserved",
			whileReading: func(t *testing.T, sc *SchedulerCache, _ *v1.Pod, task *schedulingapi.TaskInfo) {
				sc.AddOrUpdateNode(buildNode("node", schedulingapi.BuildResourceList("4", "4Gi",
					schedulingapi.ScalarResource{Name: "pods", Value: "10"})))
				binding := task.Clone()
				binding.NodeName = "node"
				require.NoError(t, sc.AddBindTask(&BindContext{TaskInfo: binding}))
				require.Same(t, task, cachedResyncTask(sc, task))
			},
			wantErr:      true,
			wantTask:     true,
			wantVersion:  "1",
			wantStatus:   schedulingapi.Binding,
			wantNode:     "node",
			wantSameTask: true,
		},
		{
			name:           "GET of a replacement UID is not added under the old task key",
			initiallyGated: true,
			responseNewUID: true,
		},
		{
			name:            "unchanged cache accepts a normal GET update",
			initiallyGated:  true,
			responseVersion: "2",
			wantTask:        true,
			wantVersion:     "2",
			wantStatus:      schedulingapi.Pending,
		},
		{
			name:           "NotFound cleans up an unchanged task",
			initiallyGated: true,
			getErr:         apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "worker"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := newMockSchedulerCache("volcano")
			t.Cleanup(func() { ShutdownMockSchedulerCache(sc) })
			sc.AddPodGroupV1beta1(resyncTestPodGroup())
			pod := resyncTestPod(tt.initiallyGated)
			sc.AddPod(pod)
			original := schedulingapi.NewTaskInfo(pod)
			task := cachedResyncTask(sc, original)
			require.NotNil(t, task)
			if tt.beforeReading != nil {
				tt.beforeReading(t, sc, pod, task)
			}

			response := pod.DeepCopy()
			if tt.responseVersion != "" {
				response.ResourceVersion = tt.responseVersion
				response.Spec.SchedulingGates = nil
			}
			if tt.responseNewUID {
				response.UID = "replacement"
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			client := sc.kubeClient.(*fake.Clientset)
			client.PrependReactor("get", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
				close(entered)
				<-release
				if tt.getErr != nil {
					return true, nil, tt.getErr
				}
				return true, response, nil
			})
			result := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				unblock()
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("syncTask goroutine did not stop during cleanup")
				}
			})
			go func() {
				defer close(finished)
				result <- sc.syncTask(task)
			}()
			if !tt.skipGET {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("syncTask did not reach the API GET")
				}
				if tt.whileReading != nil {
					tt.whileReading(t, sc, pod, task)
				}
			}
			unblock()
			select {
			case err := <-result:
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("syncTask did not return after the API GET completed")
			}
			if tt.skipGET {
				select {
				case <-entered:
					t.Error("obsolete cache snapshot should not issue an API GET")
				default:
				}
			}

			cached := cachedResyncTask(sc, original)
			if !tt.wantTask {
				assert.Nil(t, cached)
				for _, job := range sc.Jobs {
					assert.NotContains(t, job.Tasks, schedulingapi.TaskID("replacement"))
				}
				return
			}
			require.NotNil(t, cached)
			assert.Equal(t, tt.wantVersion, cached.Pod.ResourceVersion)
			assert.Equal(t, tt.wantGated, cached.SchGated)
			assert.Equal(t, tt.wantStatus, cached.Status)
			assert.Equal(t, tt.wantNode, cached.NodeName)
			if tt.wantSameTask {
				assert.Same(t, task, cached)
				if tt.wantNode != "" {
					assert.Contains(t, sc.Nodes[tt.wantNode].Tasks, schedulingapi.PodKey(pod))
				}
			}
		})
	}
}

func TestSchedulerCache_ProcessResyncTask(t *testing.T) {
	tests := []struct {
		name             string
		concurrentUpdate bool
		concurrentDelete bool
		responseNewUID   bool
		getErr           error
		wantRetries      int
		wantCallbacks    int
		wantTask         bool
	}{
		{
			name:             "concurrent update retries and preserves binding cleanup",
			concurrentUpdate: true,
			wantRetries:      1,
			wantCallbacks:    1,
			wantTask:         true,
		},
		{
			name:           "replacement UID still runs cleanup for the old binding attempt",
			responseNewUID: true,
			wantCallbacks:  1,
		},
		{
			name:          "NotFound still runs cleanup for the old binding attempt",
			getErr:        apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "worker"),
			wantCallbacks: 1,
		},
		{
			name:             "task deleted during GET is forgotten and preserves binding cleanup",
			concurrentDelete: true,
			wantCallbacks:    1,
		},
		{
			name:          "normal resync still runs its binding recovery callback",
			wantCallbacks: 1,
			wantTask:      true,
		},
		{
			name:          "transient GET failure keeps the existing retry and callback behavior",
			getErr:        apierrors.NewServiceUnavailable("try again"),
			wantRetries:   1,
			wantCallbacks: 1,
			wantTask:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := newMockSchedulerCache("volcano")
			t.Cleanup(func() { ShutdownMockSchedulerCache(sc) })
			pod := resyncTestPod(true)
			sc.AddPod(pod)
			original := schedulingapi.NewTaskInfo(pod)
			task := cachedResyncTask(sc, original)
			require.NotNil(t, task)
			callbacks := 0
			task.CustomBindErrHandler = func() error { callbacks++; return nil }
			updated := pod.DeepCopy()
			updated.ResourceVersion = "2"
			updated.Spec.SchedulingGates = nil
			client := sc.kubeClient.(*fake.Clientset)
			client.PrependReactor("get", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if tt.concurrentDelete {
					sc.DeletePod(pod)
					return true, pod.DeepCopy(), nil
				}
				if tt.concurrentUpdate {
					// Drive the real informer callback while syncTask's API read
					// is in flight. No second fake-client call is made here.
					sc.UpdatePod(pod, updated)
					return true, pod.DeepCopy(), nil
				}
				if tt.getErr != nil {
					return true, nil, tt.getErr
				}
				response := updated.DeepCopy()
				if tt.responseNewUID {
					response.UID = "replacement"
				}
				return true, response, nil
			})

			key := sc.generateErrTaskKey(task)
			sc.resyncTask(task)
			sc.processResyncTask()

			assert.Equal(t, tt.wantRetries, sc.errTasks.NumRequeues(key))
			assert.Equal(t, tt.wantCallbacks, callbacks)
			cached := cachedResyncTask(sc, original)
			if !tt.wantTask {
				assert.Nil(t, cached)
				for _, job := range sc.Jobs {
					assert.NotContains(t, job.Tasks, schedulingapi.TaskID("replacement"))
				}
				return
			}
			require.NotNil(t, cached)
			if tt.getErr == nil {
				assert.Equal(t, "2", cached.Pod.ResourceVersion)
				assert.False(t, cached.SchGated)
			} else {
				assert.Equal(t, "1", cached.Pod.ResourceVersion)
				assert.True(t, cached.SchGated)
			}
		})
	}
}
