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

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestDiscardRestoresEvictedTask(t *testing.T) {
	node := api.NewNodeInfo(util.BuildNode("node1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil))
	task := api.NewTaskInfo(util.BuildPod("ns1", "victim", node.Name, v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", nil, nil))
	job := api.NewJobInfo(task.Job, task)
	if err := node.AddTask(task); err != nil {
		t.Fatal(err)
	}
	ssn := &Session{
		Jobs:  map[api.JobID]*api.JobInfo{job.UID: job},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	var allocateEvents []*Event
	ssn.AddEventHandler(&EventHandler{
		AllocateFunc: func(event *Event) {
			allocateEvents = append(allocateEvents, event)
		},
	})

	stmt := NewStatement(ssn)
	if err := stmt.Evict(task, "test-reclaim"); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, api.Releasing, task.Status)

	stmt.Discard()

	assert.Equal(t, api.Running, task.Status)
	if assert.Len(t, allocateEvents, 1) {
		assert.Same(t, task, allocateEvents[0].Task)
		assert.True(t, allocateEvents[0].Restore, "an unevicted task must be marked as a restore")
	}
}
