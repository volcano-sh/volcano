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

package job

import (
	"testing"

	gfh "github.com/AdaLogics/go-fuzz-headers"
	v1 "k8s.io/api/core/v1"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"volcano.sh/volcano/pkg/controllers/apis"
)

func FuzzApplyPolicies(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fdp := gfh.NewConsumer(data)
		// Create a reandom job
		job := &batch.Job{}
		fdp.GenerateStruct(job)
		// Create a random request
		req := &apis.Request{}
		fdp.GenerateStruct(req)
		applyPolicies(job, req)
	})
}

func FuzzCreateJobPod(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte, numaPolicy string, ix int, jobForwarding bool) {
		fdp := gfh.NewConsumer(data)
		// Create a random job
		job := &batch.Job{}
		fdp.GenerateStruct(job)
		// Create a random PTS
		template := &v1.PodTemplateSpec{}
		fdp.GenerateStruct(template)
		_ = createJobPod(job, template, batch.NumaPolicy(numaPolicy), ix, jobForwarding)
	})
}
