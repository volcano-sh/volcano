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
	v1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ComboSetLister helps list ComboJobs.
type ComboSetLister interface {
	// List lists all ComboSets in the indexer.
	List(selector labels.Selector) (ret []*v1.ComboSet, err error)
	// ComboSets returns an object that can list and get ComboSets.
	ComboSets(namespace string) ComboSetNamespaceLister
}

// comboSetLister implements the ComboSetLister interface.
type comboSetLister struct {
	indexer cache.Indexer
}

// NewComboSetLister returns a new ComboJobLister.
func NewComboSetLister(indexer cache.Indexer) ComboSetLister {
	return &comboSetLister{indexer: indexer}
}

// List lists all ComboSets in the indexer.
func (s *comboSetLister) List(selector labels.Selector) (ret []*v1.ComboSet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ComboSet))
	})
	return ret, err
}

// ComboSets returns an object that can list and get ComboSets.
func (s *comboSetLister) ComboSets(namespace string) ComboSetNamespaceLister {
	return comboSetNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ComboSetNamespaceLister helps list and get ComboSets.
type ComboSetNamespaceLister interface {
	// List lists all ComboSets in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.ComboSet, err error)
	// Get retrieves the ComboSet from the indexer for a given namespace and name.
	Get(name string) (*v1.ComboSet, error)
}

// comboSetNamespaceLister implements the ComboSetNamespaceLister
// interface.
type comboSetNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ComboSets in the indexer for a given namespace.
func (s comboSetNamespaceLister) List(selector labels.Selector) (ret []*v1.ComboSet, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ComboSet))
	})
	return ret, err
}

// Get retrieves the ComboSet from the indexer for a given namespace and name.
func (s comboSetNamespaceLister) Get(name string) (*v1.ComboSet, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("comboset"), name)
	}
	return obj.(*v1.ComboSet), nil
}
