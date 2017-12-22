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

package util

import (
	"strconv"

	"k8s.io/api/core/v1"
)

type PodSlice []*v1.Pod

func (p PodSlice) Len() int {
	return len(p)
}

func (p PodSlice) Less(i, j int) bool {
	// compare preemption rank first
	p1 := 0
	p2 := 0
	if p[i].Labels != nil {
		p1, _ = strconv.Atoi(p[i].Labels["preemptionrank"])
	}
	if p[j].Labels != nil {
		p2, _ = strconv.Atoi(p[j].Labels["preemptionrank"])
	}
	if p1 != p2 {
		return p1 < p2
	}

	// if preemptionrank is same, pending pod is lower than running pod
	if p[i].Status.Phase == v1.PodPending {
		return true
	} else if p[j].Status.Phase == v1.PodPending {
		return false
	}

	// if both pods are running, compare start time
	time1 := p[i].Status.StartTime
	time2 := p[j].Status.StartTime
	return time2.Before(time1)
}

func (p PodSlice) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}
