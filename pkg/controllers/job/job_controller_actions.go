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
	"sync"

	"github.com/golang/glog"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	admissioncontroller "volcano.sh/volcano/pkg/admission"
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/controllers/job/apis"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

func (cc * Controller) ConfigureJob(jobInfo *apis.JobInfo) error{
	fmt.Println("going to ConfigureJob job info==================================================")
	fmt.Println(jobInfo.Job.Status)
	if job, err := cc.vkClients.BatchV1alpha1().Jobs(jobInfo.Job.Namespace).UpdateStatus(jobInfo.Job); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v in configuration action",
			job.Namespace, job.Name, err)
		return err
	} else {
		if e := cc.cache.Update(job); e != nil {
			return e
		}
	}
	return nil
}


func (cc *Controller) killJob(jobInfo *apis.JobInfo, nextState state.NextStateFn) error {
	glog.V(3).Infof("Killing Job <%s/%s>", jobInfo.Job.Namespace, jobInfo.Job.Name)
	defer glog.V(3).Infof("Finished Job <%s/%s> killing", jobInfo.Job.Namespace, jobInfo.Job.Name)

	job := jobInfo.Job
	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	var pending, running, terminating, succeeded, failed int32

	var errs []error
	var total int

	for _, pods := range jobInfo.Pods {
		for _, pds := range pods {
			for _, pod := range pds {
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
						errs = append(errs, err)
						continue
					}
					terminating++
				case v1.PodPending:
					err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
					if err != nil {
						pending++
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
						errs = append(errs, err)
						continue
					}
					terminating++
				}
			}
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("failed to kill %d pods of %d", len(errs), total)
	}

	job.Status = vkv1.JobStatus{
		State: job.Status.State,

		Pending:      pending,
		Running:      running,
		Succeeded:    succeeded,
		Failed:       failed,
		Terminating:  terminating,
		MinAvailable: int32(job.Spec.MinAvailable),
	}

	if nextState != nil {
		job.Status.State = nextState(job.Status)
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

	// Delete Service
	if err := cc.kubeClients.CoreV1().Services(job.Namespace).Delete(job.Name, nil); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.Errorf("Failed to delete Service of Job %v/%v: %v",
				job.Namespace, job.Name, err)
			return err
		}
	}

	// NOTE(k82cn): DO NOT delete input/output until job is deleted.

	return nil
}

func (cc *Controller) syncJobs(jobInfo *apis.JobInfo, nextState state.NextStateFn) error {

	if !jobInfo.JobStarted() {
		glog.Warningf("Job %s/%s has not been started, skipping sync",
			jobInfo.Namespace, jobInfo.Name)
	}
	if jobInfo.JobAbandoned(){
		if err := cc.killJob(jobInfo, nextState); err != nil {
			return err
		}

	}else{
		if err := cc.syncJob(jobInfo, nextState); err != nil {
			return err
		}
	}
	return nil
}

func (cc *Controller) syncJob(jobInfo *apis.JobInfo, nextState state.NextStateFn) error {
	glog.V(3).Infof("Starting to sync up Job <%s/%s>", jobInfo.Job.Namespace, jobInfo.Job.Name)
	defer glog.V(3).Infof("Finished Job <%s/%s> sync up", jobInfo.Job.Namespace, jobInfo.Job.Name)

	job := jobInfo.Job.DeepCopy()

	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	if err := cc.createPodGroupIfNotExist(job); err != nil {
		return err
	}

	if err := cc.createJobIOIfNotExist(job); err != nil {
		return err
	}

	if err := cc.createServiceIfNotExist(job); err != nil {
		return err
	}

	var running, pending, terminating, succeeded, failed int32
	jobVersion := fmt.Sprintf("%d", job.Status.State.Version)

	fmt.Println("============================pods in cache====================")
	for k, pods := range jobInfo.Pods {
		for podName, pd := range pods {
			for v, pod := range pd{
				fmt.Printf("Pod:%s %s %s %s\n", k, podName, pod.Name, v)
			}
		}
	}

	for _, ts := range job.Spec.Tasks {
		fmt.Println("this is the task")
		fmt.Printf(ts.Name)
	}
	var podToCreate []*v1.Pod
	var podToDelete []*v1.Pod
	var deletionErrs []error
	var creationErrs []error
	for _, ts := range job.Spec.Tasks {
		ts.Template.Name = ts.Name
		tc := ts.Template.DeepCopy()
		name := ts.Template.Name
		fmt.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!starting to handle job %s\n", name)

		pods, found := jobInfo.Pods[name]
		if !found {
			pods = map[string]map[string]*v1.Pod{}
		}

		for i := 0; i < int(ts.Replicas); i++ {
			podName := fmt.Sprintf(TaskNameFmt, job.Name, name, i)
			if _, found := pods[podName]; !found {
				newPod := createJobPod(job, tc, i)
				fmt.Printf("going to append pod to create: %s, %s\n", newPod.Name,GetPodVersion(newPod))
				podToCreate = append(podToCreate, newPod)
			} else if _, found := pods[podName][jobVersion]; !found{
				newPod := createJobPod(job, tc, i)
				fmt.Printf("going to append pod to create: %s, %s\n", newPod.Name,GetPodVersion(newPod))
				podToCreate = append(podToCreate, newPod)
			} else {
				pod := pods[podName][jobVersion]
				if pod.DeletionTimestamp != nil {
					glog.Infof("Pod <%s/%s> is terminating", pod.Namespace, pod.Name)
					terminating++
					delete(pods[podName], jobVersion)
					continue
				}
				glog.Infof("Starting to refresh Pod status <%s/%s/%s>", pod.Namespace, pod.Name, GetPodVersion(pod))
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
				delete(pods[podName], jobVersion)
			}
		}

		for _, pds := range pods {
			for _, pod := range pds {
				podToDelete = append(podToDelete, pod)
			}
		}
	}

	// Delete unnecessary pods.
	waitDeletionGroup := sync.WaitGroup{}
	waitDeletionGroup.Add(len(podToDelete))
	for _, pod := range podToDelete {
		go func(pod *v1.Pod) {
			glog.Infof("Starting to terminate Pod <%s/%s/%s>", pod.Namespace, pod.Name, GetPodVersion(pod))
			defer waitDeletionGroup.Done()
			if pod.DeletionTimestamp != nil {
				glog.Infof("Pod <%s/%s/%s> is terminating", pod.Namespace, pod.Name, GetPodVersion(pod))
				terminating++
				return
			}
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

	waitCreationGroup := sync.WaitGroup{}
	waitCreationGroup.Add(len(podToCreate))
	for _, pod := range podToCreate {
		go func(pod *v1.Pod) {
			glog.Infof("Starting to create Pod <%s/%s/%s>", pod.Namespace, pod.Name, GetPodVersion(pod))
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

	job.Status = vkv1.JobStatus{
		State: job.Status.State,

		Pending:      pending,
		Running:      running,
		Succeeded:    succeeded,
		Failed:       failed,
		Terminating:  terminating,
		MinAvailable: int32(job.Spec.MinAvailable),
	}

	if nextState != nil {
		oldState := job.Status.State.DeepCopy()
		job.Status.State = nextState(job.Status)
		job.Status.State.Version = oldState.Version
	}
	fmt.Println("going to update jobs=in sync methods========================")
	fmt.Println(job.Status)

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

func (cc *Controller) createServiceIfNotExist(job *vkv1.Job) error {
	// If Service does not exist, create one for Job.
	if _, err := cc.svcLister.Services(job.Namespace).Get(job.Name); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.V(3).Infof("Failed to get Service for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      job.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: v1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					vkv1.JobNameKey:      job.Name,
					vkv1.JobNamespaceKey: job.Namespace,
				},
			},
		}

		if _, e := cc.kubeClients.CoreV1().Services(job.Namespace).Create(svc); e != nil {
			glog.V(3).Infof("Failed to create Service for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)

			return e
		}
	}

	return nil
}

func (cc *Controller) createJobIOIfNotExist(job *vkv1.Job) error {
	// If input/output PVC does not exist, create them for Job.
	inputPVC := job.Annotations[admissioncontroller.PVCInputName]
	outputPVC := job.Annotations[admissioncontroller.PVCOutputName]
	if job.Spec.Input != nil {
		if job.Spec.Input.VolumeClaim != nil {
			if _, err := cc.pvcLister.PersistentVolumeClaims(job.Namespace).Get(inputPVC); err != nil {
				if !apierrors.IsNotFound(err) {
					glog.V(3).Infof("Failed to get input PVC for Job <%s/%s>: %v",
						job.Namespace, job.Name, err)
					return err
				}

				pvc := &v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: job.Namespace,
						Name:      inputPVC,
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(job, helpers.JobKind),
						},
					},
					Spec: *job.Spec.Input.VolumeClaim,
				}

				glog.V(3).Infof("Try to create input PVC: %v", pvc)

				if _, e := cc.kubeClients.CoreV1().PersistentVolumeClaims(job.Namespace).Create(pvc); e != nil {
					glog.V(3).Infof("Failed to create input PVC for Job <%s/%s>: %v",
						job.Namespace, job.Name, err)
					return e
				}
			}
		}
	}
	if job.Spec.Output != nil {
		if job.Spec.Output.VolumeClaim != nil {
			if _, err := cc.pvcLister.PersistentVolumeClaims(job.Namespace).Get(outputPVC); err != nil {
				if !apierrors.IsNotFound(err) {
					glog.V(3).Infof("Failed to get output PVC for Job <%s/%s>: %v",
						job.Namespace, job.Name, err)
					//return err
				}

				pvc := &v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: job.Namespace,
						Name:      outputPVC,
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(job, helpers.JobKind),
						},
					},
					Spec: *job.Spec.Output.VolumeClaim,
				}

				glog.V(3).Infof("Try to create output PVC: %v", pvc)

				if _, e := cc.kubeClients.CoreV1().PersistentVolumeClaims(job.Namespace).Create(pvc); e != nil {
					glog.V(3).Infof("Failed to create input PVC for Job <%s/%s>: %v",
						job.Namespace, job.Name, err)
					return e
				}
			}
		}
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
				Namespace: job.Namespace,
				Name:      job.Name,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: kbv1.PodGroupSpec{
				MinMember: job.Spec.MinAvailable,
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
