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

// SchedulingSpecLister helps list Queues.
type SchedulingSpecLister interface {
	// List lists all Queues in the indexer.
	List(selector labels.Selector) (ret []*arbv1.SchedulingSpec, err error)
	// SchedulingSpecs returns an object that can list and get Queues.
	SchedulingSpecs(namespace string) SchedulingSpecNamespaceLister
}

// queueLister implements the QueueLister interface.
type schedulingSpecLister struct {
	indexer cache.Indexer
}

// NewSchedulingSpecLister returns a new QueueLister.
func NewSchedulingSpecLister(indexer cache.Indexer) SchedulingSpecLister {
	return &schedulingSpecLister{indexer: indexer}
}

// List lists all Queues in the indexer.
func (s *schedulingSpecLister) List(selector labels.Selector) (ret []*arbv1.SchedulingSpec, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.SchedulingSpec))
	})
	return ret, err
}

// Queues returns an object that can list and get Queues.
func (s *schedulingSpecLister) SchedulingSpecs(namespace string) SchedulingSpecNamespaceLister {
	return schedulingSpecNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// SchedulingSpecNamespaceLister helps list and get Queues.
type SchedulingSpecNamespaceLister interface {
	// List lists all Queues in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*arbv1.SchedulingSpec, err error)
	// Get retrieves the Queue from the indexer for a given namespace and name.
	Get(name string) (*arbv1.SchedulingSpec, error)
}

// schedulingSpecNamespaceLister implements the QueueNamespaceLister
// interface.
type schedulingSpecNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Queues in the indexer for a given namespace.
func (s schedulingSpecNamespaceLister) List(selector labels.Selector) (ret []*arbv1.SchedulingSpec, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.SchedulingSpec))
	})
	return ret, err
}

// Get retrieves the Queue from the indexer for a given namespace and name.
func (s schedulingSpecNamespaceLister) Get(name string) (*arbv1.SchedulingSpec, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(arbv1.Resource("schedulingspecs"), name)
	}
	return obj.(*arbv1.SchedulingSpec), nil
}
