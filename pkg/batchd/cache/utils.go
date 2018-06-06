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

package cache

import (
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientcache "k8s.io/client-go/tools/cache"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis"
)

// podKey returns the string key of a pod.
func podKey(pod *v1.Pod) string {
	if key, err := clientcache.MetaNamespaceKeyFunc(pod); err != nil {
		return fmt.Sprintf("%v/%v", pod.Namespace, pod.Name)
	} else {
		return key
	}
}

func getController(obj interface{}) types.UID {
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

func getTaskStatus(pod *v1.Pod) apis.TaskStatus {
	switch pod.Status.Phase {
	case v1.PodRunning:
		return apis.Running
	case v1.PodPending:
		if len(pod.Spec.NodeName) == 0 {
			return apis.Pending
		}
		return apis.Bound
	case v1.PodUnknown:
		return apis.Unknown
	case v1.PodSucceeded:
		return apis.Succeeded
	case v1.PodFailed:
		return apis.Failed
	}

	return apis.Unknown
}
