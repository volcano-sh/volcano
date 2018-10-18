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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

type ArbV1Interface interface {
	RESTClient() rest.Interface
	SchedulingSpecGetter
	QueueJobGetter
	XQueueJobGetter
}

// ArbV1Client is used to interact with features provided by the  group.
type ArbV1Client struct {
	restClient rest.Interface
}

func (c *ArbV1Client) SchedulingSpecs(namespace string) SchedulingSpecInterface {
	return newSchedulingSpecs(c, namespace)
}

func (c *ArbV1Client) QueueJobs(namespace string) QueueJobInterface {
	return newQueueJobs(c, namespace)
}

func (c *ArbV1Client) XQueueJobs(namespace string) XQueueJobInterface {
	return newXQueueJobs(c, namespace)
}

// NewForConfig creates a new ArbV1Client for the given config.
func NewForConfig(c *rest.Config) (*ArbV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &ArbV1Client{client}, nil
}

// NewForConfigOrDie creates a new ArbV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *ArbV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ArbV1Client for the given RESTClient.
func New(c rest.Interface) *ArbV1Client {
	return &ArbV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		return err
	}

	config.GroupVersion = &v1.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *ArbV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
