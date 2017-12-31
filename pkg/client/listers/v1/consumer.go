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
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ConsumerLister helps list Consumers.
type ConsumerLister interface {
	// List lists all Consumers in the indexer.
	List(selector labels.Selector) (ret []*arbv1.Consumer, err error)
	// Consumers returns an object that can list and get Consumers.
	Consumers(namespace string) ConsumerNamespaceLister
}

// consumerLister implements the ConsumerLister interface.
type consumerLister struct {
	indexer cache.Indexer
}

// NewConsumerLister returns a new ConsumerLister.
func NewConsumerLister(indexer cache.Indexer) ConsumerLister {
	return &consumerLister{indexer: indexer}
}

// List lists all Consumers in the indexer.
func (s *consumerLister) List(selector labels.Selector) (ret []*arbv1.Consumer, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.Consumer))
	})
	return ret, err
}

// Consumers returns an object that can list and get Consumers.
func (s *consumerLister) Consumers(namespace string) ConsumerNamespaceLister {
	return consumerNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ConsumerNamespaceLister helps list and get Consumers.
type ConsumerNamespaceLister interface {
	// List lists all Consumers in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*arbv1.Consumer, err error)
	// Get retrieves the Consumer from the indexer for a given namespace and name.
	Get(name string) (*arbv1.Consumer, error)
}

// consumerNamespaceLister implements the ConsumerNamespaceLister
// interface.
type consumerNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Consumers in the indexer for a given namespace.
func (s consumerNamespaceLister) List(selector labels.Selector) (ret []*arbv1.Consumer, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*arbv1.Consumer))
	})
	return ret, err
}

// Get retrieves the Consumer from the indexer for a given namespace and name.
func (s consumerNamespaceLister) Get(name string) (*arbv1.Consumer, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(arbv1.Resource("queue"), name)
	}
	return obj.(*arbv1.Consumer), nil
}
