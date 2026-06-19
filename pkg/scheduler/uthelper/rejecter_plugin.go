/*
Copyright 2024 The Volcano Authors.

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

package uthelper

import (
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// RejecterPlugin is a test helper plugin that rejects the job whose PodGroup
// name matches RejectJob and permits everything else. Used in multi-plugin
// enqueue tests to verify that a co-tier rejection leaves no phantom reservation.
type RejecterPlugin struct{ RejectJob string }

func (r *RejecterPlugin) Name() string { return "test-rejecter" }
func (r *RejecterPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobEnqueueableFn(r.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		if job.PodGroup != nil && job.PodGroup.Name == r.RejectJob {
			return -1 // Reject
		}
		return 1 // Permit
	})
}
func (r *RejecterPlugin) OnSessionClose(_ *framework.Session) {}
