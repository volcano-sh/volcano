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

type QueueJobInfo struct {
	name     string
	queuejob *apiv1.QueueJob
}

func (r *QueueJobInfo) Name() string {
	return r.name
}

func (r *QueueJobInfo) QueueJob() *apiv1.QueueJob {
	return r.queuejob
}

func (r *QueueJobInfo) Clone() *QueueJobInfo {
	clone := &QueueJobInfo{
		name:     r.name,
		queuejob: r.queuejob.DeepCopy(),
	}
	return clone
}
