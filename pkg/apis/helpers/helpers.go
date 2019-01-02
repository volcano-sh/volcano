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

package helpers

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	vulcanv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

var JobKind = vulcanv1.SchemeGroupVersion.WithKind("Job")

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

func GenerateUUID() string {
	id := uuid.NewUUID()

	return fmt.Sprintf("%s", id)
}

func IsPodActive(p *v1.Pod) bool {
	return v1.PodSucceeded != p.Status.Phase &&
		v1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}
