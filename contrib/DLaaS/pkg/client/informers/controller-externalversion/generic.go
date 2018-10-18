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

package controller_externalversion

import (
	"fmt"

	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// GenericInformer is type of SharedIndexInformer which will locate and delegate to other
// sharedInformers based on type
type GenericInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() cache.GenericLister
}

type genericInformer struct {
	informer cache.SharedIndexInformer
	resource schema.GroupResource
}

// Informer returns the SharedIndexInformer.
func (f *genericInformer) Informer() cache.SharedIndexInformer {
	return f.informer
}

// Lister returns the GenericLister.
func (f *genericInformer) Lister() cache.GenericLister {
	return cache.NewGenericLister(f.Informer().GetIndexer(), f.resource)
}

// ForResource gives generic access to a shared informer of the matching type
// TODO extend this to unknown resources with a client pool
func (f *sharedInformerFactory) ForResource(resource schema.GroupVersionResource) (GenericInformer, error) {
	switch resource {
	// Group=, Version=V1
	case arbv1.SchemeGroupVersion.WithResource("schedulingspecs"):
		return &genericInformer{
			resource: resource.GroupResource(),
			informer: f.SchedulingSpec().SchedulingSpecs().Informer(),
		}, nil
	case arbv1.SchemeGroupVersion.WithResource("queuejobs"):
		return &genericInformer{
			resource: resource.GroupResource(),
			informer: f.QueueJob().QueueJobs().Informer(),
		}, nil
	case arbv1.SchemeGroupVersion.WithResource("xqueuejobs"):
		return &genericInformer{
			resource: resource.GroupResource(),
			informer: f.XQueueJob().XQueueJobs().Informer(),
		}, nil
	}

	return nil, fmt.Errorf("no informer found for %v", resource)
}
