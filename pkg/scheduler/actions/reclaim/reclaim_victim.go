/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

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

package reclaim

import (
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// isReclaimVictimTask reports whether task may be considered as a reclaim victim
// for tasks in reclaimorJob's queue.
func isReclaimVictimTask(ssn *framework.Session, task *api.TaskInfo, reclaimorJob *api.JobInfo) bool {
	if task.Status != api.Running || !task.Preemptable {
		return false
	}
	j, found := ssn.Jobs[task.Job]
	if !found {
		return false
	}
	if j.Queue == reclaimorJob.Queue {
		return false
	}
	q := ssn.Queues[j.Queue]
	if q == nil || !q.Reclaimable() {
		return false
	}
	return true
}
