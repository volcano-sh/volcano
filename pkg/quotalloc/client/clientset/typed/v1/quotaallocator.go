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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/clientset/scheme"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type QuotaAllocatorGetter interface {
	QuotaAllocators(namespaces string) QuotaAllocatorInterface
}

type QuotaAllocatorInterface interface {
	Create(*v1.QuotaAllocator) (*v1.QuotaAllocator, error)
	Update(*v1.QuotaAllocator) (*v1.QuotaAllocator, error)
	UpdateStatus(*v1.QuotaAllocator) (*v1.QuotaAllocator, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.QuotaAllocator, error)
	List(opts meta_v1.ListOptions) (*v1.QuotaAllocatorList, error)
}

// quotaAllocators implements QuotaAllocatorInterface
type quotaAllocators struct {
	client rest.Interface
	ns     string
}

// newQuotaAllocators returns a QuotaAllocators
func newQuotaAllocators(c *ArbV1Client, namespace string) *quotaAllocators {
	return &quotaAllocators{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a quotaAllocator and creates it.  Returns the server's representation of the quotaAllocator, and an error, if there is any.
func (c *quotaAllocators) Create(quotaAllocator *v1.QuotaAllocator) (result *v1.QuotaAllocator, err error) {
	result = &v1.QuotaAllocator{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		Body(quotaAllocator).
		Do().
		Into(result)
	return
}

// Update takes the representation of a quotaAllocator and updates it. Returns the server's representation of the quotaAllocator, and an error, if there is any.
func (c *quotaAllocators) Update(quotaAllocator *v1.QuotaAllocator) (result *v1.QuotaAllocator, err error) {
	result = &v1.QuotaAllocator{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		Name(quotaAllocator.Name).
		Body(quotaAllocator).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *quotaAllocators) UpdateStatus(quotaAllocator *v1.QuotaAllocator) (result *v1.QuotaAllocator, err error) {
	result = &v1.QuotaAllocator{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		Name(quotaAllocator.Name).
		SubResource("status").
		Body(quotaAllocator).
		Do().
		Into(result)
	return
}

// Delete takes name of the quotaAllocator and deletes it. Returns an error if one occurs.
func (c *quotaAllocators) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		Name(name).
		Body(options).
		Do().
		Error()
}

// Get takes name of the quotaAllocator, and returns the corresponding quotaAllocator object, and an error if there is any.
func (c *quotaAllocators) Get(name string, options meta_v1.GetOptions) (result *v1.QuotaAllocator, err error) {
	result = &v1.QuotaAllocator{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of QuotaAllocators that match those selectors.
func (c *quotaAllocators) List(opts meta_v1.ListOptions) (result *v1.QuotaAllocatorList, err error) {
	result = &v1.QuotaAllocatorList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QuotaAllocatorPlural).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}
