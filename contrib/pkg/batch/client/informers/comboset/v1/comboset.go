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
	qjob_v1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	v1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/listers/comboset/v1"
	internalinterfaces "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/internalinterfaces"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	cache "k8s.io/client-go/tools/cache"
	time "time"
)

// ComboSetInformer provides access to a shared informer and lister for
// ComboSets.
type ComboSetInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.ComboSetLister
}

type comboSetInformer struct {
	factory internalinterfaces.SharedInformerFactory
}

// NewQueueJobInformer constructs a new informer for ComboSet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewQueueJobInformer(client *rest.RESTClient, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {

	source := cache.NewListWatchFromClient(
		client,
		qjob_v1.ComboSetPlural,
		namespace,
		fields.Everything())

	return cache.NewSharedIndexInformer(
		source,
		&qjob_v1.ComboSet{},
		resyncPeriod,
		indexers,
	)
}

func defaultQueueJobInformer(client *rest.RESTClient, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewQueueJobInformer(client, meta_v1.NamespaceAll, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func (f *comboSetInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&qjob_v1.ComboSet{}, defaultQueueJobInformer)
}

func (f *comboSetInformer) Lister() v1.ComboSetLister {
	return v1.NewComboSetLister(f.Informer().GetIndexer())
}
