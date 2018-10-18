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
	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

//XQueueJobLister helps list QueueJobs.
type XQueueJobLister interface {
	// List lists all QueueJobs in the indexer.
	List(selector labels.Selector) (ret []*arbv1.XQueueJob, err error)
	// QueueJobs returns an object that can list and get QueueJobs.
	XQueueJobs(namespace string) XQueueJobNamespaceLister
}

// queueJobLister implements the QueueJobLister interface.
type xqueueJobLister struct {
	indexer cache.Indexer
}

//NewXQueueJobLister returns a new QueueJobLister.
func NewXQueueJobLister(indexer cache.Indexer) XQueueJobLister {
	return &xqueueJobLister{indexer: indexer}
}

//List lists all QueueJobs in the indexer.
func (s *xqueueJobLister) List(selector labels.Selector) (ret []*arbv1.XQueueJob, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.XQueueJob))
	})
	return ret, err
}

//XQueueJobs returns an object that can list and get QueueJobs.
func (s *xqueueJobLister) XQueueJobs(namespace string) XQueueJobNamespaceLister {
	return xqueueJobNamespaceLister{indexer: s.indexer, namespace: namespace}
}

//XQueueJobNamespaceLister helps list and get QueueJobs.
type XQueueJobNamespaceLister interface {
	// List lists all QueueJobs in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*arbv1.XQueueJob, err error)
	// Get retrieves the QueueJob from the indexer for a given namespace and name.
	Get(name string) (*arbv1.XQueueJob, error)
}

// queueJobNamespaceLister implements the QueueJobNamespaceLister
// interface.
type xqueueJobNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all QueueJobs in the indexer for a given namespace.
func (s xqueueJobNamespaceLister) List(selector labels.Selector) (ret []*arbv1.XQueueJob, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.XQueueJob))
	})
	return ret, err
}

// Get retrieves the QueueJob from the indexer for a given namespace and name.
func (s xqueueJobNamespaceLister) Get(name string) (*arbv1.XQueueJob, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(arbv1.Resource("xqueuejobs"), name)
	}
	return obj.(*arbv1.XQueueJob), nil
}
