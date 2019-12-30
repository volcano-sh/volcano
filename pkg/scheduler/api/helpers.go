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

package api

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	clientcache "k8s.io/client-go/tools/cache"
)

// PodKey returns the string key of a pod.
func PodKey(pod *v1.Pod) TaskID {
	key, err := clientcache.MetaNamespaceKeyFunc(pod)
	if err != nil {
		return TaskID(fmt.Sprintf("%v/%v", pod.Namespace, pod.Name))
	}
	return TaskID(key)
}

func getTaskStatus(pod *v1.Pod) TaskStatus {
	switch pod.Status.Phase {
	case v1.PodRunning:
		if pod.DeletionTimestamp != nil {
			return Releasing
		}

		return Running
	case v1.PodPending:
		if pod.DeletionTimestamp != nil {
			return Releasing
		}

		if len(pod.Spec.NodeName) == 0 {
			return Pending
		}
		return Bound
	case v1.PodUnknown:
		return Unknown
	case v1.PodSucceeded:
		return Succeeded
	case v1.PodFailed:
		return Failed
	}

	return Unknown
}

// AllocatedStatus checks whether the tasks has AllocatedStatus
func AllocatedStatus(status TaskStatus) bool {
	switch status {
	case Bound, Binding, Running, Allocated:
		return true
	default:
		return false
	}
}

// MergeErrors is used to merge multiple errors into single error
func MergeErrors(errs ...error) error {
	msg := "errors: "

	foundErr := false
	i := 1

	for _, e := range errs {
		if e != nil {
			if foundErr {
				msg = fmt.Sprintf("%s, %d: ", msg, i)
			} else {
				msg = fmt.Sprintf("%s %d: ", msg, i)
			}

			msg = fmt.Sprintf("%s%v", msg, e)
			foundErr = true
			i++
		}
	}

	if foundErr {
		return fmt.Errorf("%s", msg)
	}

	return nil
}

// JobTerminated checks whether job was terminated.
func JobTerminated(job *JobInfo) bool {
	return job.PodGroup == nil && len(job.Tasks) == 0
}
