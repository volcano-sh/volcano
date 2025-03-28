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

package cache

import (
	"fmt"
	"reflect"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	yaml "sigs.k8s.io/yaml/goyaml.v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	topologylister "volcano.sh/apis/pkg/client/listers/topology/v1alpha1"
)

const (
	LabelBasedTopologyName = "volcano.io/label-based-topology-name"
)

type Cache interface {
	// OpenSession loads configuration from configmap and compare the current Hypernodes in cluster with th configurtion,
	// determine which HyperNodes need to be synchronized or deleted.
	// The resulting HyperNodes are cached
	OpenSession()
	// CloseSession clear all caches
	CloseSession()
	// GetHyperNodeToGC consumes the gc cache
	GetHyperNodeToGC() (hyperNode string, done bool)
	// GetHyperNodeToSync consumes the synced cache
	GetHyperNodeToSync() (hyperNode *topologyv1alpha1.HyperNode, done bool)
}

type HyperNodeCache struct {
	namespace       string
	configMapLister corelister.ConfigMapLister
	nodeLister      corelister.NodeLister
	hyperNodeLister topologylister.HyperNodeLister

	sync.RWMutex
	sessionOpened            bool
	topologyConfigDirty      bool
	nodeLabelBasedTopologies NodeLabelBasedTopologies
	dirtyHyperNodes          sets.Set[string]
	desiredHyperNodes        sets.Set[*topologyv1alpha1.HyperNode]
	hyperNodeToGC            sets.Set[string]
}

func NewHyperNodeCache(namespace string, nodeInformer coreinformers.NodeInformer, configMapInformer coreinformers.ConfigMapInformer,
	hyperNodeLister topologylister.HyperNodeLister) Cache {
	c := &HyperNodeCache{
		namespace:       namespace,
		configMapLister: configMapInformer.Lister(),
		nodeLister:      nodeInformer.Lister(),
		hyperNodeLister: hyperNodeLister,

		dirtyHyperNodes:   sets.New[string](),
		desiredHyperNodes: sets.New[*topologyv1alpha1.HyperNode](),
		hyperNodeToGC:     sets.New[string](),
	}

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNode,
		UpdateFunc: c.updateNode,
		DeleteFunc: c.deleteNode,
	})

	configMapInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: c.updateConfigMap,
		})
	return c
}

func (c *HyperNodeCache) GetNodeLabelBasedTopologies() NodeLabelBasedTopologies {
	c.RLock()
	defer c.RUnlock()

	return c.nodeLabelBasedTopologies
}

func (c *HyperNodeCache) DirtyTopologyConfig() {
	c.Lock()
	defer c.Unlock()

	if !c.sessionOpened || c.topologyConfigDirty {
		return
	}
	c.topologyConfigDirty = true
}

func (c *HyperNodeCache) DirtyHyperNode(name string) {
	if name == "" {
		return
	}
	c.Lock()
	defer c.Unlock()

	if !c.sessionOpened {
		return
	}
	c.dirtyHyperNodes.Insert(name)
}

func (c *HyperNodeCache) DirtyHyperNodesForNode(node *corev1.Node) {
	for key, value := range node.Labels {
		for _, topo := range c.GetNodeLabelBasedTopologies().Topologies {
			for tier, topoKey := range topo.TopoLevelKeys {
				if key == topoKey {
					c.DirtyHyperNode(GetHyperNodeName(topo.Name, value, tier+1))
				}
			}
		}
	}
}

func (c *HyperNodeCache) updateConfigMap(oldObj, newObj interface{}) {
	newConfigMap, ok := newObj.(*corev1.ConfigMap)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", newObj)
		return
	}
	if newConfigMap.Namespace != c.namespace || newConfigMap.Name != NetworkTopologyAwareConfigMapName {
		return
	}

	oldConfigMap, ok := oldObj.(*corev1.ConfigMap)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", oldObj)
		return
	}

	if reflect.DeepEqual(newConfigMap.Data, oldConfigMap.Data) {
		return
	}

	c.DirtyTopologyConfig()
}

func (c *HyperNodeCache) addNode(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", obj)
		return
	}

	c.DirtyHyperNodesForNode(node)
}

func (c *HyperNodeCache) deleteNode(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", obj)
		return
	}

	c.DirtyHyperNodesForNode(node)
}

func (c *HyperNodeCache) updateNode(oldObj, newObj interface{}) {
	newNode, ok := newObj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", newObj)
		return
	}

	oldNode, ok := oldObj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", oldObj)
		return
	}

	if reflect.DeepEqual(newNode.Labels, oldNode.Labels) {
		return
	}

	for _, topo := range c.GetNodeLabelBasedTopologies().Topologies {
		dirtyTiers := make(map[int]bool)
		for index, topoKey := range topo.TopoLevelKeys {
			if newNode.Labels[topoKey] == "" && oldNode.Labels[topoKey] == "" {
				continue
			}

			tier := index + 1
			if newNode.Labels[topoKey] != oldNode.Labels[topoKey] {
				dirtyTiers[tier] = true
				// dirty current tier
				c.DirtyHyperNode(GetHyperNodeName(topo.Name, newNode.Labels[topoKey], tier))
				c.DirtyHyperNode(GetHyperNodeName(topo.Name, oldNode.Labels[topoKey], tier))
				continue
			}
			// changes in the network at lower level may affect current level
			if tier > 1 && dirtyTiers[tier-1] {
				c.DirtyHyperNode(GetHyperNodeName(topo.Name, newNode.Labels[topoKey], tier))
				c.DirtyHyperNode(GetHyperNodeName(topo.Name, oldNode.Labels[topoKey], tier))
			}
		}
	}
}

func (c *HyperNodeCache) OpenSession() {
	c.Lock()
	defer c.Unlock()

	// 1. Load conf from configMap
	config, err := c.loadConfFromConfigMap()
	if err != nil {
		// TODO: gc hyperNode if configMap deleted?
		if errors.IsNotFound(err) {
			klog.V(6).InfoS("Network topology aware configMap doesn't existed", "configMap", NetworkTopologyAwareConfigMapName)
			return
		}
		klog.ErrorS(err, "Failed to get configMap for network topology aware", "configMap", NetworkTopologyAwareConfigMapName)
		return
	}
	c.nodeLabelBasedTopologies = *config

	// 2. List all hyperNode created by topology discovery controller
	actualHyperNodeNames := sets.New[string]()
	hyperNodes, err := c.listLabelKeyBasedHyperNodes()
	if err != nil {
		klog.ErrorS(err, "Failed to list hyperNode for topology")
		return
	}
	for _, hyperNode := range hyperNodes {
		actualHyperNodeNames.Insert(hyperNode.Name)
	}

	// 3. Get desiredHyperNode from node labels
	desiredHyperNodes := make(map[string]*topologyv1alpha1.HyperNode)
	for _, topology := range c.nodeLabelBasedTopologies.Topologies {
		nodes, err := c.listNodesOfTopology(topology.TopoLevelKeys)
		if err != nil {
			klog.ErrorS(err, "Failed to list nodes for topology", "topology", topology.Name)
			return
		}

		for _, node := range nodes {
			ParseDesiredHyperNodesFromNode(node, topology.Name, topology.TopoLevelKeys, desiredHyperNodes)
		}
	}

	desiredHyperNodeName := sets.New[string]()
	for _, hyperNode := range desiredHyperNodes {
		SortHyperNodeMembers(hyperNode)
		c.desiredHyperNodes.Insert(hyperNode)
		desiredHyperNodeName.Insert(hyperNode.Name)
	}
	c.hyperNodeToGC = actualHyperNodeNames.Difference(desiredHyperNodeName)
	c.sessionOpened = true
}

func (c *HyperNodeCache) CloseSession() {
	c.Lock()
	defer c.Unlock()

	c.dirtyHyperNodes.Clear()
	c.desiredHyperNodes.Clear()
	c.hyperNodeToGC.Clear()
	c.sessionOpened = false
	c.topologyConfigDirty = false
}

func (c *HyperNodeCache) GetHyperNodeToSync() (hyperNode *topologyv1alpha1.HyperNode, done bool) {
	c.Lock()
	defer c.Unlock()

	for {
		if c.topologyConfigDirty || !c.sessionOpened {
			return nil, true
		}
		hn, ok := c.desiredHyperNodes.PopAny()
		if !ok {
			return nil, true
		}
		if hn != nil && !c.dirtyHyperNodes.Has(hn.Name) {
			return hn, false
		}
	}
}

func (c *HyperNodeCache) GetHyperNodeToGC() (hyperNode string, done bool) {
	c.Lock()
	defer c.Unlock()

	for {
		if c.topologyConfigDirty || !c.sessionOpened {
			return "", true
		}
		hn, ok := c.hyperNodeToGC.PopAny()
		if !ok {
			return "", true
		}
		if hn != "" && !c.dirtyHyperNodes.Has(hn) {
			return hn, false
		}
	}
}

func (c *HyperNodeCache) loadConfFromConfigMap() (*NodeLabelBasedTopologies, error) {
	configMap, err := c.configMapLister.ConfigMaps(c.namespace).Get(NetworkTopologyAwareConfigMapName)
	if err != nil {
		return nil, err
	}
	data := configMap.Data[NodeLabelBasedTopologiesConfKey]
	conf := &NodeLabelBasedTopologies{}
	if err := yaml.Unmarshal([]byte(data), conf); err != nil {
		return nil, err
	}
	if err := conf.Validate(); err != nil {
		return nil, err
	}
	return conf, nil
}

func (c *HyperNodeCache) listLabelKeyBasedHyperNodes() ([]*topologyv1alpha1.HyperNode, error) {
	labelSelector := labels.NewSelector()
	existRequirement, err := labels.NewRequirement(LabelBasedTopologyName, selection.Exists, nil)
	if err != nil {
		klog.ErrorS(err, "Failed to new requirement from label key ", "topoName", LabelBasedTopologyName)
		return []*topologyv1alpha1.HyperNode{}, fmt.Errorf("failed to new requirement from topoName %s", LabelBasedTopologyName)
	}
	labelSelector.Add(*existRequirement)

	hyperNodes, err := c.hyperNodeLister.List(labelSelector)
	return hyperNodes, err
}

func (c *HyperNodeCache) listNodesOfTopology(topology []string) ([]*corev1.Node, error) {
	labelSelector := labels.NewSelector()
	for _, topoKey := range topology {
		existRequirement, err := labels.NewRequirement(topoKey, selection.Exists, nil)
		if err != nil {
			klog.ErrorS(err, "Failed to new requirement from label key", "topoKey", topoKey)
			return []*corev1.Node{}, fmt.Errorf("failed to new requirement from label key %s", topoKey)
		}
		labelSelector.Add(*existRequirement)
	}
	nodes, err := c.nodeLister.List(labelSelector)
	return nodes, err
}
