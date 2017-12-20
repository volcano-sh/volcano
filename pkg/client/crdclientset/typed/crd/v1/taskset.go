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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/crdclientset/scheme"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

type TasksetGetter interface {
	Tasksets(namespaces string) TasksetInterface
}

type TasksetInterface interface {
	Create(*v1.TaskSet) (*v1.TaskSet, error)
	Update(*v1.TaskSet) (*v1.TaskSet, error)
	UpdateStatus(*v1.TaskSet) (*v1.TaskSet, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.TaskSet, error)
	List(opts meta_v1.ListOptions) (*v1.TaskSetList, error)
}

// tasksets implements TasksetInterface
type tasksets struct {
	client rest.Interface
	ns     string
}

// newTasksets returns a Tasksets
func newTasksets(c *CrdV1Client, namespace string) *tasksets {
	return &tasksets{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a taskset and creates it.  Returns the server's representation of the taskset, and an error, if there is any.
func (c *tasksets) Create(taskset *v1.TaskSet) (result *v1.TaskSet, err error) {
	result = &v1.TaskSet{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		Body(taskset).
		Do().
		Into(result)
	return
}

// Update takes the representation of a taskset and updates it. Returns the server's representation of the taskset, and an error, if there is any.
func (c *tasksets) Update(taskset *v1.TaskSet) (result *v1.TaskSet, err error) {
	result = &v1.TaskSet{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		Name(taskset.Name).
		Body(taskset).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *tasksets) UpdateStatus(taskset *v1.TaskSet) (result *v1.TaskSet, err error) {
	result = &v1.TaskSet{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		Name(taskset.Name).
		SubResource("status").
		Body(taskset).
		Do().
		Into(result)
	return
}

// Delete takes name of the taskset and deletes it. Returns an error if one occurs.
func (c *tasksets) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		Name(name).
		Body(options).
		Do().
		Error()
}

// Get takes name of the taskset, and returns the corresponding taskset object, and an error if there is any.
func (c *tasksets) Get(name string, options meta_v1.GetOptions) (result *v1.TaskSet, err error) {
	result = &v1.TaskSet{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Tasksets that match those selectors.
func (c *tasksets) List(opts meta_v1.ListOptions) (result *v1.TaskSetList, err error) {
	result = &v1.TaskSetList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.TaskSetPlural).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}
