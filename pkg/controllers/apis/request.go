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

	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

// Request struct.
type Request struct {
	Namespace string
	JobName   string
	JobUid    types.UID
	TaskName  string
	QueueName string

	Event      v1alpha1.Event
	ExitCode   int32
	Action     v1alpha1.Action
	JobVersion int32
}

// String function returns the request in string format.
func (r Request) String() string {
	return fmt.Sprintf(
		"Queue: %s, Job: %s/%s, Task:%s, Event:%s, ExitCode:%d, Action:%s, JobVersion: %d",
		r.QueueName, r.Namespace, r.JobName, r.TaskName, r.Event, r.ExitCode, r.Action, r.JobVersion)
}

// FlowRequest The object of sync operation, used for JobFlow and JobTemplate
type FlowRequest struct {
	Namespace       string
	JobFlowName     string
	JobTemplateName string

	Action flowv1alpha1.Action
	Event  flowv1alpha1.Event
}

func (r FlowRequest) String() string {
	return fmt.Sprintf(
		"JobTemplate: %s, JobFlow: %s/%s",
		r.JobTemplateName, r.JobFlowName, r.Namespace)
}
