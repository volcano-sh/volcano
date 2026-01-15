/*
Copyright 2026 The Volcano Authors.

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

package colocationconfig

import (
	"time"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	configlisters "volcano.sh/apis/pkg/client/listers/config/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func init() {
	framework.RegisterController(&colocationConfigController{})
}

const (
	controllerName = "colocation-config-controller"
)

type colocationConfigController struct {
	kubeClient        kubernetes.Interface
	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	podLister        corelisters.PodLister
	coloConfigLister configlisters.ColocationConfigurationLister

	// podToColoConfig map[string]*configv1alpha1.ColocationConfiguration
	podQueue workqueue.TypedRateLimitingInterface[string]
}

// Name returns the name of the controller.
func (c *colocationConfigController) Name() string {
	return controllerName
}

// Initialize initializes the controller.
func (c *colocationConfigController) Initialize(opt *framework.ControllerOption) error {
	c.kubeClient = opt.KubeClient
	c.informerFactory = opt.SharedInformerFactory
	c.vcInformerFactory = opt.VCSharedInformerFactory

	podInformer := c.informerFactory.Core().V1().Pods()
	c.podLister = podInformer.Lister()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.podHandler,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.podHandler(newObj) // TODO: check whether the matched colocation config name has changed
		},
	})

	coloConfigInformer := c.vcInformerFactory.Config().V1alpha1().ColocationConfigurations()
	c.coloConfigLister = coloConfigInformer.Lister()
	coloConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.colocationConfigurationHandler,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.colocationConfigurationHandler(oldObj) // TODO: check whether the selector has changed
			c.colocationConfigurationHandler(newObj) // TODO: check whether the spec has changed
		},
		DeleteFunc: c.colocationConfigurationHandler,
	})

	c.podQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	return nil
}

// Run starts the controller.
func (c *colocationConfigController) Run(stopCh <-chan struct{}) {
	c.informerFactory.Start(stopCh)
	c.vcInformerFactory.Start(stopCh)
	for informerType, ok := range c.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache", "informerType", informerType)
			return
		}
	}
	for informerType, ok := range c.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache", "informerType", informerType)
			return
		}
	}

	klog.V(3).InfoS("colocationConfigController starting")

	ctx := wait.ContextForChannel(stopCh)
	go func() {
		defer runtime.HandleCrash()
		wait.UntilWithContext(ctx, c.processPodQueue, time.Second)
	}()
	<-ctx.Done()
	c.podQueue.ShutDown()

	klog.V(3).InfoS("colocationConfigController stopped")
}
