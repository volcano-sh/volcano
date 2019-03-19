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

package helpers

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkcorev1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
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
