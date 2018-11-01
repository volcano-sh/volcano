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

type QueueJobGetter interface {
	QueueJobs(namespaces string) QueueJobInterface
}

type QueueJobInterface interface {
	Create(*v1.QueueJob) (*v1.QueueJob, error)
	Update(*v1.QueueJob) (*v1.QueueJob, error)
	UpdateStatus(*v1.QueueJob) (*v1.QueueJob, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.QueueJob, error)
	List(opts meta_v1.ListOptions) (*v1.QueueJobList, error)
}

// queuejobs implements QueueJobInterface
type queuejobs struct {
	client rest.Interface
	ns     string
}

// newQueueJobs returns a QueueJobs
func newQueueJobs(c *ArbV1Client, namespace string) *queuejobs {
	return &queuejobs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Create takes the representation of a queuejob and creates it.  Returns the server's representation of the queuejob, and an error, if there is any.
func (c *queuejobs) Create(queuejob *v1.QueueJob) (result *v1.QueueJob, err error) {
	result = &v1.QueueJob{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		Body(queuejob).
		Do().
		Into(result)
	return
}

// Update takes the representation of a queuejob and updates it. Returns the server's representation of the queuejob, and an error, if there is any.
func (c *queuejobs) Update(queuejob *v1.QueueJob) (result *v1.QueueJob, err error) {
	result = &v1.QueueJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		Name(queuejob.Name).
		Body(queuejob).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *queuejobs) UpdateStatus(queuejob *v1.QueueJob) (result *v1.QueueJob, err error) {
	result = &v1.QueueJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		Name(queuejob.Name).
		SubResource("status").
		Body(queuejob).
		Do().
		Into(result)
	return
}

// Delete takes name of the queuejob and deletes it. Returns an error if one occurs.
func (c *queuejobs) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		Name(name).
		Body(options).
		Do().
		Error()
}

// Get takes name of the queuejob, and returns the corresponding queuejob object, and an error if there is any.
func (c *queuejobs) Get(name string, options meta_v1.GetOptions) (result *v1.QueueJob, err error) {
	result = &v1.QueueJob{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of QueueJobs that match those selectors.
func (c *queuejobs) List(opts meta_v1.ListOptions) (result *v1.QueueJobList, err error) {
	result = &v1.QueueJobList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource(v1.QueueJobPlural).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}
