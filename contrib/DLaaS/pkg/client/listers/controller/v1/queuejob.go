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

// QueueJobLister helps list QueueJobs.
type QueueJobLister interface {
	// List lists all QueueJobs in the indexer.
	List(selector labels.Selector) (ret []*arbv1.QueueJob, err error)
	// QueueJobs returns an object that can list and get QueueJobs.
	QueueJobs(namespace string) QueueJobNamespaceLister
}

// queueJobLister implements the QueueJobLister interface.
type queueJobLister struct {
	indexer cache.Indexer
}

// NewQueueJobLister returns a new QueueJobLister.
func NewQueueJobLister(indexer cache.Indexer) QueueJobLister {
	return &queueJobLister{indexer: indexer}
}

// List lists all QueueJobs in the indexer.
func (s *queueJobLister) List(selector labels.Selector) (ret []*arbv1.QueueJob, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.QueueJob))
	})
	return ret, err
}

// QueueJobs returns an object that can list and get QueueJobs.
func (s *queueJobLister) QueueJobs(namespace string) QueueJobNamespaceLister {
	return queueJobNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// QueueJobNamespaceLister helps list and get QueueJobs.
type QueueJobNamespaceLister interface {
	// List lists all QueueJobs in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*arbv1.QueueJob, err error)
	// Get retrieves the QueueJob from the indexer for a given namespace and name.
	Get(name string) (*arbv1.QueueJob, error)
}

// queueJobNamespaceLister implements the QueueJobNamespaceLister
// interface.
type queueJobNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all QueueJobs in the indexer for a given namespace.
func (s queueJobNamespaceLister) List(selector labels.Selector) (ret []*arbv1.QueueJob, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.QueueJob))
	})
	return ret, err
}

// Get retrieves the QueueJob from the indexer for a given namespace and name.
func (s queueJobNamespaceLister) Get(name string) (*arbv1.QueueJob, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(arbv1.Resource("queuejobs"), name)
	}
	return obj.(*arbv1.QueueJob), nil
}
