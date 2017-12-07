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
	q_v1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	internalinterfaces "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/internalinterfaces"
	v1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/listers/taskset/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	cache "k8s.io/client-go/tools/cache"
	time "time"
)

// TaskSetInformer provides access to a shared informer and lister for
// TaskSet.
type TaskSetInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.TaskSetLister
}

type taskSetInformer struct {
	factory internalinterfaces.SharedInformerFactory
}

// NewTaskSetInformer constructs a new informer for TaskSet type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewTaskSetInformer(client *rest.RESTClient, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {

	source := cache.NewListWatchFromClient(
		client,
		q_v1.TaskSetPlural,
		namespace,
		fields.Everything())

	return cache.NewSharedIndexInformer(
		source,
		&q_v1.TaskSet{},
		resyncPeriod,
		indexers,
	)
}

func defaultTaskSetInformer(client *rest.RESTClient, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewTaskSetInformer(client, meta_v1.NamespaceAll, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func (f *taskSetInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&q_v1.TaskSet{}, defaultTaskSetInformer)
}

func (f *taskSetInformer) Lister() v1.TaskSetLister {
	return v1.NewTaskSetLister(f.Informer().GetIndexer())
}
