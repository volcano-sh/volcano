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
	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/clientset"
)

type QueueController struct {
	config     *rest.Config
	arbclient  *clientset.Clientset
	nsInformer v1.NamespaceInformer
}

func NewQueueController(config *rest.Config) *QueueController {
	cc := &QueueController{
		config:    config,
		arbclient: clientset.NewForConfigOrDie(config),
	}

	kubecli := kubernetes.NewForConfigOrDie(config)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	// create informer for node information
	cc.nsInformer = informerFactory.Core().V1().Namespaces()
	cc.nsInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc: cc.addNamespace,
			//UpdateFunc: cc.UpdateNamespace,
			DeleteFunc: cc.deleteNamespace,
		},
		0,
	)

	return cc
}

func (cc *QueueController) Run(stopCh chan struct{}) {
	// initialized
	cc.createQueueCRD()

	go cc.nsInformer.Informer().Run(stopCh)
	cache.WaitForCacheSync(stopCh, cc.nsInformer.Informer().HasSynced)
}

func (cc *QueueController) createQueueCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(cc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (cc *QueueController) addNamespace(obj interface{}) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}

	c := &arbv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ns.Name,
			Namespace: ns.Name,
		},
	}

	if _, err := cc.arbclient.ArbV1().Queues(ns.Name).Create(c); err != nil {
		glog.V(3).Infof("Create Queue <%v/%v> successfully.", c.Name, c.Name)
	}
}

func (cc *QueueController) deleteNamespace(obj interface{}) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}

	err := cc.arbclient.ArbV1().Queues(ns.Name).Delete(ns.Name, &metav1.DeleteOptions{})
	if err != nil {
		glog.V(3).Infof("Failed to delete Queue <%s>", ns.Name)
	}
}
