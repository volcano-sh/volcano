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

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corelisters "k8s.io/client-go/listers/core/v1"

	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"

	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
	"hpw.cloud/volcano/pkg/controllers/job/state"
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

func eventKey(obj interface{}) (string, error) {
	req := obj.(*state.Request)

	if req.Pod == nil && req.Job == nil {
		return "", fmt.Errorf("empty data for request")
	}

	if req.Job != nil {
		return fmt.Sprintf("%s/%s", req.Job.Namespace, req.Job.Name), nil
	}

	name, found := req.Pod.Annotations[vkv1.JobNameKey]
	if !found {
		return "", fmt.Errorf("failed to find job of pod <%s/%s>",
			req.Pod.Namespace, req.Pod.Name)
	}
	return fmt.Sprintf("%s/%s", req.Pod.Namespace, name), nil
}

func createJobPod(job *vkv1.Job, template *v1.PodTemplateSpec, ix int) *v1.Pod {
	templateCopy := template.DeepCopy()

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%d", job.Name, template.Name, ix),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, helpers.JobKind),
			},
			Labels:      templateCopy.Labels,
			Annotations: templateCopy.Annotations,
		},
		Spec: templateCopy.Spec,
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
			vm := job.Spec.Output.VolumeMount
			vm.Name = fmt.Sprintf("%s-output", job.Name)
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
			vm := job.Spec.Input.VolumeMount
			vm.Name = fmt.Sprintf("%s-input", job.Name)
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

func getPodsForJob(podLister corelisters.PodLister, job *vkv1.Job) (map[string]map[string]*v1.Pod, error) {
	pods := map[string]map[string]*v1.Pod{}

	// TODO (k82cn): optimist by cache and index of owner; add 'ControlledBy' extended interface.
	ps, err := podLister.Pods(job.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for _, pod := range ps {
		if !metav1.IsControlledBy(pod, job) {
			continue
		}
		if len(pod.Annotations) == 0 {
			glog.Errorf("The annotations of pod <%s/%s> is empty", pod.Namespace, pod.Name)
			continue
		}
		tsName, found := pod.Annotations[vkv1.TaskSpecKey]
		if found {
			// Hash by TaskSpec.Template.Name
			if _, exist := pods[tsName]; !exist {
				pods[tsName] = make(map[string]*v1.Pod)
			}
			pods[tsName][pod.Name] = pod
		}
	}

	return pods, nil
}
