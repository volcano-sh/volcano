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

package controller

import (
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client"
)

type QueueJobController struct {
	config *rest.Config
}

func NewQueueJobController(config *rest.Config) *QueueJobController {
	cc := &QueueJobController{
		config: config,
	}

	return cc
}

func (cc *QueueJobController) Run(stopCh chan struct{}) {
	// initialized
	cc.createQueueJobCRD()
}

func (cc *QueueJobController) createQueueJobCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(cc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJobCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
