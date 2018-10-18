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
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion/internalinterfaces"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/listers/controller/v1"
)

// SchedulingSpecInformer provides access to a shared informer and lister for
// Queues.
type SchedulingSpecInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.SchedulingSpecLister
}

type schedulingSpecInformer struct {
	factory internalinterfaces.SharedInformerFactory
}

// NewSchedulingSpecInformer constructs a new informer for Queue type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewSchedulingSpecInformer(client *rest.RESTClient, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	source := cache.NewListWatchFromClient(
		client,
		arbv1.SchedulingSpecPlural,
		namespace,
		fields.Everything())

	return cache.NewSharedIndexInformer(
		source,
		&arbv1.SchedulingSpec{},
		resyncPeriod,
		indexers,
	)
}

func defaultSchedulingSpecInformer(client *rest.RESTClient, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewSchedulingSpecInformer(client, meta_v1.NamespaceAll,
		resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func (f *schedulingSpecInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&arbv1.SchedulingSpec{}, defaultSchedulingSpecInformer)
}

func (f *schedulingSpecInformer) Lister() v1.SchedulingSpecLister {
	return v1.NewSchedulingSpecLister(f.Informer().GetIndexer())
}
