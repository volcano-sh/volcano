/*
Copyright 2018 The Kubernetes Authors.

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
	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	vkbatchv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/batch/v1alpha1"
	vkv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/batch/v1alpha1"
	vkcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/bus/v1alpha1"
)

var JobKind = vkbatchv1.SchemeGroupVersion.WithKind("Job")
var CommandKind = vkcorev1.SchemeGroupVersion.WithKind("Command")

func GetController(obj interface{}) types.UID {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return ""
	}

	controllerRef := metav1.GetControllerOf(accessor)
	if controllerRef != nil {
		return controllerRef.UID
	}

	return ""
}

func ControlledBy(obj interface{}, gvk schema.GroupVersionKind) bool {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false
	}

	controllerRef := metav1.GetControllerOf(accessor)
	if controllerRef != nil {
		return controllerRef.Kind == gvk.Kind
	}

	return false
}

func CreateConfigMapIfNotExist(job *vkv1.Job, kubeClients *kubernetes.Clientset, data map[string]string, cmName string) error {
	// If ConfigMap does not exist, create one for Job.
	if _, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Get(cmName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.V(3).Infof("Failed to get Configmap for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}
	}

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: job.Namespace,
			Name:      cmName,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, JobKind),
			},
		},
		Data: data,
	}

	if _, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Create(cm); err != nil {
		glog.V(3).Infof("Failed to create ConfigMap for Job <%s/%s>: %v",
			job.Namespace, job.Name, err)
		return err
	}

	return nil
}

func DeleteConfigmap(job *vkv1.Job, kubeClients *kubernetes.Clientset, cmName string) error {
	if _, err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Get(cmName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.V(3).Infof("Failed to get Configmap for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		} else {
			return nil
		}
	}

	if err := kubeClients.CoreV1().ConfigMaps(job.Namespace).Delete(cmName, nil); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.Errorf("Failed to delete Configmap of Job %v/%v: %v",
				job.Namespace, job.Name, err)
			return err
		}
	}

	return nil
}
