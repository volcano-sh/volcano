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

package cache

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/validation"
)

func UpdateStatus(task *TaskInfo, status apis.TaskStatus) error {
	if err := validation.ValidateStatusUpdate(status, status); err != nil {
		return err
	}

	return nil
}
