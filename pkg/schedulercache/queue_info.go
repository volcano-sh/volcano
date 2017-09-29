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

package schedulercache

import (
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type QueueInfo struct {
	name  string
	queue *apiv1.Queue
}

func (r *QueueInfo) Name() string {
	return r.name
}

func (r *QueueInfo) Queue() *apiv1.Queue {
	return r.queue
}

func (r *QueueInfo) Clone() *QueueInfo {
	clone := &QueueInfo{
		name:  r.name,
		queue: r.queue.DeepCopy(),
	}
	return clone
}
