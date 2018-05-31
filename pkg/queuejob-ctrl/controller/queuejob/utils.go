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

package queuejob

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/queuejob-ctrl/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/queuejob-ctrl/client"
)

func generateUUID() string {
	id := uuid.NewV1()

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

func queueJobKey(obj interface{}) (string, error) {
	qj, ok := obj.(*arbv1.QueueJob)
	if !ok {
		return "", fmt.Errorf("not a QueueJob")
	}

	return fmt.Sprintf("%s/%s", qj.Namespace, qj.Name), nil
}

func buildPod(n, ns string, template corev1.PodTemplateSpec, owner []metav1.OwnerReference, index int32) *corev1.Pod {
	templateCopy := template.DeepCopy()

	// Add env MPI_INDEX for MPI job
	if index >= 0 {
		indexEnv := corev1.EnvVar{
			Name:  "MPI_INDEX",
			Value: fmt.Sprintf("%d", index),
		}
		for index := range templateCopy.Spec.Containers {
			templateCopy.Spec.Containers[index].Env = append(templateCopy.Spec.Containers[index].Env, indexEnv)
		}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
			Labels:          templateCopy.Labels,
		},
		Spec: templateCopy.Spec,
	}
}

func createQueueJobCRD(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJobCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
