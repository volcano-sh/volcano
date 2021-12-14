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

package helpers

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestCompareTask(t *testing.T) {
	createTime := time.Now()
	items := []struct {
		lv     *api.TaskInfo
		rv     *api.TaskInfo
		expect bool
	}{
		{
			generateTaskInfo("prod-worker-21", createTime),
			generateTaskInfo("prod-worker-1", createTime),
			false,
		},
		{
			generateTaskInfo("prod-worker-0", createTime),
			generateTaskInfo("prod-worker-3", createTime),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime),
			generateTaskInfo("prod-worker-3", createTime.Add(time.Hour)),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime),
			generateTaskInfo("prod-worker-3", createTime.Add(-time.Hour)),
			false,
		},
	}

	for i, item := range items {
		if value := CompareTask(item.lv, item.rv); value != item.expect {
			t.Errorf("case %d: expected: %v, got %v", i, item.expect, value)
		}
	}
}

func generateTaskInfo(name string, createTime time.Time) *api.TaskInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Time{Time: createTime},
		},
	}
	return &api.TaskInfo{
		Name: name,
		Pod:  pod,
	}
}
