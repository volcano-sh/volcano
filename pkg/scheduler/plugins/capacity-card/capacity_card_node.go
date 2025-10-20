/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// CardInfo defines the basic information of a card.
// One node only has one type of card.
type CardInfo struct {
	Name           string // card name
	Count          int64  // card count in board slots(PCIE/SXM,etc.)
	Memory         int64  // card memory in bytes
	ResourcePrefix string // resource prefix, e.g. nvidia.com
}

// NodeCardResourceInfo defines the card resource information of a node.
type NodeCardResourceInfo struct {
	// card basic info in node labels, created by gpu operator or manually.
	CardInfo CardInfo
	// actual card resources in node status.capacity, created by device plugin.
	CardResource corev1.ResourceList
	// mapping from card name to resource name, for quick indexing in scheduler logic.
	CardNameToResourceName map[corev1.ResourceName]corev1.ResourceName
}

var (
	// nodeCardProductLabelRegex is used to extract card product information from node labels.
	// car product label is necessary to get card name, count and memory.
	nodeCardProductLabelRegex = regexp.MustCompile(`^((.+?)/(\w+))\.product$`)
)

// buildTotalResource builds the total resource of the cluster by listing all nodes from informer.
// Note that, DO NOT use ssn.Nodes where, because ssn.Nodes are synced in node event handlers asynchronously,
// which might lost some nodes in scheduling starting to work.
func (p *Plugin) buildTotalResource(ssn *framework.Session) bool {
	p.nodeLister = ssn.InformerFactory().Core().V1().Nodes().Lister()
	nodes, err := p.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list nodes: %+v", err)
		return false
	}
	p.buildTotalResourceFromNodes(nodes)
	return true
}

// buildTotalResourceFromNodes builds the total resource of the cluster from the given nodes.
func (p *Plugin) buildTotalResourceFromNodes(nodes []*corev1.Node) {
	var (
		totalNormalResource = make(corev1.ResourceList) // CPU, Memory, EphemeralStorage, etc.
		totalCardResource   = make(corev1.ResourceList) // GPU/NPU/PPU cards, etc.
	)
	for _, node := range nodes {
		// calculate cpu and memory resource
		cpuMemoryResource := make(corev1.ResourceList)
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			cpuMemoryResource[corev1.ResourceCPU] = cpu
		}
		if memory, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			cpuMemoryResource[corev1.ResourceMemory] = memory
		}
		addResourceList(
			totalNormalResource, cpuMemoryResource,
		)

		// calculate card resource
		nodeCardInfo := p.getCardResourceFromNode(node)
		addResourceList(totalCardResource, nodeCardInfo.CardResource)
		for cardName, resourceName := range nodeCardInfo.CardNameToResourceName {
			p.cardNameToResourceName[cardName] = resourceName
		}
	}
	p.totalResource = api.NewResource(totalNormalResource)
	for resName, quantity := range totalCardResource {
		p.totalResource.AddScalar(resName, float64(quantity.Value()*cardCountQuantityMultiplier))
	}
}

// getCardResourceFromNode gets the card resource from the node.
func (p *Plugin) getCardResourceFromNode(node *corev1.Node) NodeCardResourceInfo {
	if nodeCardInfo, ok := p.nodeCardInfos[node.Name]; ok {
		return nodeCardInfo
	}
	nodeCardInfo := NodeCardResourceInfo{
		CardInfo:               p.getCardInfoFromNode(node),
		CardResource:           map[corev1.ResourceName]resource.Quantity{},
		CardNameToResourceName: map[corev1.ResourceName]corev1.ResourceName{},
	}
	for resName, cardCapacity := range node.Status.Allocatable {
		if cardCapacity.Value() <= 0 {
			continue
		}
		// special MPS resource.
		if isMpsResourceName(resName) {
			mpsReplicas, err := strconv.Atoi(node.Labels[MpsReplicaLabel])
			if err != nil {
				klog.Errorf(
					"Failed to parse MPS replica label %s: %+v",
					node.Labels[MpsReplicaLabel], err,
				)
			}
			if mpsReplicas > 0 {
				cardName := fmt.Sprintf(
					MpsSharedCardNamePattern,
					nodeCardInfo.CardInfo.Name,
					int(math.Round(float64(nodeCardInfo.CardInfo.Memory)/1024)),
					mpsReplicas,
				)
				cardResourceName := corev1.ResourceName(cardName)
				nodeCardInfo.CardResource[cardResourceName] = cardCapacity.DeepCopy()
				nodeCardInfo.CardNameToResourceName[cardResourceName] = resName
			}
			continue
		}

		// special MIG resource.
		if isMigResourceName(resName) {
			var (
				migSpec          = strings.TrimPrefix(string(resName), MigLabelAndResourceNamePrefix)
				cardName         = fmt.Sprintf(MigSharedCardNamePattern, nodeCardInfo.CardInfo.Name, migSpec)
				cardResourceName = corev1.ResourceName(cardName)
			)
			nodeCardInfo.CardResource[cardResourceName] = cardCapacity.DeepCopy()
			nodeCardInfo.CardNameToResourceName[cardResourceName] = resName
			continue
		}

		// 1. whole card resource
		// 2. parts are shared card resource, parts are whole card resource
		if nodeCardInfo.CardInfo.ResourcePrefix != "" {
			if strings.HasPrefix(string(resName), nodeCardInfo.CardInfo.ResourcePrefix) && cardCapacity.Value() > 0 {
				cardResourceName := corev1.ResourceName(nodeCardInfo.CardInfo.Name)
				nodeCardInfo.CardResource[cardResourceName] = cardCapacity.DeepCopy()
				nodeCardInfo.CardNameToResourceName[cardResourceName] = resName
				continue
			}
		}
	}

	if nodeCardInfo.CardInfo.Name != "" && len(nodeCardInfo.CardResource) == 0 {
		klog.Warningf("Node <%s> has card <%s> but no card resource found.",
			node.Name,
			nodeCardInfo.CardInfo.Name,
		)
	}

	p.nodeCardInfos[node.Name] = nodeCardInfo
	return nodeCardInfo
}

// getCardInfoFromNode gets the card basic information from the node labels.
func (p *Plugin) getCardInfoFromNode(node *corev1.Node) CardInfo {
	for k, v := range node.Labels {
		if strings.Contains(k, MigLabelAndResourceNamePrefix) {
			continue
		}
		match := nodeCardProductLabelRegex.FindStringSubmatch(k)
		if len(match) == 4 {
			var (
				labelPrefix    = match[1]                              // eg: nvidia.com/gpu
				resourcePrefix = match[2]                              // eg: nvidia.com
				countLabel     = fmt.Sprintf("%s.count", labelPrefix)  // eg: nvidia.com/gpu.count
				memoryLabel    = fmt.Sprintf("%s.memory", labelPrefix) // eg: nvidia.com/gpu.memory
				cardCount      = node.Labels[countLabel]               // eg: 8
				cardMemory     = node.Labels[memoryLabel]              // eg: 81920 (in MB)
			)
			if cardCount != "" && cardMemory != "" {
				cardCountInt64, err := strconv.ParseInt(cardCount, 10, 64)
				if err != nil {
					klog.Errorf("Failed to parse label %s: %+v", countLabel, err)
				}
				cardMemoryInt64, err := strconv.ParseInt(cardMemory, 10, 64)
				if err != nil {
					klog.Errorf("Failed to parse label %s: %+v", cardMemory, err)
				}
				return CardInfo{
					Name:           v,
					Count:          cardCountInt64,
					Memory:         cardMemoryInt64,
					ResourcePrefix: resourcePrefix,
				}
			}
		}
	}
	return CardInfo{}
}

// isMpsResourceName checks if the resource name is MPS resource name.
func isMpsResourceName(resourceName corev1.ResourceName) bool {
	return resourceName == MPSResourceName
}

// isMigResourceName checks if the resource name is MIG resource name.
func isMigResourceName(resourceName corev1.ResourceName) bool {
	return strings.HasPrefix(string(resourceName), MigLabelAndResourceNamePrefix)
}

// addResourceList adds the resources in 'add' to 'total'.
func addResourceList(total corev1.ResourceList, add corev1.ResourceList) {
	for resourceName, quantity := range add {
		if val, ok := total[resourceName]; ok {
			val.Add(quantity)
			total[resourceName] = val
		} else {
			total[resourceName] = quantity.DeepCopy()
		}
	}
}
