/*
Copyright 2018 The Kubernetes Authors.

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
	"math"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

func Min(l, r *api.Resource) *api.Resource {
	res := &api.Resource{}

	res.MilliCPU = math.Min(l.MilliCPU, r.MilliCPU)
	res.MilliGPU = math.Min(l.MilliGPU, r.MilliGPU)
	res.Memory = math.Min(l.Memory, r.Memory)

	return res
}

func Share(l, r float64) float64 {
	var share float64
	if r == 0 {
		if l == 0 {
			share = 0
		} else {
			share = 1
		}
	} else {
		share = l / r
	}

	return share
}
