/*
Copyright 2025 The Volcano Authors.

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

package helpers

import (
	"context"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
)

// CreateOrUpdateConfigMap creates or updates a ConfigMap with the given data, labels, and annotations
func CreateOrUpdateConfigMap(job *batch.Job, kubeClient kubernetes.Interface, data map[string]string, name string, labels map[string]string, annotations map[string]string) error {
	// Try to get existing ConfigMap
	configMap, err := kubeClient.CoreV1().ConfigMaps(job.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get ConfigMap for Job <%s/%s>: %v", job.Namespace, job.Name, err)
			return err
		}

		// ConfigMap doesn't exist, create it
		configMap = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   job.Namespace,
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Data: data,
		}

		if _, e := kubeClient.CoreV1().ConfigMaps(job.Namespace).Create(context.TODO(), configMap, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create ConfigMap for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		klog.V(3).Infof("Created ConfigMap <%s/%s> for Job <%s/%s>", job.Namespace, name, job.Namespace, job.Name)
		return nil
	}

	// ConfigMap exists, update it
	configMap.Data = data
	if labels != nil {
		configMap.Labels = labels
	}
	if annotations != nil {
		configMap.Annotations = annotations
	}

	if _, e := kubeClient.CoreV1().ConfigMaps(job.Namespace).Update(context.TODO(), configMap, metav1.UpdateOptions{}); e != nil {
		klog.V(3).Infof("Failed to update ConfigMap for Job <%s/%s>: %v", job.Namespace, job.Name, e)
		return e
	}
	klog.V(3).Infof("Updated ConfigMap <%s/%s> for Job <%s/%s>", job.Namespace, name, job.Namespace, job.Name)
	return nil
}
