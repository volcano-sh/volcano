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

package hypernode

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
	topologylisterv1alpha1 "volcano.sh/apis/pkg/client/listers/topology/v1alpha1"
	"volcano.sh/hypernode/pkg/api"
	"volcano.sh/hypernode/pkg/config"
	"volcano.sh/hypernode/pkg/discovery"
	"volcano.sh/hypernode/pkg/utils"
)

const controllerName = "hyperNode-controller"

// Controller reconciles HyperNode resources and network topology discovery.
type Controller struct {
	vcClient          vcclientset.Interface
	kubeClient        kubernetes.Interface
	vcInformerFactory vcinformer.SharedInformerFactory
	informerFactory   informers.SharedInformerFactory

	hyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer
	hyperNodeLister   topologylisterv1alpha1.HyperNodeLister
	hyperNodeQueue    workqueue.TypedRateLimitingInterface[string]
	nodeLister        listersv1.NodeLister

	configMapInformer coreinformers.ConfigMapInformer
	configMapLister   listersv1.ConfigMapLister
	configMapQueue    workqueue.TypedRateLimitingInterface[string]

	discoveryManager   discovery.Manager
	configMapNamespace string
	configMapName      string
}

// NewController returns a HyperNode controller instance (not yet initialized).
func NewController() *Controller {
	return &Controller{}
}

// Name matches the controller-manager gate name for hyperNode-controller.
func (c *Controller) Name() string {
	return controllerName
}

// Run starts the HyperNode controller.
func (c *Controller) Run(stopCh <-chan struct{}) {
	c.vcInformerFactory.Start(stopCh)
	c.informerFactory.Start(stopCh)
	for informerType, ok := range c.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache: %v", informerType)
			return
		}
	}
	for informerType, ok := range c.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache", "informerType", informerType)
			return
		}
	}

	if err := c.discoveryManager.Start(); err != nil {
		klog.ErrorS(err, "Failed to start network topology discovery manager")
		return
	}
	go c.watchDiscoveryResults()

	go c.processHyperNodeQueue()

	klog.InfoS("HyperNode controller started")
	<-stopCh
	c.discoveryManager.Stop()
	c.hyperNodeQueue.ShutDown()
	klog.InfoS("HyperNode controller stopped")
}

// Initialize wires clients, informers, and discovery from Options.
func (c *Controller) Initialize(opt *Options) error {
	c.vcClient = opt.VolcanoClient
	c.kubeClient = opt.KubeClient
	c.vcInformerFactory = opt.VCSharedInformerFactory
	c.informerFactory = opt.SharedInformerFactory

	c.hyperNodeInformer = c.vcInformerFactory.Topology().V1alpha1().HyperNodes()
	c.hyperNodeLister = c.hyperNodeInformer.Lister()
	c.hyperNodeQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	c.nodeLister = c.informerFactory.Core().V1().Nodes().Lister()

	c.setConfigMapNamespaceAndName()
	c.setupConfigMapInformer()
	c.configMapLister = c.configMapInformer.Lister()
	c.configMapQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	configLoader := config.NewConfigLoader(
		c.configMapLister,
		c.configMapNamespace,
		c.configMapName,
	)

	c.discoveryManager = discovery.NewManager(configLoader, c.configMapQueue, c.kubeClient, c.vcClient)

	c.hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addHyperNode,
		UpdateFunc: c.updateHyperNode,
	})

	return nil
}

func (c *Controller) watchDiscoveryResults() {
	resultCh := c.discoveryManager.ResultChannel()
	klog.InfoS("Starting to watch discovery results")
	for result := range resultCh {
		if result.HyperNodes != nil {
			c.reconcileTopology(result.Source, result.HyperNodes)
			c.discoveryManager.ResultSynced(result.Source)
		}
	}
	klog.InfoS("Discovery result channel closed")
}

func (c *Controller) reconcileTopology(source string, discoveredNodes []*topologyv1alpha1.HyperNode) {
	klog.InfoS("Starting topology reconciliation", "source", source, "discoveredNodeCount", len(discoveredNodes))

	existingNodes, err := c.hyperNodeLister.List(labels.SelectorFromSet(labels.Set{
		api.NetworkTopologySourceLabelKey: source,
	}))
	if err != nil {
		klog.ErrorS(err, "Failed to list existing HyperNode resources")
		return
	}

	existingNodeMap := make(map[string]*topologyv1alpha1.HyperNode)
	for _, node := range existingNodes {
		existingNodeMap[node.Name] = node
	}

	discoveredNodeMap := make(map[string]*topologyv1alpha1.HyperNode)
	for _, node := range discoveredNodes {
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		node.Labels[api.NetworkTopologySourceLabelKey] = source
		discoveredNodeMap[node.Name] = node
	}

	for name, node := range discoveredNodeMap {
		if _, exists := existingNodeMap[name]; !exists {
			klog.InfoS("Creating new HyperNode", "name", name, "source", source)
			if err := utils.CreateHyperNode(c.vcClient, node); err != nil {
				klog.ErrorS(err, "Failed to create HyperNode", "name", name)
			}
		} else {
			klog.InfoS("Updating HyperNode", "name", name, "source", source)
			if err := utils.UpdateHyperNode(c.vcClient, c.hyperNodeLister, node); err != nil {
				klog.ErrorS(err, "Failed to update HyperNode", "name", name)
			}
		}

		delete(existingNodeMap, name)
	}

	for name := range existingNodeMap {
		klog.InfoS("Deleting HyperNode", "name", name, "source", source)
		if err := utils.DeleteHyperNode(c.vcClient, name); err != nil {
			klog.ErrorS(err, "Failed to delete HyperNode", "name", name)
		}
	}

	klog.InfoS("Topology reconciliation completed",
		"source", source,
		"discovered", len(discoveredNodes),
		"created/updated", len(discoveredNodeMap),
		"deleted", len(existingNodeMap))
}
