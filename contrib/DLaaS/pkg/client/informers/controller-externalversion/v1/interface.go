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

package v1

import (
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// SchedulingSpecs returns a SchedulingSpecInformer.
	SchedulingSpecs() SchedulingSpecInformer
	// QueueJobs returns a QueueJobInformer.
	QueueJobs() QueueJobInformer
	XQueueJobs() XQueueJobInformer
}

type version struct {
	internalinterfaces.SharedInformerFactory
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory) Interface {
	return &version{f}
}

// SchedulingSpecs returns a SchedulingSpecInformer.
func (v *version) SchedulingSpecs() SchedulingSpecInformer {
	return &schedulingSpecInformer{factory: v.SharedInformerFactory}
}

// QueueJobs returns a QueueJobInformer.
func (v *version) QueueJobs() QueueJobInformer {
	return &queueJobInformer{factory: v.SharedInformerFactory}
}

func (v *version) XQueueJobs() XQueueJobInformer {
	return &xqueueJobInformer{factory: v.SharedInformerFactory}
}
