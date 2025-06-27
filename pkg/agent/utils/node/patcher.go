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

package node

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientretry "k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/config"
)

var updateNodeBackoff = wait.Backoff{
	Steps:    20,
	Duration: 50 * time.Millisecond,
	Jitter:   1.0,
}

type Modifier func(*v1.Node)

func update(config *config.Configuration, nodeModifiers []Modifier) error {
	err := clientretry.RetryOnConflict(updateNodeBackoff, func() error {
		curNode, err := config.GetNode()
		if err != nil {
			return err
		}

		newNode := curNode.DeepCopy()
		for _, modify := range nodeModifiers {
			if modify == nil {
				continue
			}
			modify(newNode)
		}
		if needUpdate(curNode, newNode) {
			_, err := config.GenericConfiguration.KubeClient.CoreV1().Nodes().UpdateStatus(context.TODO(), newNode, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func updateNodeOverSoldStatus(res apis.Resource) Modifier {
	return func(node *v1.Node) {
		if node.Status.Allocatable == nil {
			node.Status.Allocatable = make(v1.ResourceList)
		}
		if node.Status.Capacity == nil {
			node.Status.Capacity = make(v1.ResourceList)
		}
		for k, v := range res {
			switch k {
			case v1.ResourceCPU:
				node.Status.Allocatable[apis.GetExtendResourceCPU()] = *resource.NewQuantity(v, resource.DecimalSI)
				node.Status.Capacity[apis.GetExtendResourceCPU()] = *resource.NewQuantity(v, resource.DecimalSI)
			case v1.ResourceMemory:
				node.Status.Allocatable[apis.GetExtendResourceMemory()] = *resource.NewQuantity(v, resource.BinarySI)
				node.Status.Capacity[apis.GetExtendResourceMemory()] = *resource.NewQuantity(v, resource.BinarySI)
			default:
				klog.ErrorS(nil, "Unsupported resource", "resType", k)
			}
		}
	}
}

func deleteNodeOverSoldStatus() Modifier {
	return func(node *v1.Node) {
		delete(node.Status.Capacity, apis.GetExtendResourceCPU())
		delete(node.Status.Capacity, apis.GetExtendResourceMemory())
		delete(node.Status.Allocatable, apis.GetExtendResourceCPU())
		delete(node.Status.Allocatable, apis.GetExtendResourceMemory())
	}
}

func resetOverSubscriptionInfo() []Modifier {
	var resetAnnotation Modifier = func(node *v1.Node) {
		for _, at := range []string{apis.NodeOverSubscriptionCPUKey, apis.NodeOverSubscriptionMemoryKey} {
			if _, ok := node.Annotations[at]; !ok {
				continue
			}
			updateAnnotation(map[string]string{
				at: "0",
			})(node)
		}
	}

	return []Modifier{resetAnnotation, ResetOverSubscriptionLabel()}
}

func DeleteNodeOverSoldStatus(config *config.Configuration) error {
	return update(config, []Modifier{
		ResetOverSubscriptionLabel(),
		deleteNodeOverSoldStatus(),
	})
}

// ResetOverSubNativeResource do following things:
// 1.set overSubscription label=false to disable NodeResourcesProbe and handler(IMPORTANT!)
// 2.set oversold resources = 0 on annotation
func ResetOverSubNativeResource(config *config.Configuration) error {
	return update(config, resetOverSubscriptionInfo())
}

// ResetOverSubscriptionInfoAndRemoveEvictionAnnotation reset overSubscription info and remove eviction annotation.
func ResetOverSubscriptionInfoAndRemoveEvictionAnnotation(config *config.Configuration) error {
	return update(config, append(resetOverSubscriptionInfo(), removeEvictionAnnotation()))
}

// UpdateNodeResourceToAnnotation update oversold resources on annotation.
func UpdateNodeResourceToAnnotation(config *config.Configuration, res apis.Resource) error {
	klog.InfoS("Try to update overSubscription resource on annotation", "resource", res)
	return update(config, []Modifier{
		updateAnnotation(map[string]string{
			apis.NodeOverSubscriptionCPUKey:    fmt.Sprintf("%d", res[v1.ResourceCPU]),
			apis.NodeOverSubscriptionMemoryKey: fmt.Sprintf("%d", res[v1.ResourceMemory]),
		}),
	})
}

// UpdateNodeExtendResource update node extend resource in node status.
func UpdateNodeExtendResource(config *config.Configuration, res apis.Resource) error {
	klog.InfoS("Try to update overSubscription resource on node status", "resource", res)
	return update(config, []Modifier{updateNodeOverSoldStatus(res)})
}

func needUpdate(curNode, newNode *v1.Node) bool {
	return !equality.Semantic.DeepEqual(curNode.Annotations, newNode.Annotations) ||
		!equality.Semantic.DeepEqual(curNode.Labels, newNode.Labels) ||
		!equality.Semantic.DeepEqual(curNode.Status.Allocatable[apis.GetExtendResourceCPU()], newNode.Status.Allocatable[apis.GetExtendResourceCPU()]) ||
		!equality.Semantic.DeepEqual(curNode.Status.Capacity[apis.GetExtendResourceMemory()], newNode.Status.Capacity[apis.GetExtendResourceMemory()]) ||
		!equality.Semantic.DeepEqual(curNode.Spec.Taints, newNode.Spec.Taints)
}
