/*
Copyright 2024 The Volcano Authors.

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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	topologyv1alpha1lister "volcano.sh/apis/pkg/client/listers/topology/v1alpha1"

	"volcano.sh/volcano/pkg/controllers/framework"
	hncache "volcano.sh/volcano/pkg/controllers/hypernode/cache"
)

func init() {
	framework.RegisterController(&hyperNodeController{})
}

type hyperNodeController struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	nodeLister      corelisters.NodeLister
	hyperNodeLister topologyv1alpha1lister.HyperNodeLister

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	nodeQueue      workqueue.RateLimitingInterface
	hyperNodeQueue workqueue.RateLimitingInterface

	hyperNodesCache *hncache.HyperNodesCache
}

func (hnc *hyperNodeController) Name() string {
	return "hypernode-controller"
}

func (hnc *hyperNodeController) Initialize(opt *framework.ControllerOption) error {
	hnc.kubeClient = opt.KubeClient
	hnc.vcClient = opt.VolcanoClient

	hnc.informerFactory = opt.SharedInformerFactory
	hnc.vcInformerFactory = opt.VCSharedInformerFactory

	nodeInformer := hnc.informerFactory.InformerFor(&v1.Node{}, newNodeInformer)
	hnc.nodeLister = corelisters.NewNodeLister(nodeInformer.GetIndexer())
	hyperNodeInformer := hnc.vcInformerFactory.Topology().V1alpha1().HyperNodes()
	hnc.hyperNodeLister = hyperNodeInformer.Lister()

	hnc.nodeQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	hnc.hyperNodeQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    hnc.addNode,
		UpdateFunc: hnc.updateNode,
		DeleteFunc: hnc.deleteNode,
	})

	hnc.hyperNodesCache = hncache.NewHyperNodesCache(cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	))

	hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    hnc.addHyperNode,
		UpdateFunc: hnc.updateHyperNode,
		DeleteFunc: hnc.deleteHyperNode,
	})

	return nil
}

func (hnc *hyperNodeController) Run(stopCh <-chan struct{}) {
	klog.Infof("Starting hypernode controller.")
	defer klog.Infof("Shutting down hypernode controller.")

	hnc.informerFactory.Start(stopCh)

	for informerType, ok := range hnc.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	go wait.Until(hnc.nodeWorker, 0, stopCh)
	go wait.Until(hnc.hyperNodeWorker, 0, stopCh)

	<-stopCh
}

func (hnc *hyperNodeController) nodeWorker() {
	for hnc.processNextNodeEvent() {
	}
}

func (hnc *hyperNodeController) hyperNodeWorker() {
	for hnc.processNextHyperNodeEvent() {
	}
}
