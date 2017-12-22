/*
Copyright 2014 The Kubernetes Authors.

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

package combosetresources

import (
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

// Interface is an abstract interface for queue job resource management.
type Interface interface {
	GetResourceAllocated() *qjobv1.ResourceList
	GetResourceRequest() *schedulercache.Resource

	SetResourceAllocated(qjobv1.ResourceList) error
	Sync(queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error
	Cleanup(queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error
	Run(stopCh <-chan struct{})
}
