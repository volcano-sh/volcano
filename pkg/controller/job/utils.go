/*
Copyright 2017 The Kubernetes Authors.

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	extv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/extensions/v1alpha1"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/scheduling/v1alpha1"
)

var JobKind = arbv1.SchemeGroupVersion.WithKind("Job")

func generateUUID() string {
	id := uuid.NewUUID()

	return fmt.Sprintf("%s", id)
}

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

func createPodGroup(qj *extv1.Job) *arbv1.PodGroup {
	return &arbv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      qj.Name,
			Namespace: qj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(qj, JobKind),
			},
		},
		Spec: arbv1.PodGroupSpec{
			NumMember: qj.Spec.MinAvailable,
		},
	}
}

func createJobPod(qj *extv1.Job, template *corev1.PodTemplateSpec, ix int32) *corev1.Pod {
	templateCopy := template.DeepCopy()
	prefix := fmt.Sprintf("%s-", qj.Name)

	// Set Pod's GroupName by annotations.
	if len(templateCopy.Annotations) == 0 {
		templateCopy.Annotations = map[string]string{}
	}
	templateCopy.Annotations[arbv1.GroupNameAnnotationKey] = qj.Name

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    qj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(qj, JobKind),
			},
			Labels:      templateCopy.Labels,
			Annotations: templateCopy.Annotations,
		},
		Spec: templateCopy.Spec,
	}
	// we fill the schedulerName in the pod definition with the one specified in the QJ template
	if qj.Spec.SchedulerName != "" && pod.Spec.SchedulerName == "" {
		pod.Spec.SchedulerName = qj.Spec.SchedulerName
	}
	return pod
}
