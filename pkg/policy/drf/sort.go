/*
Copyright 2017 The Kubernetes Authors.

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

package drf

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
	"k8s.io/api/core/v1"
)

type job struct {
	dominantResource v1.ResourceName
	podSet           *cache.PodSet
}

type drfSort []*job

func (d drfSort) Len() int {
	return len(d)
}
func (d drfSort) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}
func (d drfSort) Less(i, j int) bool {
	j1 := d[i]
	j2 := d[j]

	r1 := j1.podSet.Allocated.Get(j1.dominantResource) / j1.podSet.TotalRequest.Get(j1.dominantResource)
	r2 := j1.podSet.Allocated.Get(j2.dominantResource) / j2.podSet.TotalRequest.Get(j1.dominantResource)

	return r1 < r2
}
