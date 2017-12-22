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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/arbclientset/scheme"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

type QueueGetter interface {
	Queues(namespaces string) QueueInterface
}

type QueueInterface interface {
	Create(*v1.Queue) (*v1.Queue, error)
	Update(*v1.Queue) (*v1.Queue, error)
	UpdateStatus(*v1.Queue) (*v1.Queue, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.Queue, error)
	List(opts meta_v1.ListOptions) (*v1.QueueList, error)
}

// queues implements QueueInterface
type queues struct {
	client rest.Interface
	ns     string
}

// newQueues returns a Queues
func newQueues(c *ArbV1Client, namespace string) *queues {
	return &queues{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a queue and creates it.  Returns the server's representation of the queue, and an error, if there is any.
func (c *queues) Create(queue *v1.Queue) (result *v1.Queue, err error) {
	result = &v1.Queue{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		Body(queue).
		Do().
		Into(result)
	return
}

// Update takes the representation of a queue and updates it. Returns the server's representation of the queue, and an error, if there is any.
func (c *queues) Update(queue *v1.Queue) (result *v1.Queue, err error) {
	result = &v1.Queue{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		Name(queue.Name).
		Body(queue).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *queues) UpdateStatus(queue *v1.Queue) (result *v1.Queue, err error) {
	result = &v1.Queue{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		Name(queue.Name).
		SubResource("status").
		Body(queue).
		Do().
		Into(result)
	return
}

// Delete takes name of the queue and deletes it. Returns an error if one occurs.
func (c *queues) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		Name(name).
		Body(options).
		Do().
		Error()
}

// Get takes name of the queue, and returns the corresponding queue object, and an error if there is any.
func (c *queues) Get(name string, options meta_v1.GetOptions) (result *v1.Queue, err error) {
	result = &v1.Queue{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Queues that match those selectors.
func (c *queues) List(opts meta_v1.ListOptions) (result *v1.QueueList, err error) {
	result = &v1.QueueList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QueuePlural).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}
