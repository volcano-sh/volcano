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
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/hypernode/pkg/nodematch"
)

func (c *Controller) addHyperNode(obj interface{}) {
	hyperNode, ok := obj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *topologyv1alpha1.HyperNode", "obj", obj)
		return
	}
	klog.V(3).InfoS("Add HyperNode", "name", hyperNode.Name)
	c.enqueueHyperNode(hyperNode)
}

func (c *Controller) updateHyperNode(oldObj, newObj interface{}) {
	hyperNode, ok := newObj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *topologyv1alpha1.HyperNode", "obj", newObj)
		return
	}
	klog.V(3).InfoS("Update HyperNode", "name", hyperNode.Name)
	c.enqueueHyperNode(hyperNode)
}

func (c *Controller) enqueueHyperNode(hyperNode *topologyv1alpha1.HyperNode) {
	key, err := cache.MetaNamespaceKeyFunc(hyperNode)
	if err != nil {
		klog.ErrorS(err, "Failed to get key for HyperNode", "hyperNode", hyperNode.Name)
		return
	}
	c.hyperNodeQueue.Add(key)
}

func (c *Controller) processHyperNodeQueue() {
	for {
		key, shutdown := c.hyperNodeQueue.Get()
		if shutdown {
			klog.InfoS("HyperNode queue has been shut down")
			return
		}

		func() {
			defer c.hyperNodeQueue.Done(key)
			err := c.syncHyperNodeStatus(key)
			if err != nil {
				klog.ErrorS(err, "Error syncing HyperNode", "key", key)
				c.hyperNodeQueue.AddRateLimited(key)
				return
			}

			c.hyperNodeQueue.Forget(key)
		}()
	}
}

func (c *Controller) syncHyperNodeStatus(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta namespace key", "key", key)
		return err
	}

	hyperNode, err := c.hyperNodeLister.Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.InfoS("HyperNode has been deleted, no status update needed", "name", name)
			return nil
		}
		klog.ErrorS(err, "Failed to get HyperNode", "name", name)
		return err
	}

	nodeCount := c.actualNodeCnt(hyperNode)
	if hyperNode.Status.NodeCount != int64(nodeCount) {
		hyperNodeCopy := hyperNode.DeepCopy()
		hyperNodeCopy.Status.NodeCount = int64(nodeCount)

		_, err = c.vcClient.TopologyV1alpha1().HyperNodes().UpdateStatus(context.Background(), hyperNodeCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to update HyperNode status", "name", name)
			return err
		}
		klog.V(3).InfoS("Updated HyperNode status", "name", name, "nodeCount", nodeCount)
	}

	return nil
}

func (c *Controller) actualNodeCnt(hyperNode *topologyv1alpha1.HyperNode) int {
	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "Failed to list nodes", "name", hyperNode.Name)
		return 0
	}
	memberNames := sets.New[string]()
	for _, member := range hyperNode.Spec.Members {
		memberNames.Insert(nodematch.NodeNamesForSelector(member.Selector, nodes).UnsortedList()...)
	}
	return len(memberNames)
}
