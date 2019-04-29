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
	"fmt"
	"sort"
	"sync"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	kbv1 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/apis"
	vkjobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/job/state"
	kbapi "volcano.sh/volcano/pkg/scheduler/api"
)

func (cc *Controller) killJob(jobInfo *apis.JobInfo, updateStatus state.UpdateStatusFn) error {
	glog.V(3).Infof("Killing Job <%s/%s>", jobInfo.Job.Namespace, jobInfo.Job.Name)
	defer glog.V(3).Infof("Finished Job <%s/%s> killing", jobInfo.Job.Namespace, jobInfo.Job.Name)

	job := jobInfo.Job
	//Job version is bumped only when job is killed
	job.Status.Version = job.Status.Version + 1
	glog.Infof("Current Version is: %d of job: %s/%s", job.Status.Version, job.Namespace, job.Name)
	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	var pending, running, terminating, succeeded, failed int32

	var errs []error
	var total int

	for _, pods := range jobInfo.Pods {
		for _, pod := range pods {
			total++

			if pod.DeletionTimestamp != nil {
				glog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
				terminating++
				continue
			}

			switch pod.Status.Phase {
			case v1.PodRunning:
				err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
				if err != nil {
					running++
					glog.Errorf("Failed to delete pod %s for Job %s, err %#v",
						pod.Name, job.Name, err)
					errs = append(errs, err)
					continue
				}
				terminating++
			case v1.PodPending:
				err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
				if err != nil {
					pending++
					glog.Errorf("Failed to delete pod %s for Job %s, err %#v",
						pod.Name, job.Name, err)
					errs = append(errs, err)
					continue
				}
				terminating++
			case v1.PodSucceeded:
				succeeded++
			case v1.PodFailed:
				err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
				if err != nil {
					failed++
					glog.Errorf("Failed to delete pod %s for Job %s, err %#v",
						pod.Name, job.Name, err)
					errs = append(errs, err)
					continue
				}
				terminating++
			}
		}
	}

	if len(errs) != 0 {
		glog.Errorf("failed to kill pods for job %s/%s, with err %+v", job.Namespace, job.Name, errs)
		return fmt.Errorf("failed to kill %d pods of %d", len(errs), total)
	}

	job.Status = vkv1.JobStatus{
		State: job.Status.State,

		Pending:      pending,
		Running:      running,
		Succeeded:    succeeded,
		Failed:       failed,
		Terminating:  terminating,
		Version:      job.Status.Version,
		MinAvailable: int32(job.Spec.MinAvailable),
		RetryCount:   job.Status.RetryCount,
	}

	if updateStatus != nil {
		updateStatus(&job.Status)
	}

	// Update Job status
	if job, err := cc.vkClients.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(job); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	} else {
		if e := cc.cache.Update(job); e != nil {
			return e
		}
	}

	// Delete PodGroup
	if err := cc.kbClients.SchedulingV1alpha1().PodGroups(job.Namespace).Delete(job.Name, nil); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.Errorf("Failed to delete PodGroup of Job %v/%v: %v",
				job.Namespace, job.Name, err)
			return err
		}
	}

	if err := cc.pluginOnJobDelete(job); err != nil {
		return err
	}

	// NOTE(k82cn): DO NOT delete input/output until job is deleted.

	return nil
}

func (cc *Controller) createJob(jobInfo *apis.JobInfo, nextState state.UpdateStatusFn) error {
	glog.V(3).Infof("Starting to create Job <%s/%s>", jobInfo.Job.Namespace, jobInfo.Job.Name)
	defer glog.V(3).Infof("Finished Job <%s/%s> create", jobInfo.Job.Namespace, jobInfo.Job.Name)

	job := jobInfo.Job
	glog.Infof("Current Version is: %d of job: %s/%s", job.Status.Version, job.Namespace, job.Name)

	update, err := cc.checkUpdate(job)
	if err != nil {
		return err
	}
	if update {
		if job, err := cc.vkClients.BatchV1alpha1().Jobs(job.Namespace).Update(job); err != nil {
			glog.Errorf("Failed to update Job %v/%v: %v",
				job.Namespace, job.Name, err)
			return err
		}
		return nil
	}

	if err := cc.pluginOnJobAdd(job); err != nil {
		cc.recorder.Event(job, v1.EventTypeWarning, string(vkbatchv1.PluginError),
			fmt.Sprintf("Execute plugin when job add failed, err: %v", err))
		return err
	}

	if err := cc.createPodGroupIfNotExist(job); err != nil {
		return err
	}

	if err := cc.createJobIOIfNotExist(job); err != nil {
		return err
	}

	return nil
}

func (cc *Controller) syncJob(jobInfo *apis.JobInfo, updateStatus state.UpdateStatusFn) error {
	glog.V(3).Infof("Starting to sync up Job <%s/%s>", jobInfo.Job.Namespace, jobInfo.Job.Name)
	defer glog.V(3).Infof("Finished Job <%s/%s> sync up", jobInfo.Job.Namespace, jobInfo.Job.Name)

	job := jobInfo.Job
	glog.Infof("Current Version is: %d of job: %s/%s", job.Status.Version, job.Namespace, job.Name)

	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	var running, pending, terminating, succeeded, failed int32

	var podToCreate []*v1.Pod
	var podToDelete []*v1.Pod
	var creationErrs []error
	var deletionErrs []error

	for _, ts := range job.Spec.Tasks {
		ts.Template.Name = ts.Name
		tc := ts.Template.DeepCopy()
		name := ts.Template.Name

		pods, found := jobInfo.Pods[name]
		if !found {
			pods = map[string]*v1.Pod{}
		}

		for i := 0; i < int(ts.Replicas); i++ {
			podName := fmt.Sprintf(vkjobhelpers.PodNameFmt, job.Name, name, i)
			if pod, found := pods[podName]; !found {
				newPod := createJobPod(job, tc, i)
				if err := cc.pluginOnPodCreate(job, newPod); err != nil {
					return err
				}
				podToCreate = append(podToCreate, newPod)
			} else {
				if pod.DeletionTimestamp != nil {
					glog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
					terminating++
					delete(pods, podName)
					continue
				}

				switch pod.Status.Phase {
				case v1.PodPending:
					if pod.DeletionTimestamp != nil {
						terminating++
					} else {
						pending++
					}
				case v1.PodRunning:
					if pod.DeletionTimestamp != nil {
						terminating++
					} else {
						running++
					} /**/
				case v1.PodSucceeded:
					succeeded++
				case v1.PodFailed:
					failed++
				}
				delete(pods, podName)
			}
		}

		for _, pod := range pods {
			podToDelete = append(podToDelete, pod)
		}
	}

	waitCreationGroup := sync.WaitGroup{}
	waitCreationGroup.Add(len(podToCreate))
	for _, pod := range podToCreate {
		go func(pod *v1.Pod) {
			defer waitCreationGroup.Done()
			_, err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Create(pod)
			if err != nil {
				// Failed to create Pod, waitCreationGroup a moment and then create it again
				// This is to ensure all podsMap under the same Job created
				// So gang-scheduling could schedule the Job successfully
				glog.Errorf("Failed to create pod %s for Job %s, err %#v",
					pod.Name, job.Name, err)
				creationErrs = append(creationErrs, err)
			} else {
				pending++
				glog.V(3).Infof("Created Task <%s> of Job <%s/%s>",
					pod.Name, job.Namespace, job.Name)
			}
		}(pod)
	}
	waitCreationGroup.Wait()

	if len(creationErrs) != 0 {
		return fmt.Errorf("failed to create %d pods of %d", len(creationErrs), len(podToCreate))
	}

	// Delete unnecessary pods.
	waitDeletionGroup := sync.WaitGroup{}
	waitDeletionGroup.Add(len(podToDelete))
	for _, pod := range podToDelete {
		go func(pod *v1.Pod) {
			defer waitDeletionGroup.Done()
			err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
			if err != nil {
				// Failed to create Pod, waitCreationGroup a moment and then create it again
				// This is to ensure all podsMap under the same Job created
				// So gang-scheduling could schedule the Job successfully
				glog.Errorf("Failed to delete pod %s for Job %s, err %#v",
					pod.Name, job.Name, err)
				deletionErrs = append(deletionErrs, err)
			} else {
				glog.V(3).Infof("Deleted Task <%s> of Job <%s/%s>",
					pod.Name, job.Namespace, job.Name)
				terminating++
			}
		}(pod)
	}
	waitDeletionGroup.Wait()

	if len(deletionErrs) != 0 {
		return fmt.Errorf("failed to delete %d pods of %d", len(deletionErrs), len(podToDelete))
	}

	job.Status = vkv1.JobStatus{
		State: job.Status.State,

		Pending:             pending,
		Running:             running,
		Succeeded:           succeeded,
		Failed:              failed,
		Terminating:         terminating,
		Version:             job.Status.Version,
		MinAvailable:        int32(job.Spec.MinAvailable),
		ControlledResources: job.Status.ControlledResources,
		RetryCount:          job.Status.RetryCount,
	}

	if updateStatus != nil {
		updateStatus(&job.Status)
	}

	if job, err := cc.vkClients.BatchV1alpha1().Jobs(job.Namespace).UpdateStatus(job); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	} else {
		if e := cc.cache.Update(job); e != nil {
			return e
		}
	}

	return nil
}

func (cc *Controller) calculateVersion(current int32, bumpVersion bool) int32 {
	if current == 0 {
		current += 1
	}
	if bumpVersion {
		current += 1
	}
	return current
}

func (cc *Controller) createJobIOIfNotExist(job *vkv1.Job) error {
	// If PVC does not exist, create them for Job.
	volumes := job.Spec.Volumes
	for _, volume := range volumes {
		vcName := volume.VolumeClaimName
		exist, err := cc.checkPVCExist(job, vcName)
		if err != nil {
			return err
		}
		if !exist {
			if job.Status.ControlledResources == nil {
				job.Status.ControlledResources = make(map[string]string)
			}
			if volume.VolumeClaim != nil {
				if err := cc.createPVC(job, vcName, volume.VolumeClaim); err != nil {
					return err
				}
				job.Status.ControlledResources["volume-pvc-"+vcName] = vcName
			} else {
				job.Status.ControlledResources["volume-emptyDir-"+vcName] = vcName
			}
		}
	}
	return nil
}

func (cc *Controller) checkUpdate(job *vkv1.Job) (bool, error) {
	// If VolumeClaimName does not exist, generate them for Job.
	volumes := job.Spec.Volumes
	update := false
	for index, volume := range volumes {
		vcName := volume.VolumeClaimName
		if len(vcName) == 0 {
			for {
				randomStr := vkjobhelpers.GenRandomStr(12)
				vcName = fmt.Sprintf("%s-volume-%s", job.Name, randomStr)
				exist, err := cc.checkPVCExist(job, vcName)
				if err != nil {
					return false, err
				}
				if !exist {
					volumes[index].VolumeClaimName = vcName
					update = true
					break
				}
			}
		}
	}
	return update, nil
}

func (cc *Controller) checkPVCExist(job *vkv1.Job, vcName string) (bool, error) {
	if _, err := cc.pvcLister.PersistentVolumeClaims(job.Namespace).Get(vcName); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		glog.V(3).Infof("Failed to get PVC for job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return false, err
	}
	return true, nil
}

func (cc *Controller) createPVC(job *vkv1.Job, vcName string, volumeClaim *v1.PersistentVolumeClaimSpec) error {
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

	glog.V(3).Infof("Try to create PVC: %v", pvc)

	if _, e := cc.kubeClients.CoreV1().PersistentVolumeClaims(job.Namespace).Create(pvc); e != nil {
		glog.V(3).Infof("Failed to create PVC for Job <%s/%s>: %v",
			job.Namespace, job.Name, e)
		return e
	}
	return nil
}

func (cc *Controller) createPodGroupIfNotExist(job *vkv1.Job) error {
	// If PodGroup does not exist, create one for Job.
	if _, err := cc.pgLister.PodGroups(job.Namespace).Get(job.Name); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.V(3).Infof("Failed to get PodGroup for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}
		pg := &kbv1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   job.Namespace,
				Name:        job.Name,
				Annotations: job.Annotations,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: kbv1.PodGroupSpec{
				MinMember:    job.Spec.MinAvailable,
				Queue:        job.Spec.Queue,
				MinResources: cc.calcPGMinResources(job),
			},
		}

		if _, e := cc.kbClients.SchedulingV1alpha1().PodGroups(job.Namespace).Create(pg); e != nil {
			glog.V(3).Infof("Failed to create PodGroup for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)

			return e
		}
	}

	return nil
}

type TaskPriority struct {
	priority int32

	vkbatchv1.TaskSpec
}

type TasksPriority []TaskPriority

func (p TasksPriority) Len() int { return len(p) }

func (p TasksPriority) Less(i, j int) bool {
	return p[i].priority > p[j].priority
}

func (p TasksPriority) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func (cc *Controller) calcPGMinResources(job *vkv1.Job) *v1.ResourceList {
	// sort task by priorityClasses
	var tasksPriority TasksPriority
	for index, _ := range job.Spec.Tasks {
		tp := TaskPriority{0, job.Spec.Tasks[index]}
		pc := job.Spec.Tasks[index].Template.Spec.PriorityClassName
		if len(cc.priorityClasses) != 0 && cc.priorityClasses[pc] != nil {
			tp.priority = cc.priorityClasses[pc].Value
		}
		tasksPriority = append(tasksPriority, tp)
	}

	sort.Sort(tasksPriority)

	minAvailableTasksRes := kbapi.EmptyResource()
	podCnt := int32(0)
	for _, task := range tasksPriority {
		for i := int32(0); i < task.Replicas; i++ {
			if podCnt >= job.Spec.MinAvailable {
				break
			}
			podCnt++
			for _, c := range task.Template.Spec.Containers {
				minAvailableTasksRes.Add(kbapi.NewResource(c.Resources.Requests))
			}
		}
	}

	return minAvailableTasksRes.Convert2K8sResource()
}
