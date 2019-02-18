/*
Copyright 2017 The Volcano Authors.

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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/controllers/job/apis"
)

func validate(job *vkv1.Job) error {
	tsNames := map[string]string{}

	for _, ts := range job.Spec.Tasks {
		if _, found := tsNames[ts.Template.Name]; found {
			return fmt.Errorf("duplicated TaskSpec")
		}

		tsNames[ts.Template.Name] = ts.Template.Name
	}

	return nil
}

func eventKey(obj interface{}) interface{} {
	req, ok := obj.(apis.Request)
	if !ok {
		return obj
	}

	return apis.Request{
		Namespace: req.Namespace,
		JobName:   req.JobName,
	}
}

func MakePodName(jobName string, taskName string, index int) string {
	return fmt.Sprintf(TaskNameFmt, jobName, taskName, index)
}

func createJobPod(job *vkv1.Job, template *v1.PodTemplateSpec, ix int) *v1.Pod {
	templateCopy := template.DeepCopy()

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MakePodName(job.Name, template.Name, ix),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, helpers.JobKind),
			},
			Labels:      templateCopy.Labels,
			Annotations: templateCopy.Annotations,
		},
		Spec: templateCopy.Spec,
	}

	// If no scheduler name in Pod, use scheduler name from Job.
	if len(pod.Spec.SchedulerName) == 0 {
		pod.Spec.SchedulerName = job.Spec.SchedulerName
	}

	if job.Spec.Output != nil {
		if job.Spec.Output.VolumeClaim == nil {
			volume := v1.Volume{
				Name: fmt.Sprintf("%s-output", job.Name),
			}
			volume.EmptyDir = &v1.EmptyDirVolumeSource{}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
		} else {
			volume := v1.Volume{
				Name: fmt.Sprintf("%s-output", job.Name),
			}
			volume.PersistentVolumeClaim = &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: fmt.Sprintf("%s-output", job.Name),
			}

			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
		}

		for i, c := range pod.Spec.Containers {
			vm := v1.VolumeMount{
				MountPath: job.Spec.Output.MountPath,
				Name:      fmt.Sprintf("%s-output", job.Name),
			}
			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
		}
	}

	if job.Spec.Input != nil {
		if job.Spec.Input.VolumeClaim == nil {
			volume := v1.Volume{
				Name: fmt.Sprintf("%s-input", job.Name),
			}
			volume.EmptyDir = &v1.EmptyDirVolumeSource{}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
		} else {
			volume := v1.Volume{
				Name: fmt.Sprintf("%s-input", job.Name),
			}
			volume.PersistentVolumeClaim = &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: fmt.Sprintf("%s-input", job.Name),
			}

			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
		}

		for i, c := range pod.Spec.Containers {
			vm := v1.VolumeMount{
				MountPath: job.Spec.Input.MountPath,
				Name:      fmt.Sprintf("%s-input", job.Name),
			}

			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)

		}
	}

	if len(pod.Annotations) == 0 {
		pod.Annotations = make(map[string]string)
	}

	tsKey := templateCopy.Name
	if len(tsKey) == 0 {
		tsKey = vkv1.DefaultTaskSpec
	}
	pod.Annotations[vkv1.TaskSpecKey] = tsKey

	if len(pod.Annotations) == 0 {
		pod.Annotations = make(map[string]string)
	}

	pod.Annotations[kbapi.GroupNameAnnotationKey] = job.Name
	pod.Annotations[vkv1.JobNameKey] = job.Name

	if len(pod.Labels) == 0 {
		pod.Labels = make(map[string]string)
	}

	// Set pod labels for Service.
	pod.Labels[vkv1.JobNameKey] = job.Name
	pod.Labels[vkv1.JobNamespaceKey] = job.Namespace

	// we fill the schedulerName in the pod definition with the one specified in the QJ template
	if job.Spec.SchedulerName != "" && pod.Spec.SchedulerName == "" {
		pod.Spec.SchedulerName = job.Spec.SchedulerName
	}

	return pod
}

func applyPolicies(job *vkv1.Job, req *apis.Request) vkv1.Action {
	if len(req.Action) != 0 {
		return req.Action
	}

	if req.Event == vkv1.OutOfSyncEvent {
		return vkv1.SyncJobAction
	}

	// Overwrite Job level policies
	if len(req.TaskName) != 0 {
		// Parse task level policies
		for _, task := range job.Spec.Tasks {
			if task.Name == req.TaskName {
				for _, policy := range task.Policies {
					if policy.Event == req.Event || policy.Event == vkv1.AnyEvent {
						return policy.Action
					}
				}
			}
		}
	}

	// Parse Job level policies
	for _, policy := range job.Spec.Policies {
		if policy.Event == req.Event || policy.Event == vkv1.AnyEvent {
			return policy.Action
		}
	}

	return vkv1.SyncJobAction
}
