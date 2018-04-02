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

package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	uuid "github.com/satori/go.uuid"
)

func generateUUID() string {
	id := uuid.NewV1()

	return fmt.Sprintf("%s", id)
}
func queueJobKey(obj interface{}) (string, error) {
	qji, ok := obj.(*QueueJobInfo)
	if !ok {
		return "", fmt.Errorf("not a QueueJob")
	}

	return fmt.Sprintf("%s/%s", qji.QueueJob.Namespace, qji.QueueJob.Name), nil
}

func buildOwnerReference(apiversion, kind, owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: apiversion,
		Kind:       kind,
		Name:       owner,
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func buildPod(n, ns string, template corev1.PodTemplateSpec, owner []metav1.OwnerReference, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
			Labels:          labels,
		},
		Spec: template.Spec,
	}
}
