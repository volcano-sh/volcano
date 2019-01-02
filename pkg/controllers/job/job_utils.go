/*
Copyright 2018 The Volcano Authors.

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"

	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
)

// getStatus returns no of succeeded and failed pods running a job
func getStatus(pods []*corev1.Pod) (succeeded, failed int32) {
	succeeded = int32(filterPods(pods, corev1.PodSucceeded))
	failed = int32(filterPods(pods, corev1.PodFailed))
	return
}

// filterPods returns pods based on their phase.
func filterPods(pods []*corev1.Pod, phase corev1.PodPhase) int {
	result := 0
	for i := range pods {
		if phase == pods[i].Status.Phase {
			result++
		}
	}
	return result
}

func eventKey(obj interface{}) (string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return "", err
	}

	return string(accessor.GetUID()), nil
}

func createJobPod(qj *vkv1.Job, template *corev1.PodTemplateSpec, ix int32) *corev1.Pod {
	templateCopy := template.DeepCopy()

	prefix := fmt.Sprintf("%s-", qj.Name)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    qj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(qj, helpers.JobKind),
			},
			Labels:      templateCopy.Labels,
			Annotations: templateCopy.Annotations,
		},
		Spec: templateCopy.Spec,
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

	pod.Annotations[kbapi.GroupNameAnnotationKey] = qj.Name

	// we fill the schedulerName in the pod definition with the one specified in the QJ template
	if qj.Spec.SchedulerName != "" && pod.Spec.SchedulerName == "" {
		pod.Spec.SchedulerName = qj.Spec.SchedulerName
	}
	return pod
}

// filterActivePods returns pods that have not terminated.
func filterActivePods(pods []*corev1.Pod) []*corev1.Pod {
	var result []*corev1.Pod
	for _, p := range pods {
		if helpers.IsPodActive(p) {
			result = append(result, p)
		} else {
			glog.V(4).Infof("Ignoring inactive pod %v/%v in state %v, deletion time %v",
				p.Namespace, p.Name, p.Status.Phase, p.DeletionTimestamp)
		}
	}
	return result
}
