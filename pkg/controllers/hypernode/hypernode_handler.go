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
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func (hn *hyperNodeController) addHyperNode(obj interface{}) {
	hyperNode, ok := obj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *topologyv1alpha1.HyperNode", "obj", obj)
		return
	}
	klog.V(3).InfoS("Add HyperNode", "name", hyperNode.Name)
	hn.enqueueHyperNode(hyperNode)
}

func (hn *hyperNodeController) updateHyperNode(oldObj, newObj interface{}) {
	hyperNode, ok := newObj.(*topologyv1alpha1.HyperNode)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *topologyv1alpha1.HyperNode", "obj", newObj)
		return
	}
	klog.V(3).InfoS("Update HyperNode", "name", hyperNode.Name)
	hn.enqueueHyperNode(hyperNode)
}

func (hn *hyperNodeController) enqueueHyperNode(hyperNode *topologyv1alpha1.HyperNode) {
	key, err := cache.MetaNamespaceKeyFunc(hyperNode)
	if err != nil {
		klog.ErrorS(err, "Failed to get key for HyperNode", "hyperNode", hyperNode.Name)
		return
	}
	hn.hyperNodeQueue.Add(key)
}

// processHyperNodeQueue processes items from the hyperNode work queue
func (hn *hyperNodeController) processHyperNodeQueue() {
	for {
		key, shutdown := hn.hyperNodeQueue.Get()
		if shutdown {
			klog.InfoS("HyperNode queue has been shut down")
			return
		}

		func() {
			defer hn.hyperNodeQueue.Done(key)
			err := hn.syncHyperNodeStatus(key)
			if err != nil {
				klog.ErrorS(err, "Error syncing HyperNode", "key", key)
				hn.hyperNodeQueue.AddRateLimited(key)
				return
			}

			hn.hyperNodeQueue.Forget(key)
		}()
	}
}

// syncHyperNodeStatus updates the NodeCount field in HyperNode status
func (hn *hyperNodeController) syncHyperNodeStatus(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta namespace key", "key", key)
		return err
	}

	hyperNode, err := hn.hyperNodeLister.Get(name)
	if err != nil {
		// Check if the error happens because the HyperNode is deleted
		if errors.IsNotFound(err) {
			klog.InfoS("HyperNode has been deleted, no status update needed", "name", name)
			return nil
		}
		klog.ErrorS(err, "Failed to get HyperNode", "name", name)
		return err
	}

	nodeCount := len(hyperNode.Spec.Members)
	if hyperNode.Status.NodeCount != int64(nodeCount) {
		// Create a deep copy to avoid modifying cache objects
		hyperNodeCopy := hyperNode.DeepCopy()
		hyperNodeCopy.Status.NodeCount = int64(nodeCount)

		_, err = hn.vcClient.TopologyV1alpha1().HyperNodes().UpdateStatus(context.Background(), hyperNodeCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to update HyperNode status", "name", name)
			return err
		}
		klog.V(3).InfoS("Updated HyperNode status", "name", name, "nodeCount", nodeCount)
	}

	return nil
}
