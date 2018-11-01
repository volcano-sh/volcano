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
	v1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned/scheme"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type SchedulingSpecGetter interface {
	SchedulingSpecs(namespaces string) SchedulingSpecInterface
}

type SchedulingSpecInterface interface {
	Create(*v1.SchedulingSpec) (*v1.SchedulingSpec, error)
	Update(*v1.SchedulingSpec) (*v1.SchedulingSpec, error)
	UpdateStatus(*v1.SchedulingSpec) (*v1.SchedulingSpec, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.SchedulingSpec, error)
	List(opts meta_v1.ListOptions) (*v1.SchedulingSpecList, error)
}

// schedulingSpecs implements SchedulingSpecInterface
type schedulingSpecs struct {
	client rest.Interface
	ns     string
}

// newQueues returns a Queues
func newSchedulingSpecs(c *ArbV1Client, namespace string) *schedulingSpecs {
	return &schedulingSpecs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a queue and creates it.  Returns the server's representation of the queue, and an error, if there is any.
func (c *schedulingSpecs) Create(queue *v1.SchedulingSpec) (result *v1.SchedulingSpec, err error) {
	result = &v1.SchedulingSpec{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		Body(queue).
		Do().
		Into(result)
	return
}

// Update takes the representation of a queue and updates it. Returns the server's representation of the queue, and an error, if there is any.
func (c *schedulingSpecs) Update(queue *v1.SchedulingSpec) (result *v1.SchedulingSpec, err error) {
	result = &v1.SchedulingSpec{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		Name(queue.Name).
		Body(queue).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *schedulingSpecs) UpdateStatus(queue *v1.SchedulingSpec) (result *v1.SchedulingSpec, err error) {
	result = &v1.SchedulingSpec{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		Name(queue.Name).
		SubResource("status").
		Body(queue).
		Do().
		Into(result)
	return
}

// Delete takes name of the queue and deletes it. Returns an error if one occurs.
func (c *schedulingSpecs) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		Name(name).
		Body(options).
		Do().
		Error()
}

// Get takes name of the queue, and returns the corresponding queue object, and an error if there is any.
func (c *schedulingSpecs) Get(name string, options meta_v1.GetOptions) (result *v1.SchedulingSpec, err error) {
	result = &v1.SchedulingSpec{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Queues that match those selectors.
func (c *schedulingSpecs) List(opts meta_v1.ListOptions) (result *v1.SchedulingSpecList, err error) {
	result = &v1.SchedulingSpecList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.SchedulingSpecPlural).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}
