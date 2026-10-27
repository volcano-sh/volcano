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
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	schedmetrics "volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestOpenSessionQueueTaskMetricsForNamespaceQueues(t *testing.T) {
	const queueName = "ns-queue-metrics-shared"
	teamA := string(api.NamespaceQueueID("team-a", queueName))
	teamB := string(api.NamespaceQueueID("team-b", queueName))
	for _, name := range []string{queueName, teamA, teamB} {
		schedmetrics.DeleteQueueMetrics(name)
		defer schedmetrics.DeleteQueueMetrics(name)
	}

	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	defer schedulerCache.OnSessionClose()
	clusterQueue := api.NewQueueInfo(&scheduling.Queue{ObjectMeta: metav1.ObjectMeta{Name: queueName}})
	schedulerCache.Queues[clusterQueue.UID] = clusterQueue
	for _, namespace := range []string{"team-a", "team-b"} {
		queue, err := api.NewNamespaceQueueInfo(&scheduling.NamespaceQueue{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: queueName},
			Spec:       scheduling.NamespaceQueueSpec{Parent: "cluster/" + queueName},
		})
		require.NoError(t, err)
		schedulerCache.Queues[queue.UID] = queue
	}

	addJob := func(namespace string, pending int) {
		var tasks []*api.TaskInfo
		for i := 0; i < pending; i++ {
			pod := util.BuildPod(namespace, "task-"+string(rune('a'+i)), "", v1.PodPending, nil, "job", nil, nil)
			tasks = append(tasks, api.NewTaskInfo(pod))
		}
		job := api.NewJobInfo(api.JobID(namespace+"/job"), tasks...)
		job.Queue = api.NamespaceQueueID(namespace, queueName)
		job.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: namespace},
			Spec:       scheduling.PodGroupSpec{Queue: queueName},
		}}
		schedulerCache.Jobs[job.UID] = job
	}
	addJob("team-a", 3)
	addJob("team-b", 1)

	OpenSession(schedulerCache, nil, nil)

	for name, expected := range map[string]float64{queueName: 0, teamA: 3, teamB: 1} {
		requireQueueGaugeValue(t, "volcano_queue_session_start_task_count", map[string]string{
			"queue_name": name,
			"status":     "pending",
		}, expected)
	}
}
