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
