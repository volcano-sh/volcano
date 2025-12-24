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

package podgroup

import "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

type PodGroupStatistics struct {
	Inqueue   int
	Pending   int
	Running   int
	Unknown   int
	Completed int
}

func (pgStats *PodGroupStatistics) StatPodGroupCountsForQueue(pg *v1beta1.PodGroup) {
	switch pg.Status.Phase {
	case v1beta1.PodGroupInqueue:
		pgStats.Inqueue++
	case v1beta1.PodGroupPending:
		pgStats.Pending++
	case v1beta1.PodGroupRunning:
		pgStats.Running++
	case v1beta1.PodGroupUnknown:
		pgStats.Unknown++
	case v1beta1.PodGroupCompleted:
		pgStats.Completed++
	}
}
