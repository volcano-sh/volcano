/*
Copyright 2019 The Volcano Authors.

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
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/controllers/apis"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

var calMutex sync.Mutex

// getPodGroupByJob returns the podgroup related to the vcjob.
// it will return normal pg if it exist in cluster,
// else it return legacy pg before version 1.5.
func (cc *jobcontroller) getPodGroupByJob(job *batch.Job) (*scheduling.PodGroup, error) {
	pgName := cc.generateRelatedPodGroupName(job)
	pg, err := cc.pgLister.PodGroups(job.Namespace).Get(pgName)
	if err == nil {
		return pg, nil
	}
	if apierrors.IsNotFound(err) {
		pg, err := cc.pgLister.PodGroups(job.Namespace).Get(job.Name)
		if err != nil {
			return nil, err
		}
		return pg, nil
	}
	return nil, err
}

func (cc *jobcontroller) generateRelatedPodGroupName(job *batch.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, string(job.UID))
}

func (cc *jobcontroller) killTarget(jobInfo *apis.JobInfo, target state.Target, updateStatus state.UpdateStatusFn) error {
	if target.Type == state.TargetTypeTask {
		klog.V(3).Infof("Killing task <%s> of Job <%s/%s>, current version %d", target.TaskName, jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)
		defer klog.V(3).Infof("Finished task <%s> of Job <%s/%s> killing, current version %d", target.TaskName, jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)
	} else if target.Type == state.TargetTypePod {
		klog.V(3).Infof("Killing pod <%s> of Job <%s/%s>, current version %d", target.PodName, jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)
		defer klog.V(3).Infof("Finished pod <%s> of Job <%s/%s> killing, current version %d", target.PodName, jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)
	}
	return cc.killPods(jobInfo, nil, &target, updateStatus)
}

func (cc *jobcontroller) killJob(jobInfo *apis.JobInfo, podRetainPhase state.PhaseMap, updateStatus state.UpdateStatusFn) error {
	klog.V(3).Infof("Killing Job <%s/%s>, current version %d", jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)
	defer klog.V(3).Infof("Finished Job <%s/%s> killing, current version %d", jobInfo.Namespace, jobInfo.Name, jobInfo.Job.Status.Version)

	return cc.killPods(jobInfo, podRetainPhase, nil, updateStatus)
}

func (cc *jobcontroller) killPods(jobInfo *apis.JobInfo, podRetainPhase state.PhaseMap, target *state.Target, updateStatus state.UpdateStatusFn) error {
	job := jobInfo.Job
	if job.DeletionTimestamp != nil {
		klog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	var pending, running, terminating, succeeded, failed, unknown int32
	taskStatusCount := make(map[string]batch.TaskState)

	var errs []error
	var total int

	podsToKill := make(map[string]*v1.Pod)

	if target != nil {
		if target.Type == state.TargetTypeTask {
			podsToKill = jobInfo.Pods[target.TaskName]
		} else if target.Type == state.TargetTypePod {
			podsToKill[target.PodName] = jobInfo.Pods[target.TaskName][target.PodName]
		}
		total += len(podsToKill)
	} else {
		// Job version is bumped only when job is killed
		job.Status.Version++
		for _, pods := range jobInfo.Pods {
			for _, pod := range pods {
				total++
				if pod.DeletionTimestamp != nil {
					klog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
					terminating++
					continue
				}

				maxRetry := job.Spec.MaxRetry
				lastRetry := false
				if job.Status.RetryCount >= maxRetry-1 {
					lastRetry = true
				}

				// Only retain the Failed and Succeeded pods at the last retry.
				// If it is not the last retry, kill pod as defined in `podRetainPhase`.
				retainPhase := podRetainPhase
				if lastRetry {
					retainPhase = state.PodRetainPhaseSoft
				}
				_, retain := retainPhase[pod.Status.Phase]

				if !retain {
					podsToKill[pod.Name] = pod
				}
			}
		}
	}

	for _, pod := range podsToKill {
		if pod.DeletionTimestamp != nil {
			klog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
			terminating++
			continue
		}

		err := cc.deleteJobPod(job.Name, pod)
		if err == nil {
			terminating++
			continue
		}
		// record the err, and then collect the pod info like retained pod
		errs = append(errs, err)
		cc.resyncTask(pod)

		classifyAndAddUpPodBaseOnPhase(pod, &pending, &running, &succeeded, &failed, &unknown)
		calcPodStatus(pod, taskStatusCount)
	}

	if len(errs) != 0 {
		klog.Errorf("failed to kill pods for job %s/%s, with err %+v", job.Namespace, job.Name, errs)
		cc.recorder.Event(job, v1.EventTypeWarning, FailedDeletePodReason,
			fmt.Sprintf("Error deleting pods: %+v", errs))
		return fmt.Errorf("failed to kill %d pods of %d", len(errs), total)
	}

	job = job.DeepCopy()
	job.Status.Pending = pending
	job.Status.Running = running
	job.Status.Succeeded = succeeded
	job.Status.Failed = failed
	job.Status.Terminating = terminating
	job.Status.Unknown = unknown
	job.Status.TaskStatusCount = taskStatusCount

	if updateStatus != nil {
		if updateStatus(&job.Status) {
			job.Status.State.LastTransitionTime = metav1.Now()
			jobCondition := newCondition(job.Status.State.Phase, &job.Status.State.LastTransitionTime)
			job.Status.Conditions = append(job.Status.Conditions, jobCondition)
		}
	}

	// Update running duration
	runningDuration := metav1.Duration{Duration: job.Status.State.LastTransitionTime.Sub(jobInfo.Job.CreationTimestamp.Time)}
	klog.V(3).Infof("Running duration is %s", runningDuration.ToUnstructured())
	job.Status.RunningDuration = &runningDuration

	// must be called before update job status
	if err := cc.pluginOnJobDelete(job); err != nil {
		return err
	}

	// Update Job status
	newJob, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(context.TODO(), job, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		klog.Errorf("Job %v/%v was not found", job.Namespace, job.Name)
		return nil
	}
	if err != nil {
		klog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}
	if e := cc.cache.Update(newJob); e != nil {
		klog.Errorf("KillJob - Failed to update Job %v/%v in cache:  %v",
			newJob.Namespace, newJob.Name, e)
		return e
	}

	// Delete PodGroup
	pg, err := cc.getPodGroupByJob(job)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to find PodGroup of Job: %s/%s, error: %s", job.Namespace, job.Name, err.Error())
		return err
	}
	if pg != nil {
		if err := cc.vcClient.SchedulingV1beta1().PodGroups(job.Namespace).Delete(context.TODO(), pg.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("Failed to delete PodGroup of Job %s/%s: %v", job.Namespace, job.Name, err)
				return err
			}
		}
	}

	// NOTE(k82cn): DO NOT delete input/output until job is deleted.

	return nil
}

func (cc *jobcontroller) initiateJob(job *batch.Job) (*batch.Job, error) {
	klog.V(3).Infof("Starting to initiate Job <%s/%s>", job.Namespace, job.Name)
	jobInstance, err := cc.initJobStatus(job)
	if err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.JobStatusError),
			fmt.Sprintf("Failed to initialize job status, err: %v", err))
		return nil, err
	}

	if err := cc.pluginOnJobAdd(jobInstance); err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.PluginError),
			fmt.Sprintf("Execute plugin when job add failed, err: %v", err))
		return nil, err
	}

	newJob, err := cc.createJobIOIfNotExist(jobInstance)
	if err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.PVCError),
			fmt.Sprintf("Failed to create PVC, err: %v", err))
		return nil, err
	}

	if err := cc.createOrUpdatePodGroup(newJob); err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.PodGroupError),
			fmt.Sprintf("Failed to create PodGroup, err: %v", err))
		return nil, err
	}

	return newJob, nil
}

func (cc *jobcontroller) initOnJobUpdate(job *batch.Job) error {
	klog.V(3).Infof("Starting to initiate Job <%s/%s> on update", job.Namespace, job.Name)

	if err := cc.pluginOnJobUpdate(job); err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.PluginError),
			fmt.Sprintf("Execute plugin when job add failed, err: %v", err))
		return err
	}

	if err := cc.createOrUpdatePodGroup(job); err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(batch.PodGroupError),
			fmt.Sprintf("Failed to create PodGroup, err: %v", err))
		return err
	}

	return nil
}

func (cc *jobcontroller) GetQueueInfo(queue string) (*scheduling.Queue, error) {
	queueInfo, err := cc.queueLister.Get(queue)
	if err != nil {
		klog.Errorf("Failed to get queue from queueLister, error: %s", err.Error())
	}

	return queueInfo, err
}

func (cc *jobcontroller) syncJob(jobInfo *apis.JobInfo, updateStatus state.UpdateStatusFn) error {
	job := jobInfo.Job
	klog.V(3).Infof("Starting to sync up Job <%s/%s>, current version %d", job.Namespace, job.Name, job.Status.Version)
	defer klog.V(3).Infof("Finished Job <%s/%s> sync up, current version %d", job.Namespace, job.Name, job.Status.Version)

	if jobInfo.Job.DeletionTimestamp != nil {
		klog.Infof("Job <%s/%s> is terminating, skip management process.",
			jobInfo.Job.Namespace, jobInfo.Job.Name)
		return nil
	}

	// deep copy job to prevent mutate it
	job = job.DeepCopy()

	// Find queue that job belongs to, and check if the queue has forwarding metadata
	queueInfo, err := cc.GetQueueInfo(job.Spec.Queue)
	if err != nil {
		return err
	}

	var jobForwarding bool
	if len(queueInfo.Spec.ExtendClusters) != 0 {
		jobForwarding = true
		if len(job.Annotations) == 0 {
			job.Annotations = make(map[string]string)
		}
		job.Annotations[batch.JobForwardingKey] = "true"
		job, err = cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).Update(context.TODO(), job, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update job: %s/%s, error: %s", job.Namespace, job.Name, err.Error())
			return err
		}
	}

	// Skip job initiation if job is already initiated
	if !isInitiated(job) {
		if job, err = cc.initiateJob(job); err != nil {
			return err
		}
	} else {
		// TODO: optimize this call it only when scale up/down
		if err = cc.initOnJobUpdate(job); err != nil {
			return err
		}
	}

	if len(queueInfo.Spec.ExtendClusters) != 0 {
		jobForwarding = true
		job.Annotations[batch.JobForwardingKey] = "true"
		_, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).Update(context.TODO(), job, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update job: %s/%s, error: %s", job.Namespace, job.Name, err.Error())
			return err
		}
	}

	var syncTask bool
	pg, err := cc.getPodGroupByJob(job)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to find PodGroup of Job: %s/%s, error: %s", job.Namespace, job.Name, err.Error())
		return err
	}
	if pg != nil {
		if pg.Status.Phase != "" && pg.Status.Phase != scheduling.PodGroupPending {
			syncTask = true
		}
		cc.recordPodGroupEvent(job, pg)
	}

	var jobCondition batch.JobCondition
	oldStatus := job.Status
	if !syncTask {
		if updateStatus != nil {
			updateStatus(&job.Status)
		}

		if equality.Semantic.DeepEqual(job.Status, oldStatus) {
			klog.V(4).Infof("Job <%s/%s> has not updated for no changing", job.Namespace, job.Name)
			return nil
		}
		job.Status.State.LastTransitionTime = metav1.Now()
		jobCondition = newCondition(job.Status.State.Phase, &job.Status.State.LastTransitionTime)
		job.Status.Conditions = append(job.Status.Conditions, jobCondition)
		newJob, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(context.TODO(), job, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update status of Job %v/%v: %v",
				job.Namespace, job.Name, err)
			return err
		}
		if e := cc.cache.Update(newJob); e != nil {
			klog.Errorf("SyncJob - Failed to update Job %v/%v in cache:  %v",
				newJob.Namespace, newJob.Name, e)
			return e
		}
		return nil
	}

	var running, pending, terminating, succeeded, failed, unknown int32
	taskStatusCount := make(map[string]batch.TaskState)

	podToCreate := make(map[string][]*v1.Pod)
	var podToDelete []*v1.Pod
	var creationErrs []error
	var deletionErrs []error
	appendMutex := sync.Mutex{}

	appendError := func(container *[]error, err error) {
		appendMutex.Lock()
		defer appendMutex.Unlock()
		*container = append(*container, err)
	}

	waitCreationGroup := sync.WaitGroup{}

	for _, ts := range job.Spec.Tasks {
		ts.Template.Name = ts.Name
		tc := ts.Template.DeepCopy()
		name := ts.Template.Name

		pods, found := jobInfo.Pods[name]
		if !found {
			pods = map[string]*v1.Pod{}
		}

		var podToCreateEachTask []*v1.Pod
		for i := 0; i < int(ts.Replicas); i++ {
			podName := fmt.Sprintf(jobhelpers.PodNameFmt, job.Name, name, i)
			if pod, found := pods[podName]; !found {
				newPod := createJobPod(job, tc, ts.TopologyPolicy, i, jobForwarding)
				if err := cc.pluginOnPodCreate(job, newPod); err != nil {
					return err
				}
				podToCreateEachTask = append(podToCreateEachTask, newPod)
				waitCreationGroup.Add(1)
			} else {
				delete(pods, podName)
				if pod.DeletionTimestamp != nil {
					klog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
					atomic.AddInt32(&terminating, 1)
					continue
				}

				classifyAndAddUpPodBaseOnPhase(pod, &pending, &running, &succeeded, &failed, &unknown)
				calcPodStatus(pod, taskStatusCount)
			}
		}
		podToCreate[ts.Name] = podToCreateEachTask
		for _, pod := range pods {
			podToDelete = append(podToDelete, pod)
		}
	}

	for taskName, podToCreateEachTask := range podToCreate {
		if len(podToCreateEachTask) == 0 {
			continue
		}
		go func(taskName string, podToCreateEachTask []*v1.Pod) {
			taskIndex := jobhelpers.GetTaskIndexUnderJob(taskName, job)
			if job.Spec.Tasks[taskIndex].DependsOn != nil {
				if !cc.waitDependsOnTaskMeetCondition(taskIndex, job) {
					klog.V(3).Infof("Job %s/%s depends on task not ready", job.Name, job.Namespace)
					// release wait group
					for _, pod := range podToCreateEachTask {
						go func(pod *v1.Pod) {
							defer waitCreationGroup.Done()
						}(pod)
					}
					return
				}
			}

			for _, pod := range podToCreateEachTask {
				go func(pod *v1.Pod) {
					defer waitCreationGroup.Done()
					newPod, err := cc.kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
					if err != nil && !apierrors.IsAlreadyExists(err) {
						// Failed to create Pod, waitCreationGroup a moment and then create it again
						// This is to ensure all podsMap under the same Job created
						// So gang-scheduling could schedule the Job successfully
						klog.Errorf("Failed to create pod %s for Job %s, err %#v",
							pod.Name, job.Name, err)
						appendError(&creationErrs, fmt.Errorf("failed to create pod %s, err: %#v", pod.Name, err))
					} else {
						classifyAndAddUpPodBaseOnPhase(newPod, &pending, &running, &succeeded, &failed, &unknown)
						calcPodStatus(newPod, taskStatusCount)
						klog.V(5).Infof("Created Task <%s> of Job <%s/%s>",
							pod.Name, job.Namespace, job.Name)
					}
				}(pod)
			}
		}(taskName, podToCreateEachTask)
	}

	waitCreationGroup.Wait()

	if len(creationErrs) != 0 {
		cc.recorder.Event(job, v1.EventTypeWarning, FailedCreatePodReason,
			fmt.Sprintf("Error creating pods: %+v", creationErrs))
		return fmt.Errorf("failed to create %d pods of %d", len(creationErrs), len(podToCreate))
	}

	// Delete pods when scale down.
	waitDeletionGroup := sync.WaitGroup{}
	waitDeletionGroup.Add(len(podToDelete))
	for _, pod := range podToDelete {
		go func(pod *v1.Pod) {
			defer waitDeletionGroup.Done()
			err := cc.deleteJobPod(job.Name, pod)
			if err != nil {
				// Failed to delete Pod, waitCreationGroup a moment and then create it again
				// This is to ensure all podsMap under the same Job created
				// So gang-scheduling could schedule the Job successfully
				klog.Errorf("Failed to delete pod %s for Job %s, err %#v",
					pod.Name, job.Name, err)
				appendError(&deletionErrs, err)
				cc.resyncTask(pod)
			} else {
				klog.V(3).Infof("Deleted Task <%s> of Job <%s/%s>",
					pod.Name, job.Namespace, job.Name)
				atomic.AddInt32(&terminating, 1)
			}
		}(pod)
	}
	waitDeletionGroup.Wait()

	if len(deletionErrs) != 0 {
		cc.recorder.Event(job, v1.EventTypeWarning, FailedDeletePodReason,
			fmt.Sprintf("Error deleting pods: %+v", deletionErrs))
		return fmt.Errorf("failed to delete %d pods of %d", len(deletionErrs), len(podToDelete))
	}

	newStatus := batch.JobStatus{
		State: job.Status.State,

		Pending:             pending,
		Running:             running,
		Succeeded:           succeeded,
		Failed:              failed,
		Terminating:         terminating,
		Unknown:             unknown,
		Version:             job.Status.Version,
		MinAvailable:        job.Spec.MinAvailable,
		TaskStatusCount:     taskStatusCount,
		ControlledResources: job.Status.ControlledResources,
		Conditions:          job.Status.Conditions,
		RetryCount:          job.Status.RetryCount,
	}

	if updateStatus != nil {
		updateStatus(&newStatus)
	}

	if reflect.DeepEqual(job.Status, newStatus) {
		klog.V(3).Infof("Job <%s/%s> has not updated for no changing", job.Namespace, job.Name)
		return nil
	}
	job.Status = newStatus
	job.Status.State.LastTransitionTime = metav1.Now()
	jobCondition = newCondition(job.Status.State.Phase, &job.Status.State.LastTransitionTime)
	job.Status.Conditions = append(job.Status.Conditions, jobCondition)
	newJob, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(context.TODO(), job, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}
	if e := cc.cache.Update(newJob); e != nil {
		klog.Errorf("SyncJob - Failed to update Job %v/%v in cache:  %v",
			newJob.Namespace, newJob.Name, e)
		return e
	}

	return nil
}

func (cc *jobcontroller) waitDependsOnTaskMeetCondition(taskIndex int, job *batch.Job) bool {
	if job.Spec.Tasks[taskIndex].DependsOn == nil {
		return true
	}
	dependsOn := *job.Spec.Tasks[taskIndex].DependsOn
	if len(dependsOn.Name) > 1 && dependsOn.Iteration == batch.IterationAny {
		// any ready to create task, return true
		for _, task := range dependsOn.Name {
			if cc.isDependsOnPodsReady(task, job) {
				return true
			}
		}
		// all not ready to skip create task, return false
		return false
	}
	for _, dependsOnTask := range dependsOn.Name {
		// any not ready to skip create task, return false
		if !cc.isDependsOnPodsReady(dependsOnTask, job) {
			return false
		}
	}
	// all ready to create task, return true
	return true
}

func (cc *jobcontroller) isDependsOnPodsReady(task string, job *batch.Job) bool {
	dependsOnPods := jobhelpers.GetPodsNameUnderTask(task, job)
	dependsOnTaskIndex := jobhelpers.GetTaskIndexUnderJob(task, job)
	runningPodCount := 0
	for _, podName := range dependsOnPods {
		pod, err := cc.podLister.Pods(job.Namespace).Get(podName)
		if err != nil {
			// If pod is not found. There are 2 possibilities.
			// 1. vcjob has been deleted. This function should return true.
			// 2. pod is not created. This function should return false, continue waiting.
			if apierrors.IsNotFound(err) {
				_, errGetJob := cc.jobLister.Jobs(job.Namespace).Get(job.Name)
				if errGetJob != nil {
					return apierrors.IsNotFound(errGetJob)
				}
			}
			klog.Errorf("Failed to get pod %v/%v %v", job.Namespace, podName, err)
			continue
		}

		if pod.Status.Phase != v1.PodRunning && pod.Status.Phase != v1.PodSucceeded {
			klog.V(5).Infof("Sequential state, pod %v/%v of depends on tasks is not running", pod.Namespace, pod.Name)
			continue
		}

		allContainerReady := true
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allContainerReady = false
				break
			}
		}
		if allContainerReady {
			runningPodCount++
		}
	}
	dependsOnTaskMinReplicas := job.Spec.Tasks[dependsOnTaskIndex].MinAvailable
	if dependsOnTaskMinReplicas != nil {
		if runningPodCount < int(*dependsOnTaskMinReplicas) {
			klog.V(5).Infof("In a depends on startup state, there are already %d pods running, which is less than the minimum number of runs", runningPodCount)
			return false
		}
	}
	return true
}

func (cc *jobcontroller) createJobIOIfNotExist(job *batch.Job) (*batch.Job, error) {
	// If PVC does not exist, create them for Job.
	var needUpdate bool
	if job.Status.ControlledResources == nil {
		job.Status.ControlledResources = make(map[string]string)
	}
	for index, volume := range job.Spec.Volumes {
		vcName := volume.VolumeClaimName
		if len(vcName) == 0 {
			// NOTE(k82cn): Ensure never have duplicated generated names.
			for {
				vcName = jobhelpers.GenPVCName(job.Name)
				exist, err := cc.checkPVCExist(job, vcName)
				if err != nil {
					return job, err
				}
				if exist {
					continue
				}
				job.Spec.Volumes[index].VolumeClaimName = vcName
				needUpdate = true
				break
			}
			// TODO: check VolumeClaim must be set if VolumeClaimName is empty
			if volume.VolumeClaim != nil {
				if err := cc.createPVC(job, vcName, volume.VolumeClaim); err != nil {
					return job, err
				}
			}
		} else {
			exist, err := cc.checkPVCExist(job, vcName)
			if err != nil {
				return job, err
			}
			if !exist {
				return job, fmt.Errorf("pvc %s is not found, the job will be in the Pending state until the PVC is created", vcName)
			}
		}
		job.Status.ControlledResources["volume-pvc-"+vcName] = vcName
	}
	if needUpdate {
		newJob, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).Update(context.TODO(), job, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update Job %v/%v for volume claim name: %v ",
				job.Namespace, job.Name, err)
			return job, err
		}

		newJob.Status = job.Status
		return newJob, err
	}
	return job, nil
}

func (cc *jobcontroller) checkPVCExist(job *batch.Job, pvc string) (bool, error) {
	if _, err := cc.pvcLister.PersistentVolumeClaims(job.Namespace).Get(pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		klog.V(3).Infof("Failed to get PVC %s for job <%s/%s>: %v",
			pvc, job.Namespace, job.Name, err)
		return false, err
	}
	return true, nil
}

func (cc *jobcontroller) createPVC(job *batch.Job, vcName string, volumeClaim *v1.PersistentVolumeClaimSpec) error {
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: job.Namespace,
			Name:      vcName,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, helpers.JobKind),
			},
		},
		Spec: *volumeClaim,
	}

	klog.V(3).Infof("Try to create PVC: %v", pvc)

	if _, e := cc.kubeClient.CoreV1().PersistentVolumeClaims(job.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{}); e != nil {
		klog.V(3).Infof("Failed to create PVC for Job <%s/%s>: %v",
			job.Namespace, job.Name, e)
		return e
	}
	return nil
}

func (cc *jobcontroller) createOrUpdatePodGroup(job *batch.Job) error {
	// If PodGroup does not exist, create one for Job.
	pg, err := cc.getPodGroupByJob(job)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get PodGroup for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}
		minTaskMember := map[string]int32{}
		for _, task := range job.Spec.Tasks {
			if task.MinAvailable != nil {
				minTaskMember[task.Name] = *task.MinAvailable
			} else {
				minTaskMember[task.Name] = task.Replicas
			}
		}

		pg := &scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				// add job.UID into its name when create new PodGroup
				Name:        cc.generateRelatedPodGroupName(job),
				Annotations: job.Annotations,
				Labels:      job.Labels,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember:         job.Spec.MinAvailable,
				MinTaskMember:     minTaskMember,
				Queue:             job.Spec.Queue,
				MinResources:      cc.calcPGMinResources(job),
				PriorityClassName: job.Spec.PriorityClassName,
			},
		}
		if job.Spec.NetworkTopology != nil {
			nt := &scheduling.NetworkTopologySpec{
				Mode: scheduling.NetworkTopologyMode(job.Spec.NetworkTopology.Mode),
			}
			if job.Spec.NetworkTopology.HighestTierAllowed != nil {
				nt.HighestTierAllowed = job.Spec.NetworkTopology.HighestTierAllowed
			}
			pg.Spec.NetworkTopology = nt
		}

		if _, err = cc.vcClient.SchedulingV1beta1().PodGroups(job.Namespace).Create(context.TODO(), pg, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				klog.Errorf("Failed to create PodGroup for Job <%s/%s>: %v",
					job.Namespace, job.Name, err)
				return err
			}
		}
		return nil
	}

	pgShouldUpdate := false
	if pg.Spec.PriorityClassName != job.Spec.PriorityClassName {
		pg.Spec.PriorityClassName = job.Spec.PriorityClassName
		pgShouldUpdate = true
	}

	minResources := cc.calcPGMinResources(job)
	if pg.Spec.MinMember != job.Spec.MinAvailable || !equality.Semantic.DeepEqual(pg.Spec.MinResources, minResources) {
		pg.Spec.MinMember = job.Spec.MinAvailable
		pg.Spec.MinResources = minResources
		pgShouldUpdate = true
	}

	if pg.Spec.MinTaskMember == nil {
		pgShouldUpdate = true
		pg.Spec.MinTaskMember = make(map[string]int32)
	}

	for _, task := range job.Spec.Tasks {
		cnt := task.Replicas
		if task.MinAvailable != nil {
			cnt = *task.MinAvailable
		}

		if taskMember, ok := pg.Spec.MinTaskMember[task.Name]; !ok {
			pgShouldUpdate = true
			pg.Spec.MinTaskMember[task.Name] = cnt
		} else {
			if taskMember == cnt {
				continue
			}

			pgShouldUpdate = true
			pg.Spec.MinTaskMember[task.Name] = cnt
		}
	}

	if !pgShouldUpdate {
		return nil
	}

	_, err = cc.vcClient.SchedulingV1beta1().PodGroups(job.Namespace).Update(context.TODO(), pg, metav1.UpdateOptions{})
	if err != nil {
		klog.V(3).Infof("Failed to update PodGroup for Job <%s/%s>: %v",
			job.Namespace, job.Name, err)
	}
	return err
}

func (cc *jobcontroller) deleteJobPod(jobName string, pod *v1.Pod) error {
	err := cc.kubeClient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to delete pod %s/%s for Job %s, err %#v",
			pod.Namespace, pod.Name, jobName, err)

		return fmt.Errorf("failed to delete pod %s, err %#v", pod.Name, err)
	}

	return nil
}

func (cc *jobcontroller) calcPGMinResources(job *batch.Job) *v1.ResourceList {
	// sort task by priorityClasses
	var tasksPriority TasksPriority
	totalMinAvailable := int32(0)
	for _, task := range job.Spec.Tasks {
		tp := TaskPriority{0, task}
		pc := task.Template.Spec.PriorityClassName

		if pc != "" {
			priorityClass, err := cc.pcLister.Get(pc)
			if err != nil || priorityClass == nil {
				klog.Warningf("Ignore task %s priority class %s: %v", task.Name, pc, err)
			} else {
				tp.priority = priorityClass.Value
			}
		}
		tasksPriority = append(tasksPriority, tp)
		if task.MinAvailable != nil { // actually, it can not be nil, because nil value will be patched in webhook
			totalMinAvailable += *task.MinAvailable
		} else {
			totalMinAvailable += task.Replicas
		}
	}

	// see docs https://github.com/volcano-sh/volcano/pull/2945
	// 1. job.MinAvailable < sum(task.MinAvailable), regard podgroup's min resource as sum of the first minAvailable,
	// according to https://github.com/volcano-sh/volcano/blob/c91eb07f2c300e4d5c826ff11a63b91781b3ac11/pkg/scheduler/api/job_info.go#L738-L740
	if job.Spec.MinAvailable < totalMinAvailable {
		minReq := tasksPriority.CalcFirstCountResources(job.Spec.MinAvailable)
		return &minReq
	}

	// 2. job.MinAvailable >= sum(task.MinAvailable)
	minReq := tasksPriority.CalcPGMinResources(job.Spec.MinAvailable)

	return &minReq
}

func (cc *jobcontroller) initJobStatus(job *batch.Job) (*batch.Job, error) {
	if job.Status.State.Phase != "" {
		return job, nil
	}

	job.Status.State.Phase = batch.Pending
	job.Status.State.LastTransitionTime = metav1.Now()
	job.Status.MinAvailable = job.Spec.MinAvailable
	jobCondition := newCondition(job.Status.State.Phase, &job.Status.State.LastTransitionTime)
	job.Status.Conditions = append(job.Status.Conditions, jobCondition)
	newJob, err := cc.vcClient.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(context.TODO(), job, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return nil, err
	}
	if err := cc.cache.Update(newJob); err != nil {
		klog.Errorf("CreateJob - Failed to update Job %v/%v in cache:  %v",
			newJob.Namespace, newJob.Name, err)
		return nil, err
	}

	return newJob, nil
}

func (cc *jobcontroller) recordPodGroupEvent(job *batch.Job, podGroup *scheduling.PodGroup) {
	var latestCondition *scheduling.PodGroupCondition

	// Get the latest condition by timestamp
	for _, condition := range podGroup.Status.Conditions {
		if condition.Status == v1.ConditionTrue {
			if latestCondition == nil ||
				condition.LastTransitionTime.Time.After(latestCondition.LastTransitionTime.Time) {
				latestCondition = &condition
			}
		}
	}

	// If the latest condition is not scheduled, then a warning event is recorded
	if latestCondition != nil && latestCondition.Type != scheduling.PodGroupScheduled {
		cc.recorder.Eventf(job, v1.EventTypeWarning, string(batch.PodGroupPending),
			fmt.Sprintf("PodGroup %s:%s %s, reason: %s", job.Namespace, job.Name,
				strings.ToLower(string(latestCondition.Type)), latestCondition.Message))
	}
}

func classifyAndAddUpPodBaseOnPhase(pod *v1.Pod, pending, running, succeeded, failed, unknown *int32) {
	switch pod.Status.Phase {
	case v1.PodPending:
		atomic.AddInt32(pending, 1)
	case v1.PodRunning:
		atomic.AddInt32(running, 1)
	case v1.PodSucceeded:
		atomic.AddInt32(succeeded, 1)
	case v1.PodFailed:
		atomic.AddInt32(failed, 1)
	default:
		atomic.AddInt32(unknown, 1)
	}
}

func calcPodStatus(pod *v1.Pod, taskStatusCount map[string]batch.TaskState) {
	taskName, found := pod.Annotations[batch.TaskSpecKey]
	if !found {
		return
	}

	calMutex.Lock()
	defer calMutex.Unlock()
	if _, ok := taskStatusCount[taskName]; !ok {
		taskStatusCount[taskName] = batch.TaskState{
			Phase: make(map[v1.PodPhase]int32),
		}
	}

	switch pod.Status.Phase {
	case v1.PodPending:
		taskStatusCount[taskName].Phase[v1.PodPending]++
	case v1.PodRunning:
		taskStatusCount[taskName].Phase[v1.PodRunning]++
	case v1.PodSucceeded:
		taskStatusCount[taskName].Phase[v1.PodSucceeded]++
	case v1.PodFailed:
		taskStatusCount[taskName].Phase[v1.PodFailed]++
	default:
		taskStatusCount[taskName].Phase[v1.PodUnknown]++
	}
}

func isInitiated(job *batch.Job) bool {
	if job.Status.State.Phase == "" || job.Status.State.Phase == batch.Pending {
		return false
	}

	return true
}

func newCondition(status batch.JobPhase, lastTransitionTime *metav1.Time) batch.JobCondition {
	return batch.JobCondition{
		Status:             status,
		LastTransitionTime: lastTransitionTime,
	}
}
