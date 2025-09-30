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
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"
	"volcano.sh/volcano/pkg/controllers/hypernode/discovery"
	"volcano.sh/volcano/pkg/controllers/hypernode/utils"
)

func init() {
	framework.RegisterController(&hyperNodeController{})
}

const (
	name = "hyperNode-controller"
)

type hyperNodeController struct {
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

// Run starts the hyperNode controller
func (hn *hyperNodeController) Run(stopCh <-chan struct{}) {
	hn.vcInformerFactory.Start(stopCh)
	hn.informerFactory.Start(stopCh)
	for informerType, ok := range hn.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache: %v", informerType)
			return
		}
	}
	for informerType, ok := range hn.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.ErrorS(nil, "Failed to sync informer cache", "informerType", informerType)
			return
		}
	}

	if err := hn.discoveryManager.Start(); err != nil {
		klog.ErrorS(err, "Failed to start network topology discovery manager")
		return
	}
	go hn.watchDiscoveryResults()

	// Start HyperNode queue processor
	go hn.processHyperNodeQueue()

	klog.InfoS("HyperNode controller started")
	<-stopCh
	hn.discoveryManager.Stop()
	hn.hyperNodeQueue.ShutDown()
	klog.InfoS("HyperNode controller stopped")
}

// Name returns the name of the controller
func (hn *hyperNodeController) Name() string {
	return name
}

// Initialize initializes the hyperNode controller
func (hn *hyperNodeController) Initialize(opt *framework.ControllerOption) error {
	hn.vcClient = opt.VolcanoClient
	hn.kubeClient = opt.KubeClient
	hn.vcInformerFactory = opt.VCSharedInformerFactory
	hn.informerFactory = opt.SharedInformerFactory

	hn.hyperNodeInformer = hn.vcInformerFactory.Topology().V1alpha1().HyperNodes()
	hn.hyperNodeLister = hn.hyperNodeInformer.Lister()
	hn.hyperNodeQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	hn.nodeLister = hn.informerFactory.Core().V1().Nodes().Lister()

	hn.setConfigMapNamespaceAndName()
	hn.setupConfigMapInformer()
	hn.configMapLister = hn.configMapInformer.Lister()
	hn.configMapQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	configLoader := config.NewConfigLoader(
		hn.configMapLister,
		hn.configMapNamespace,
		hn.configMapName,
	)

	hn.discoveryManager = discovery.NewManager(configLoader, hn.configMapQueue, hn.kubeClient, hn.vcClient)

	// Add event handlers for HyperNode
	hn.hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    hn.addHyperNode,
		UpdateFunc: hn.updateHyperNode,
	})

	return nil
}

func (hn *hyperNodeController) watchDiscoveryResults() {
	resultCh := hn.discoveryManager.ResultChannel()
	klog.InfoS("Starting to watch discovery results")
	for result := range resultCh {
		if result.HyperNodes != nil {
			hn.reconcileTopology(result.Source, result.HyperNodes)
			hn.discoveryManager.ResultSynced(result.Source)
		}
	}
	klog.InfoS("Discovery result channel closed")
}

// reconcileTopology reconciles the discovered topology with existing HyperNode resources
func (hn *hyperNodeController) reconcileTopology(source string, discoveredNodes []*topologyv1alpha1.HyperNode) {
	klog.InfoS("Starting topology reconciliation", "source", source, "discoveredNodeCount", len(discoveredNodes))

	existingNodes, err := hn.hyperNodeLister.List(labels.SelectorFromSet(labels.Set{
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
			if err := utils.CreateHyperNode(hn.vcClient, node); err != nil {
				klog.ErrorS(err, "Failed to create HyperNode", "name", name)
			}
		} else {
			klog.InfoS("Updating HyperNode", "name", name, "source", source)
			if err := utils.UpdateHyperNode(hn.vcClient, hn.hyperNodeLister, node); err != nil {
				klog.ErrorS(err, "Failed to update HyperNode", "name", name)
			}
		}

		delete(existingNodeMap, name)
	}

	for name := range existingNodeMap {
		klog.InfoS("Deleting HyperNode", "name", name, "source", source)
		if err := utils.DeleteHyperNode(hn.vcClient, name); err != nil {
			klog.ErrorS(err, "Failed to delete HyperNode", "name", name)
		}
	}

	klog.InfoS("Topology reconciliation completed",
		"source", source,
		"discovered", len(discoveredNodes),
		"created/updated", len(discoveredNodeMap),
		"deleted", len(existingNodeMap))
}
