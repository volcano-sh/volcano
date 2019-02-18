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

package apis

import (
	"fmt"
	"k8s.io/api/core/v1"

	vkbatchv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

type JobInfo struct {
	Job  *vkbatchv1.Job
	Pods map[string]map[string]*v1.Pod
}

type Request struct {
	Namespace string
	JobName   string
	TaskName  string

	Event  vkbatchv1.Event
	Action vkbatchv1.Action
}

func (r Request) String() string {
	return fmt.Sprintf(
		"Job: %s/%s, Task:%s, Event:%s, Action:%s",
		r.Namespace, r.JobName, r.TaskName, r.Event, r.Action)
}
