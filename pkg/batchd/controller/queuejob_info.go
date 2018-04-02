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

package controller

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
)

type QueueJobInfo struct {
	Status   string
	Submit   int
	Finish   int
	QueueJob *v1.QueueJob
}

func newQueueJobInfo(qj *v1.QueueJob) *QueueJobInfo {
	return &QueueJobInfo{
		Status:   "",
		Submit:   0,
		Finish:   0,
		QueueJob: qj,
	}
}
