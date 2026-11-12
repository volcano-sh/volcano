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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	informercache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type beforeAssumeCache struct {
	Cache
	beforeAssume func()
}

func (c *beforeAssumeCache) AddBindTask(bindContext *agentapi.BindContext) error {
	c.beforeAssume()
	return c.Cache.AddBindTask(bindContext)
}

func TestCheckAndBindPodPreservesInformerPod(t *testing.T) {
	for _, update := range []bool{false, true} {
		name := "add event"
		if update {
			name = "update event"
		}
		t.Run(name, func(t *testing.T) {
			cache, queue := mockForTest()
			defer cache.cancel()
			setupCacheWithNode(cache, "node-1")
			pod := createTestPod("pending-pod", "pod-uid")
			pod.ResourceVersion = "1"
			cache.AddPodToSchedulingQueue(pod)
			if update {
				updated := pod.DeepCopy()
				updated.ResourceVersion = "2"
				cache.UpdatePodInSchedulingQueue(pod, updated)
				pod = updated
			}
			podInfo, err := queue.Pop(klog.Background())
			require.NoError(t, err)
			task, found := cache.GetTaskInfo(api.TaskID(pod.UID))
			require.True(t, found)

			// The informer runs its assigned-Pod and pending-Pod listeners
			// independently. Let the assigned-Pod listener inspect the same
			// create/update object after the Binder selects a node but before
			// AddBindTask assumes it. The API Pod is still unbound here.
			assignedHandler := informercache.FilteringResourceEventHandler{
				FilterFunc: func(obj interface{}) bool { return obj.(*v1.Pod).Spec.NodeName != "" },
				Handler:    informercache.ResourceEventHandlerFuncs{AddFunc: cache.AddPodToCache},
			}
			binderCache := &beforeAssumeCache{Cache: cache, beforeAssume: func() {
				assignedHandler.OnAdd(pod, false)
			}}
			binder := NewConflictAwareBinder(binderCache, queue)
			schedCtx := &agentapi.SchedulingContext{Task: task, QueuedPodInfo: podInfo}
			binder.CheckAndBindPod(&agentapi.PodScheduleResult{
				SchedCtx: schedCtx,
				SuggestedNodes: []*api.NodeInfo{
					cache.Nodes["node-1"].info.Clone(),
				},
				BindContext: &agentapi.BindContext{SchedCtx: schedCtx},
			})

			assert.Empty(t, pod.Spec.NodeName, "binding must not assign the shared informer object")
			assert.Equal(t, "node-1", task.Pod.Spec.NodeName)
			assert.Equal(t, api.Binding, task.Status)
			select {
			case bindContext := <-cache.BindFlowChannel:
				assert.Same(t, task, bindContext.SchedCtx.Task)
			default:
				t.Error("the delayed informer handler must not create a phantom assignment that prevents binding")
			}
		})
	}
}
