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

package cronjob

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	batchv1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"

	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
)

func (cc *cronjobcontroller) addJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		klog.Errorf("obj is not Job")
		return
	}
	if job.DeletionTimestamp != nil {
		cc.deleteJob(job)
		return
	}
	cronJob := cc.isControlledBy(job)
	if cronJob != nil {
		cc.addCronjobcontrollerQueue(cronJob, 0)
		return
	}
}
func (cc *cronjobcontroller) updateJob(oldObj, newObj interface{}) {
	oldJob, okOld := oldObj.(*batchv1.Job)
	if !okOld {
		klog.Errorf("Failed to convert %v to batchv1.Job", oldObj)
		return
	}
	newJob, okNew := newObj.(*batchv1.Job)
	if !okNew {
		klog.Errorf("Failed to convert %v to batchv1.Job", newObj)
		return
	}
	if newJob.ResourceVersion == oldJob.ResourceVersion {
		return
	}
	oldCronJob := cc.isControlledBy(oldJob)
	newCronJob := cc.isControlledBy(newJob)
	if newCronJob != nil {
		cc.addCronjobcontrollerQueue(newCronJob, 0)
	}
	oldEqualNew := reflect.DeepEqual(metav1.GetControllerOf(oldJob), metav1.GetControllerOf(newJob))
	if !oldEqualNew && oldCronJob != nil {
		cc.addCronjobcontrollerQueue(oldCronJob, 0)
	}
}
func (cc *cronjobcontroller) deleteJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		job, ok = tombstone.Obj.(*batchv1.Job)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a Job: %#v", obj)
			return
		}
	}
	cronJob := cc.isControlledBy(job)
	if cronJob != nil {
		cc.addCronjobcontrollerQueue(cronJob, 0)
	}
}
func (cc *cronjobcontroller) addCronjobcontrollerQueue(obj interface{}, duration time.Duration) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to get key for object <%v>: %v", obj, err)
		return
	}
	if duration <= 0 {
		cc.queue.Add(key)
		return
	}
	cc.queue.AddAfter(key, duration)
}
func (cc *cronjobcontroller) updateCronJob(oldObj interface{}, newObj interface{}) {
	oldCronjob, okOld := oldObj.(*batchv1.CronJob)
	if !okOld {
		klog.Errorf("Failed to convert %v to batchv1.CronJob", oldObj)
		return
	}
	newCronjob, okNew := newObj.(*batchv1.CronJob)
	if !okNew {
		klog.Errorf("Failed to convert %v to batchv1.CronJob", newObj)
		return
	}
	if newCronjob.ResourceVersion == oldCronjob.ResourceVersion {
		klog.V(6).Infof("No need to update because cronjob is not modified.")
		return
	}

	if oldCronjob.Spec.Schedule != newCronjob.Spec.Schedule || !ptr.Equal(oldCronjob.Spec.TimeZone, newCronjob.Spec.TimeZone) {
		sched, err := cron.ParseStandard(formatSchedule(newCronjob, nil))
		if err != nil {
			klog.V(2).Info("Unparseable schedule for cronjob", "cronjob", klog.KObj(newCronjob), "schedule", newCronjob.Spec.Schedule, "err", err)
			cc.recorder.Eventf(newCronjob, corev1.EventTypeWarning, "UnParseableCronJobSchedule", "unparseable schedule for cronjob: %s", newCronjob.Spec.Schedule)
			return
		}
		now := cc.now()
		t := nextScheduleTimeDuration(newCronjob, now, sched)

		cc.addCronjobcontrollerQueue(newObj, *t)
		return
	}

	cc.addCronjobcontrollerQueue(newObj, 0)
}
func (cc *cronjobcontroller) isControlledBy(job *batchv1.Job) *batchv1.CronJob {
	controllerRef := metav1.GetControllerOf(job)
	if controllerRef == nil {
		return nil
	}
	if controllerRef.APIVersion == controllerKind.GroupVersion().String() && controllerRef.Kind == controllerKind.Kind {
		cronJob, err := cc.cronJobList.CronJobs(job.Namespace).Get(controllerRef.Name)
		if err != nil || cronJob.UID != controllerRef.UID {
			return nil
		}
		return cronJob
	}
	return nil
}

func (cc *cronjobcontroller) getJobsByCronJob(cronJob *batchv1.CronJob) ([]*batchv1.Job, error) {
	jobList, err := cc.jobLister.Jobs(cronJob.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	jobsByCronJob := make([]*batchv1.Job, 0, len(jobList))

	for _, job := range jobList {
		controllerRef := metav1.GetControllerOf(job)
		if controllerRef == nil ||
			controllerRef.Kind != "CronJob" ||
			controllerRef.Name != cronJob.Name ||
			controllerRef.UID != cronJob.UID {
			continue
		}
		jobsByCronJob = append(jobsByCronJob, job)
	}

	return jobsByCronJob, nil
}
func (cc *cronjobcontroller) processFinishedJobs(cronJob *batchv1.CronJob, jobsByCronJob []*batchv1.Job) bool {
	updateStatus := false
	failedJobs := []*batchv1.Job{}
	successfulJobs := []*batchv1.Job{}

	for _, job := range jobsByCronJob {
		isFinish, phase := isJobFinished(job)
		if isFinish {
			//remove from active list if job is finished
			if found := inActiveList(cronJob, job.ObjectMeta.UID); found {
				deleteFromActiveList(cronJob, job.ObjectMeta.UID)
				cc.recorder.Eventf(cronJob, corev1.EventTypeNormal, "SawCompletedJob", "Saw completed job: %s in active, remove", job.Name)
				updateStatus = true
			}
			switch phase {
			case batchv1.Completed:
				// If the job is successful, update the last successful time
				jobFinishTime := job.Status.State.LastTransitionTime
				if cronJob.Status.LastSuccessfulTime == nil {
					cronJob.Status.LastSuccessfulTime = &jobFinishTime
					updateStatus = true
				}
				if !jobFinishTime.IsZero() && cronJob.Status.LastSuccessfulTime != nil && jobFinishTime.After(cronJob.Status.LastSuccessfulTime.Time) {
					cronJob.Status.LastSuccessfulTime = &jobFinishTime
					updateStatus = true
				}
				successfulJobs = append(successfulJobs, job)
			case batchv1.Failed:
				failedJobs = append(failedJobs, job)
			}
		}
	}

	//delete old jobs
	if cronJob.Spec.FailedJobsHistoryLimit == nil && cronJob.Spec.SuccessfulJobsHistoryLimit == nil {
		return updateStatus
	}
	if cronJob.Spec.SuccessfulJobsHistoryLimit != nil &&
		cc.removeOldestJobs(cronJob,
			successfulJobs,
			*cronJob.Spec.SuccessfulJobsHistoryLimit) {
		updateStatus = true
	}

	if cronJob.Spec.FailedJobsHistoryLimit != nil &&
		cc.removeOldestJobs(cronJob,
			failedJobs,
			*cronJob.Spec.FailedJobsHistoryLimit) {
		updateStatus = true
	}

	return updateStatus
}

// 1. Identifies and handles orphaned jobs (jobs owned by the CronJob but not in its active list)
// 2. Cleans up stale references in the CronJob's active list (jobs that no longer exist or have mismatched UIDs)
func (cc *cronjobcontroller) processCtlJobAndActiveJob(cronJob *batchv1.CronJob, jobsByCronJob []*batchv1.Job) (bool, error) {
	updateStatus := false
	ctrlJobs := make(map[types.UID]bool)

	for _, job := range jobsByCronJob {
		ctrlJobs[job.UID] = true
		found := inActiveList(cronJob, job.ObjectMeta.UID)
		isFinish, _ := isJobFinished(job)
		if !found && !isFinish {
			cjCopy, err := cc.cronjobClient.getCronJobClient(cc.vcClient, cronJob.Namespace, cronJob.Name)
			if err != nil {
				return updateStatus, err
			}
			if inActiveList(cjCopy, job.ObjectMeta.UID) {
				cronJob = cjCopy
				continue
			}
			cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "OrphanedJob",
				"Detected orphaned job forgotten or not managed by controller: %s (UID: %s)",
				job.Name, job.UID)
		}
	}

	for _, activeRef := range cronJob.Status.Active {
		if _, managed := ctrlJobs[activeRef.UID]; managed {
			continue
		}
		job, err := cc.jobLister.Jobs(activeRef.Namespace).Get(activeRef.Name)
		switch {
		case errors.IsNotFound(err):
			klog.Warningf("Orphaned active job reference detected: %s/%s (UID: %s)",
				activeRef.Namespace, activeRef.Name, activeRef.UID)

			cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "StaleReference",
				"Active job %q no longer exists (UID: %s)", activeRef.Name, activeRef.UID)
			deleteFromActiveList(cronJob, activeRef.UID)
			updateStatus = true
		case err != nil:
			klog.Errorf("Failed to check job %s/%s: %v", activeRef.Namespace, activeRef.Name, err)
			return updateStatus, err
		default:
			if job.UID != activeRef.UID {
				cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "StaleReference",
					"Active job reference %q points to a different UID (expected: %s, actual: %s)",
					activeRef.Name, activeRef.UID, job.UID)
				deleteFromActiveList(cronJob, activeRef.UID)
				updateStatus = true
			} else {
				klog.V(4).Infof("Job %s/%s exists but not managed by this controller",
					activeRef.Namespace, activeRef.Name)
			}
		}
	}
	return updateStatus, nil
}
func isJobFinished(job *batchv1.Job) (bool, batchv1.JobPhase) {
	if job.Status.State.Phase == batchv1.Completed || job.Status.State.Phase == batchv1.Failed || job.Status.State.Phase == batchv1.Terminated {
		return true, job.Status.State.Phase
	}
	return false, ""
}
func (cc *cronjobcontroller) removeOldestJobs(cj *batchv1.CronJob, js []*batchv1.Job, maxJobs int32) bool {
	updateStatus := false
	numToDelete := len(js) - int(maxJobs)
	if numToDelete <= 0 {
		return updateStatus
	}
	klog.V(4).Info("Cleaning up jobs from CronJob list", "deletejobnum", numToDelete, "jobnum", len(js), "cronjob", klog.KObj(cj))
	sort.Sort(byJobCreationTimestamp(js))
	for i := 0; i < numToDelete; i++ {
		klog.V(4).Info("Removing job from CronJob list", "job", js[i].Name, "cronjob", klog.KObj(cj))
		if deleteJobByClient(cc.vcClient, cc.jobClient, cj, js[i], cc.recorder) {
			updateStatus = true
		}
	}
	return updateStatus
}

// deleteJobByClient attempts to delete a Job through the API server and updates the CronJob's status.
// Returns true if deletion was successful, false otherwise.
func deleteJobByClient(vcClient vcclientset.Interface, jobClient jobClientInterface, cc *batchv1.CronJob, job *batchv1.Job, recorder record.EventRecorder) bool {
	if vcClient == nil || cc == nil || job == nil {
		klog.ErrorS(nil, "Invalid arguments provided to deleteJob",
			"clientNil", vcClient == nil,
			"cronjobNil", cc == nil,
			"jobNil", job == nil)
		return false
	}

	// Execute Clinet deletion
	err := jobClient.deleteJobClient(vcClient, job.Namespace, job.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to delete Job",
			"job", klog.KObj(job),
			"cronjob", klog.KObj(cc))
		recorder.Eventf(cc, corev1.EventTypeWarning, "FailedDelete",
			"Error deleting job %q: %v", job.Name, err)

		return false
	}

	// Cleanup local state after successful deletion
	deleteFromActiveList(cc, job.ObjectMeta.UID)
	recorder.Eventf(cc, corev1.EventTypeNormal, "SuccessfulDelete",
		"Deleted job %q", job.Name)
	return true
}

// return if addqueueAfter and updateStatus
func (cc *cronjobcontroller) processConcurrencyPolicy(cj *batchv1.CronJob) (bool, bool, error) {
	if cj.Spec.ConcurrencyPolicy == batchv1.ForbidConcurrent {
		if len(cj.Status.Active) > 0 {
			klog.V(4).Info("Forbid concurrent jobs for CronJob", "cronjob", klog.KObj(cj))
			cc.recorder.Eventf(cj, corev1.EventTypeWarning, "ForbidConcurrent",
				"Skipping job creation because another job is already running")
			return true, false, nil
		}
	}

	if cj.Spec.ConcurrencyPolicy == batchv1.ReplaceConcurrent {
		updateStatus := false
		if len(cj.Status.Active) > 0 {
			klog.V(4).Info("Replacing active job for CronJob", "cronjob", klog.KObj(cj))
			cc.recorder.Eventf(cj, corev1.EventTypeWarning, "ReplaceConcurrent",
				"Replacing active job for CronJob")
			for _, activeJob := range cj.Status.Active {
				klog.V(4).Info("Deleting job that was still running at next scheduled start time", "job", klog.KRef(activeJob.Namespace, activeJob.Name))
				job, err := cc.jobClient.getJobClient(cc.vcClient, activeJob.Namespace, activeJob.Name)
				if err != nil {
					cc.recorder.Eventf(cj, corev1.EventTypeWarning, "FailedGet", "Get job: %v", err)
					return false, updateStatus, err
				}
				if !deleteJobByClient(cc.vcClient, cc.jobClient, cj, job, cc.recorder) {
					return false, updateStatus, fmt.Errorf("could not replace job %s/%s", job.Namespace, job.Name)
				}
				updateStatus = true
			}
		}
		return false, updateStatus, nil
	}
	return false, false, nil
}
func (cc *cronjobcontroller) createJob(cronJob *batchv1.CronJob, scheduledTime time.Time) (*batchv1.Job, error) {
	jobTemplate, err := getJobFromTemplate(cronJob, scheduledTime)
	if err != nil {
		klog.Errorf("Failed to get job from template for cronjob %s: %v", klog.KObj(cronJob), err)
		cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "FailedCreate", "Failed to create job from template: %v", err)
		return nil, err
	}
	job, err := cc.jobClient.createJobClient(cc.vcClient, cronJob.Namespace, jobTemplate)
	switch {
	case errors.HasStatusCause(err, corev1.NamespaceTerminatingCause):
		klog.V(2).Infof("Namespace %s is terminating, skipping job creation for CronJob %s", cronJob.Namespace, klog.KObj(cronJob))
		cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "NamespaceTerminating", "Namespace %s is terminating, skipping job creation", cronJob.Namespace)
		return nil, err

	case errors.IsAlreadyExists(err):
		existingJob, err := cc.jobClient.getJobClient(cc.vcClient, jobTemplate.Namespace, jobTemplate.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warningf("Job %s disappeared after creation conflict, retrying", jobTemplate.Name)
				return nil, err
			}
			return nil, fmt.Errorf("failed to fetch conflicting job: %w", err)
		}
		if !metav1.IsControlledBy(existingJob, cronJob) {
			ownerRef := metav1.GetControllerOf(existingJob)
			ownerName := "none"
			if ownerRef != nil {
				ownerName = ownerRef.Name
			}
			cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "ForeignJob",
				"Job %s exists but is not owned by this CronJob (current owner: %s)",
				existingJob.Name, ownerName)
			return nil, nil
		}
		if isFinished, _ := isJobFinished(existingJob); isFinished {
			klog.V(2).Infof("Found finished duplicate job %s", klog.KObj(existingJob))
			return nil, nil
		}

		klog.V(2).Infof("Job %s already exists and is managed by this CronJob", klog.KObj(existingJob))
		return existingJob, nil
	case err != nil:
		klog.Errorf("Failed to create job for CronJob %s: %v", klog.KObj(cronJob), err)
		cc.recorder.Eventf(cronJob, corev1.EventTypeWarning, "FailedCreate", "Failed to create job: %v", err)
		return nil, err
	}

	if !metav1.IsControlledBy(job, cronJob) {
		cleanupErr := cc.jobClient.deleteJobClient(cc.vcClient, job.Namespace, job.Name)
		if cleanupErr != nil {
			klog.Errorf("Failed to cleanup orphaned job %s: %v", klog.KObj(job), cleanupErr)
		}
		return nil, fmt.Errorf("created job missing owner reference")
	}

	klog.V(2).Infof("Created job %s for CronJob %s", klog.KObj(job), klog.KObj(cronJob))
	cc.recorder.Eventf(cronJob, corev1.EventTypeNormal, "SuccessfulCreate",
		"Created job %s for CronJob %s", job.Name, klog.KObj(cronJob))
	return job, nil
}
func getRef(object runtime.Object) (*corev1.ObjectReference, error) {
	return ref.GetReference(scheme.Scheme, object)
}
