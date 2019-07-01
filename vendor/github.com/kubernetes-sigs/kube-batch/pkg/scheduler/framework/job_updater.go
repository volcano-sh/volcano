package framework

import (
	"context"
	"math/rand"
	"reflect"
	"time"

	"github.com/golang/glog"

	"k8s.io/client-go/util/workqueue"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

const (
	jobUpdaterWorker = 16

	jobConditionUpdateTime       = time.Minute
	jobConditionUpdateTimeJitter = 30 * time.Second
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

func isPodGroupConditionsUpdated(newCondition, oldCondition []v1alpha1.PodGroupCondition) bool {
	if len(newCondition) != len(oldCondition) {
		return true
	}

	for index, newCond := range newCondition {
		oldCond := oldCondition[index]

		newTime := newCond.LastTransitionTime
		oldTime := oldCond.LastTransitionTime
		if TimeJitterAfter(newTime.Time, oldTime.Time, jobConditionUpdateTime, jobConditionUpdateTimeJitter) {
			return true
		}

		// if newCond is not new enough, we treat it the same as the old one
		newCond.LastTransitionTime = oldTime

		// comparing should ignore the TransitionID
		newTransitionID := newCond.TransitionID
		newCond.TransitionID = oldCond.TransitionID

		shouldUpdate := !reflect.DeepEqual(&newCond, &oldCond)

		newCond.LastTransitionTime = newTime
		newCond.TransitionID = newTransitionID
		if shouldUpdate {
			return true
		}
	}

	return false
}

func isPodGroupStatusUpdated(newStatus, oldStatus *v1alpha1.PodGroupStatus) bool {
	newCondition := newStatus.Conditions
	newStatus.Conditions = nil
	oldCondition := oldStatus.Conditions
	oldStatus.Conditions = nil

	shouldUpdate := !reflect.DeepEqual(newStatus, oldStatus) || isPodGroupConditionsUpdated(newCondition, oldCondition)

	newStatus.Conditions = newCondition
	oldStatus.Conditions = oldCondition

	return shouldUpdate
}

// updateJob update specified job
func (ju *jobUpdater) updateJob(index int) {
	job := ju.jobQueue[index]
	ssn := ju.ssn

	// If job is using PDB, ignore it.
	// TODO(k82cn): remove it when removing PDB support
	if job.PodGroup == nil {
		ssn.cache.RecordJobStatusEvent(job)
		return
	}

	job.PodGroup.Status = jobStatus(ssn, job)
	oldStatus, found := ssn.podGroupStatus[job.UID]
	updatePG := !found || isPodGroupStatusUpdated(&job.PodGroup.Status, oldStatus)

	if _, err := ssn.cache.UpdateJobStatus(job, updatePG); err != nil {
		glog.Errorf("Failed to update job <%s/%s>: %v",
			job.Namespace, job.Name, err)
	}
}
