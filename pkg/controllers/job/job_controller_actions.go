/*
Copyright 2019 The Vulcan Authors.

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

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"

	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
	"hpw.cloud/volcano/pkg/controllers/job/state"
)

func (cc *Controller) killJob(job *vkv1.Job, nextState state.NextStateFn) error {
	job, err := cc.jobLister.Jobs(job.Namespace).Get(job.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(3).Infof("Job has been deleted: %v", job.Name)
			return nil
		}
		return err
	}

	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	podsMap, err := getPodsForJob(cc.podLister, job)
	if err != nil {
		return err
	}

	var pending, running, terminating, succeeded, failed int32

	var errs []error
	var total int

	for _, pods := range podsMap {
		for _, pod := range pods {
			total++

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
				failed++
			}
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("failed to kill %d pods of %d", len(errs), total)
	}

	job.Status = vkv1.JobStatus{
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

	if _, err := cc.vkClients.BatchV1alpha1().Jobs(job.Namespace).Update(job); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	if err := cc.kbClients.SchedulingV1alpha1().PodGroups(job.Namespace).Delete(job.Name, nil); err != nil {
		glog.Errorf("Failed to delete PodGroup of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	if err := cc.kubeClients.CoreV1().Services(job.Namespace).Delete(job.Name, nil); err != nil {
		glog.Errorf("Failed to delete Service of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	// NOTE(k82cn): DO NOT delete input/output until job is deleted.

	return nil
}

func (cc *Controller) syncJob(job *vkv1.Job, nextState state.NextStateFn) error {
	job, err := cc.jobLister.Jobs(job.Namespace).Get(job.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if job.DeletionTimestamp != nil {
		glog.Infof("Job <%s/%s> is terminating, skip management process.",
			job.Namespace, job.Name)
		return nil
	}

	podsMap, err := getPodsForJob(cc.podLister, job)
	if err != nil {
		return err
	}

	glog.V(3).Infof("Start to manage job <%s/%s>", job.Namespace, job.Name)

	// TODO(k82cn): add WebHook to validate job.
	if err := validate(job); err != nil {
		glog.Errorf("Failed to validate Job <%s/%s>: %v", job.Namespace, job.Name, err)
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

	var podToCreate []*v1.Pod
	var podToDelete []*v1.Pod

	var running, pending, terminating, succeeded, failed int32

	for _, ts := range job.Spec.Tasks {
		name := ts.Template.Name
		// TODO(k82cn): the template name should be set in default func.
		if len(name) == 0 {
			name = vkv1.DefaultTaskSpec
		}

		pods, found := podsMap[name]
		if !found {
			pods = map[string]*v1.Pod{}
		}

		for i := 0; i < int(ts.Replicas); i++ {
			podName := fmt.Sprintf(TaskNameFmt, job.Name, name, i)
			if pod, found := pods[podName]; !found {
				newPod := createJobPod(job, &ts.Template, i)
				podToCreate = append(podToCreate, newPod)
			} else {
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

		var creationErrs []error
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
		var deletionErrs []error
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
	}

	job.Status = vkv1.JobStatus{
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

	if _, err := cc.vkClients.BatchV1alpha1().Jobs(job.Namespace).Update(job); err != nil {
		glog.Errorf("Failed to update status of Job %v/%v: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return err
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
	inputPVC := fmt.Sprintf("%s-input", job.Name)
	outputPVC := fmt.Sprintf("%s-output", job.Name)
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
