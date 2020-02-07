/*
Copyright 2020 The Volcano Authors.

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

package router

import (
	"fmt"
	"reflect"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/internalclientset"
	crdinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/internalversion/apiextensions/internalversion"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	schedulingv1alpha1 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	schedulingv1alpha2 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
)

type CRDController struct {
	crdInformer crdinformers.CustomResourceDefinitionInformer
	crdSynced   cache.InformerSynced
	crdClient   *crdclientset.Clientset

	webhookConfigs map[string]*apiextensions.WebhookClientConfig

	// queue is where incoming work is placed to de-dup and to allow "easy" rate limited requeues on errors
	// this is actually keyed by a groupVersion
	queue workqueue.RateLimitingInterface
}

func NewController(crdinformer crdinformers.CustomResourceDefinitionInformer, clientset *crdclientset.Clientset) *CRDController {
	c := &CRDController{
		crdInformer: crdinformer,
		crdSynced:   crdinformer.Informer().HasSynced,
		crdClient:   clientset,

		webhookConfigs: make(map[string]*apiextensions.WebhookClientConfig),

		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "crd-conversion-controller"),
	}

	crdinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cast := obj.(*apiextensions.CustomResourceDefinition)
			c.queue.Add(cast.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			cast := newObj.(*apiextensions.CustomResourceDefinition)
			c.queue.Add(cast.Name)
		},
		DeleteFunc: func(obj interface{}) {
			cast, ok := obj.(*apiextensions.CustomResourceDefinition)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.V(2).Infof("Couldn't get object from tombstone %#v", obj)
					return
				}
				cast, ok = tombstone.Obj.(*apiextensions.CustomResourceDefinition)
				if !ok {
					klog.V(2).Infof("Tombstone contained unexpected object: %#v", obj)
					return
				}
			}

			c.queue.Add(cast.Name)
		},
	})

	return c
}

func (c *CRDController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	// make sure the work queue is shutdown which will trigger workers to end
	defer c.queue.ShutDown()

	cache.WaitForCacheSync(stopCh, c.crdSynced)

	go wait.Until(c.runWorker, 0, stopCh)

	// wait until we're told to stop
	<-stopCh
}

func (c *CRDController) runWorker() {
	// hot loop until we're told to stop.  processNextWorkItem will automatically wait until there's work
	// available, so we don't worry about secondary waits
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem deals with one key off the queue.  It returns false when it's time to quit.
func (c *CRDController) processNextWorkItem() bool {
	// pull the next work item from queue.  It should be a key we use to lookup something in a cache
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	// you always have to indicate to the queue that you've completed a piece of work
	defer c.queue.Done(key)

	// do your work on the key.  This method will contains your "do stuff" logic
	err := c.syncHandler(key.(string))
	if err == nil {
		// if you had no error, tell the queue to stop tracking history for your key.  This will
		// reset things like failure counts for per-item rate limiting
		c.queue.Forget(key)
		return true
	}

	// there was a failure so be sure to report it.  This method allows for pluggable error handling
	// which can be used for things like cluster-monitoring
	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", key, err))
	// since we failed, we should requeue the item to work on later.  This method will add a backoff
	// to avoid hotlooping on particular items (they're probably still not going to work right away)
	// and overall controller protection (everything I've done is broken, this controller needs to
	// calm down or it can starve other useful work) cases.
	c.queue.AddRateLimited(key)

	return true
}

func (c *CRDController) RegisterWebhookConfig(name string, config *apiextensions.WebhookClientConfig) {
	c.webhookConfigs[name] = config
}

func (c *CRDController) syncHandler(name string) error {
	klog.V(3).Infof("Handling CRD %s.", name)

	crd, err := c.crdClient.Apiextensions().CustomResourceDefinitions().Get(name, v1.GetOptions{})

	if err != nil {
		klog.Errorf("Failed to get CRD by name %s.", name)
		return err
	}

	config, found := c.webhookConfigs[name]
	if !found {
		klog.V(3).Infof("No webhook config for %s.", name)
		return nil
	}

	conv := &apiextensions.CustomResourceConversion{
		Strategy:            apiextensions.WebhookConverter,
		WebhookClientConfig: config,
	}

	if reflect.DeepEqual(conv, crd.Spec.Conversion) {
		return nil
	}

	FALSE := false
	crd.Spec.PreserveUnknownFields = &FALSE
	crd.Spec.Conversion = conv
	// TODO(@k82cn): Move version into ConversionService when conversion is ready.
	crd.Spec.Conversion.ConversionReviewVersions = []string{
		schedulingv1alpha1.GroupVersion,
		schedulingv1alpha2.GroupVersion,
		"v1beta1",
		"v1",
	}

	_, err = c.crdClient.Apiextensions().CustomResourceDefinitions().Update(crd)
	if err != nil {
		klog.Errorf("Failed to update CRD %s: %v", name, err)
	}
	return err
}
