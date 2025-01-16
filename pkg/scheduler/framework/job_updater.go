package framework

import (
	"context"
	"math/rand"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	jobUpdaterWorker = 16
)

// TimeJitterAfter means: new after old + duration + jitter
func TimeJitterAfter(new, old time.Time, duration, maxJitter time.Duration) bool {
	var jitter int64
	if maxJitter > 0 {
		jitter = rand.Int63n(int64(maxJitter))
	}
	return new.After(old.Add(duration + time.Duration(jitter)))
}

type jobUpdater struct {
	ssn      *Session
	jobQueue []*api.JobInfo
}

func newJobUpdater(ssn *Session) *jobUpdater {
	queue := make([]*api.JobInfo, 0, len(ssn.Jobs))
	for _, job := range ssn.Jobs {
		queue = append(queue, job)
	}

	ju := &jobUpdater{
		ssn:      ssn,
		jobQueue: queue,
	}
	return ju
}

func (ju *jobUpdater) UpdateAll() {
	workqueue.ParallelizeUntil(context.TODO(), jobUpdaterWorker, len(ju.jobQueue), ju.updateJob)
}

func isPodGroupConditionsUpdated(newCondition, oldCondition []scheduling.PodGroupCondition) bool {
	if len(newCondition) != len(oldCondition) {
		return true
	}

	for index, newCond := range newCondition {
		oldCond := oldCondition[index]

		newTime := newCond.LastTransitionTime
		oldTime := oldCond.LastTransitionTime

		// if newCond is not new enough, we treat it the same as the old one
		newCond.LastTransitionTime = oldTime

		// comparing should ignore the TransitionID
		newTransitionID := newCond.TransitionID
		newCond.TransitionID = oldCond.TransitionID

		shouldUpdate := !equality.Semantic.DeepEqual(&newCond, &oldCond)

		newCond.LastTransitionTime = newTime
		newCond.TransitionID = newTransitionID
		if shouldUpdate {
			return true
		}
	}

	return false
}

func isPodGroupStatusUpdated(newStatus, oldStatus scheduling.PodGroupStatus) bool {
	newCondition := newStatus.Conditions
	newStatus.Conditions = nil
	oldCondition := oldStatus.Conditions
	oldStatus.Conditions = nil

	return !equality.Semantic.DeepEqual(newStatus, oldStatus) || isPodGroupConditionsUpdated(newCondition, oldCondition)
}

// updateJob update specified job
func (ju *jobUpdater) updateJob(index int) {
	job := ju.jobQueue[index]
	ssn := ju.ssn

	job.PodGroup.Status = jobStatus(ssn, job)
	oldStatus, found := ssn.podGroupStatus[job.UID]
	updatePG := !found || isPodGroupStatusUpdated(job.PodGroup.Status, oldStatus)
	if _, err := ssn.cache.UpdateJobStatus(job, updatePG, ssn.podStatusRateLimit); err != nil {
		klog.Errorf("Failed to update job <%s/%s>: %v",
			job.Namespace, job.Name, err)
	}
}
