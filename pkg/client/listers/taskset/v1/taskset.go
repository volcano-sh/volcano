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
	v1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// TaskSetLister helps list TaskSets.
type TaskSetLister interface {
	// List lists all Queues in the indexer.
	List(selector labels.Selector) (ret []*v1.TaskSet, err error)
	// TaskSets returns an object that can list and get TaskSets.
	TaskSets(namespace string) TaskSetNamespaceLister
}

// taskSetLister implements the TaskSetLister interface.
type taskSetLister struct {
	indexer cache.Indexer
}

// NewTaskSetLister returns a new TaskSetLister.
func NewTaskSetLister(indexer cache.Indexer) TaskSetLister {
	return &taskSetLister{indexer: indexer}
}

// List lists all TaskSets in the indexer.
func (s *taskSetLister) List(selector labels.Selector) (ret []*v1.TaskSet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.TaskSet))
	})
	return ret, err
}

// TaskSets returns an object that can list and get TaskSets.
func (s *taskSetLister) TaskSets(namespace string) TaskSetNamespaceLister {
	return taskSetNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// TaskSetNamespaceLister helps list and get TaskSets.
type TaskSetNamespaceLister interface {
	// List lists all TaskSet in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.TaskSet, err error)
	// Get retrieves the TaskSet from the indexer for a given namespace and name.
	Get(name string) (*v1.TaskSet, error)
}

// taskSetNamespaceLister implements the TaskSetNamespaceLister
// interface.
type taskSetNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all TaskSets in the indexer for a given namespace.
func (s taskSetNamespaceLister) List(selector labels.Selector) (ret []*v1.TaskSet, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.TaskSet))
	})
	return ret, err
}

// Get retrieves the Queue from the indexer for a given namespace and name.
func (s taskSetNamespaceLister) Get(name string) (*v1.TaskSet, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("taskset"), name)
	}
	return obj.(*v1.TaskSet), nil
}
