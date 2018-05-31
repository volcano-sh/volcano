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

package apis

// TaskStatus defines the status of a task/pod.
type TaskStatus int

const (
	// Pending means the task is pending in the apiserver.
	Pending TaskStatus = 1 << iota
	// Binding means the scheduler send Bind request to apiserver.
	Binding
	// Bound means the task/Pod bounds to a host.
	Bound
	// Running means a task is running on the host.
	Running
	// Releasing means a task/pod is deleted.
	Releasing
	// Unknown means the status of task/pod is unknown to the scheduler.
	Unknown
)

type LessFn func(interface{}, interface{}) bool
