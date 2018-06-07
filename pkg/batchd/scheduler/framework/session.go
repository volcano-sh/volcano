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

package framework

import (
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/api"
)

type Session struct {
	ID types.UID

	Jobs  []*api.JobInfo
	Nodes []*api.NodeInfo
}

func (ssn *Session) Bind(task *api.TaskInfo, hostname string) error {
	return nil
}

func (ssn *Session) Evict(task *api.TaskInfo) error {
	return nil
}
