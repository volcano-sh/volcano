/*
Copyright 2025 The Volcano Authors.

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

package topoautodiscovery

import (
	"context"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	topologylister "volcano.sh/apis/pkg/client/listers/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/framework"
	hypernodecache "volcano.sh/volcano/pkg/controllers/networktopologyaware/topoautodiscovery/cache"
)

const (
	networkTopologyAutoDiscoveryControllerName = "network-topo-auto-discovery-controller"
	metadataNameSelector                       = "metadata.name"
)

func init() {
	framework.RegisterController(&networkTopologyAutoDiscoveryController{})
}

type networkTopologyAutoDiscoveryController struct {
	namespace  string
	worker     uint32
	gcWorker   uint32
	syncPeriod time.Duration

	kubeClient              clientset.Interface
	vcClient                vcclientset.Interface
	sharedInformerFactory   informers.SharedInformerFactory
	vcSharedInformerFactory vcinformer.SharedInformerFactory
	hyperNodeLister         topologylister.HyperNodeLister

	cache hypernodecache.Cache
}

func (c *networkTopologyAutoDiscoveryController) Name() string {
	return networkTopologyAutoDiscoveryControllerName
}

func (c *networkTopologyAutoDiscoveryController) Initialize(opt *framework.ControllerOption) error {
	c.kubeClient = opt.KubeClient
	c.vcClient = opt.VolcanoClient
	c.sharedInformerFactory = opt.SharedInformerFactory
	c.vcSharedInformerFactory = opt.VCSharedInformerFactory
	c.namespace = opt.KubePodNamespace
	c.worker = opt.NetworkTopologyAutoDiscoveryWorkerThreads
	c.gcWorker = opt.NetworkTopologyAutoDiscoveryGCWorkerThreads
	c.syncPeriod = opt.NetworkTopologyAutoDiscoverySyncPeriod

	tweakListOptions := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector(metadataNameSelector, hypernodecache.NetworkTopologyAwareConfigMapName).String()
	}
	opt.SharedInformerFactory.InformerFor(&corev1.ConfigMap{},
		func(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			return coreinformers.NewFilteredConfigMapInformer(client, c.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
				tweakListOptions)
		})

	nodeInformer := opt.SharedInformerFactory.Core().V1().Nodes()
	configMapInformer := opt.SharedInformerFactory.Core().V1().ConfigMaps()
	c.hyperNodeLister = opt.VCSharedInformerFactory.Topology().V1alpha1().HyperNodes().Lister()
	c.cache = hypernodecache.NewHyperNodeCache(c.namespace, nodeInformer, configMapInformer, c.hyperNodeLister)
	return nil
}

func (c *networkTopologyAutoDiscoveryController) Run(stopCh <-chan struct{}) {
	klog.Info("Starting network topology autoDiscovery controller")
	defer klog.Info("Shuting down network topology autoDiscovery controller")

	c.sharedInformerFactory.Start(stopCh)
	c.vcSharedInformerFactory.Start(stopCh)
	for informerType, ok := range c.sharedInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Caches failed to sync", "informerType", informerType)
			return
		}
	}
	for informerType, ok := range c.vcSharedInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Caches failed to sync", "informerType", informerType)
			return
		}
	}

	go wait.Until(c.Sync, c.syncPeriod, stopCh)

	<-stopCh
}

func (c *networkTopologyAutoDiscoveryController) Sync() {
	c.cache.OpenSession()
	defer c.cache.CloseSession()

	var wg wait.Group
	defer wg.Wait()
	for i := 0; i < int(c.worker); i++ {
		wg.Start(c.syncWork)
	}
	for i := 0; i < int(c.gcWorker); i++ {
		wg.Start(c.gcWork)
	}
}

func (c *networkTopologyAutoDiscoveryController) syncWork() {
	for {
		hyperNode, done := c.cache.GetHyperNodeToSync()
		if done {
			return
		}

		actualHyperNode, err := c.hyperNodeLister.Get(hyperNode.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.InfoS("Creating hyperNode", "hyperNode", hyperNode.Name)
				_, createErr := c.vcClient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
				if createErr != nil {
					klog.ErrorS(createErr, "Failed to create hyperNode", "hyperNode", hyperNode.Name)
				}
				continue
			}
			klog.ErrorS(err, "Failed to get hyperNode", "hyperNode", hyperNode.Name)
			continue
		}

		hyperNodeUpdate := actualHyperNode.DeepCopy()
		if reflect.DeepEqual(hyperNode.Spec, hyperNodeUpdate.Spec) && reflect.DeepEqual(hyperNode.Labels, hyperNodeUpdate.Labels) {
			continue
		}
		hyperNodeUpdate.Labels = hyperNode.Labels
		hyperNodeUpdate.Spec = hyperNode.Spec
		_, updateErr := c.vcClient.TopologyV1alpha1().HyperNodes().Update(context.TODO(), hyperNodeUpdate, metav1.UpdateOptions{})
		if updateErr != nil {
			klog.ErrorS(updateErr, "Failed to update hyperNode", "hyperNode", hyperNode.Name)
		}
	}
}

func (c *networkTopologyAutoDiscoveryController) gcWork() {
	for {
		hyperNode, done := c.cache.GetHyperNodeToGC()
		if done {
			return
		}

		_, err := c.hyperNodeLister.Get(hyperNode)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			klog.ErrorS(err, "Failed to get hyperNode", "hyperNode", hyperNode)
			continue
		}

		klog.InfoS("Deleting hyperNode", "hyperNode", hyperNode)
		deleteErr := c.vcClient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNode, metav1.DeleteOptions{})
		if deleteErr != nil && !errors.IsNotFound(deleteErr) {
			klog.ErrorS(err, "Failed to delete hyperNode", "hyperNode", hyperNode)
		}
	}
}
