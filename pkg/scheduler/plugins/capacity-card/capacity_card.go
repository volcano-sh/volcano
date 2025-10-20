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

// Package capacitycard implements a capacity-based scheduler plugin for managing GPU/NPU/TPU
// and other accelerator card resources in Volcano scheduler.
//
// The capacity-card plugin provides fine-grained resource management and scheduling capabilities
// for heterogeneous computing resources, with a focus on AI accelerator cards. It extends the
// basic capacity plugin functionality with specialized support for:
//
//   - Queue-level card resource quota management with per-card-type allocations
//   - Multi-card type support (e.g., NVIDIA A100, H100, V100 mixed scheduling)
//   - GPU sharing modes including MPS (Multi-Process Service) and MIG (Multi-Instance GPU)
//   - Flexible card selection with priority-based multi-card-type requests
//   - Decoupled CPU/memory quotas from card resource quotas
//
// # Core Features
//
// Queue Resource Management:
// The plugin manages resources at the queue level, tracking allocated, requested, guaranteed,
// and elastic resources for each queue. It calculates queue shares and enforces queue capacity
// limits based on configured quotas.
//
// Card Resource Annotations:
//   - volcano.sh/card.quota: Queue-level card quota configuration (JSON format)
//   - volcano.sh/card.request: Job-level card request specification (JSON format)
//   - volcano.sh/card.name: Task-level card type selection (pipe-separated list)
//
// Multi-Card Type Support:
// Tasks can specify multiple acceptable card types in priority order using the card.name annotation.
// The scheduler will select nodes based on card type availability and configured priority, allowing
// flexible resource utilization across heterogeneous GPU clusters.
//
// Special Card Modes:
//   - MPS Mode: Supports GPU time-slicing with configurable replica counts
//   - MIG Mode: Supports NVIDIA Multi-Instance GPU with various profile configurations
//
// CPU/Memory Independence:
// When cardUnlimitedCpuMemory is enabled, card resources are scheduled independently from CPU
// and memory constraints, allowing specialized quota management for card resources.
//
// # Plugin Functions
//
// The plugin implements the following scheduler extension points:
//
//   - JobEnqueueableFn: Validates if a job can be enqueued based on queue card quotas
//   - AllocatableFn: Checks if a task can be allocated to a node considering card availability
//   - NodeOrderFn: Scores nodes based on card type preferences and availability
//   - OnAllocate/OnDeallocate: Updates queue resource accounting on task allocation changes
//
// # Configuration
//
// Plugin configuration in scheduler configuration:
//
// 	actions: "enqueue, allocate, backfill"
// 	tiers:
// 	- plugins:
// 	  - name: capacity-card
// 	    arguments:
// 	      cardUnlimitedCpuMemory: true    # Optional: decouple card from CPU/memory
// 	      nodeOrderWeight: 1.5            # Optional: adjust node scoring weight
//
// # Resource Name Patterns
//
// The plugin recognizes the following card resource naming patterns:
//
//   - Standard GPU: nvidia.com/gpu, amd.com/gpu
//   - MPS Cards: nvidia.com/mps-<N>g*1/<M> (N=card count, M=replica count)
//   - MIG Cards: nvidia.com/mig-<profile>-mixed (e.g., mig-1g.12gb-mixed)
//   - Custom Cards: Configured via node labels with <vendor>/<card-type> pattern
//
// # Example Usage
//
// Queue with card quota:
//
// 	apiVersion: scheduling.volcano.sh/v1beta1
// 	kind: Queue
// 	metadata:
// 	  name: ai-queue
// 	  annotations:
// 	    volcano.sh/card.quota: |
// 	      {
// 	        "NVIDIA-A100": 8,
// 	        "NVIDIA-V100": 16
// 	      }
//
// Job with card request:
//
// 	apiVersion: batch.volcano.sh/v1alpha1
// 	kind: Job
// 	metadata:
// 	  annotations:
// 	    volcano.sh/card.request: |
// 	      {
// 	        "NVIDIA-A100": 2
// 	      }
//
// Task with multi-card-type request:
//
// 	spec:
// 	  tasks:
// 	  - replicas: 1
// 	    template:
// 	      metadata:
// 	        annotations:
// 	          volcano.sh/card.name: "NVIDIA-A100|NVIDIA-H100|NVIDIA-V100"
//
// For detailed implementation and algorithm documentation, see capacity-card-plugin-doc.md.

package capacitycard

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "capacity-card"

	// MPSResourceName is the fixed resource name for GPU mps card. e.g. nvidia.com/gpu.shared
	MPSResourceName = "nvidia.com/gpu.shared"

	// MpsReplicaLabel is the fixed label for GPU mps replica number. e.g. nvidia.com/gpu.replicas=2
	MpsReplicaLabel = "nvidia.com/gpu.replicas"

	// MpsSharedCardNamePattern is the fixed naming pattern for mps card name. e.g. nvidia.com/mps-2g*1/2
	MpsSharedCardNamePattern = "%s/mps-%dg*1/%d"

	// MigSharedCardNamePattern is the fixed naming pattern for mig card name. e.g. nvidia.com/mig-1g.12gb-mixed
	MigSharedCardNamePattern = "%s/mig-%s-mixed"

	// MigLabelAndResourceNamePrefix is the fixed prefix for GPU mig label and resource name.
	MigLabelAndResourceNamePrefix = "nvidia.com/mig-"

	// QueueAnnotationKeyCardQuota is the annotation keys of queue and job for card resources.
	// This annotation is added to queue's capability and guarantee during queue creation.
	QueueAnnotationKeyCardQuota = "volcano.sh/card.quota"

	// JobAnnotationKeyCardRequest is the annotation keys of queue and job for card resources.
	// This annotation is for job-level card request pre-check purpose.
	JobAnnotationKeyCardRequest = "volcano.sh/card.request"

	// TaskAnnotationKeyCardName is the annotation key of task for card name request.
	TaskAnnotationKeyCardName = "volcano.sh/card.name"

	// MultiCardSeparator is the separator for multi-card request in task annotation.
	MultiCardSeparator = "|"

	// cardCountQuantityMultiplier is used to convert card count to resource quantity in milli.
	// As all volcano scalar resource quantity is in milli, we use 1000 as multiplier.
	// For example, if a task requests 2 cards, it will be converted to 2000 in scalar resource quantity.
	cardCountQuantityMultiplier = 1000

	// cardUnlimitedCpuMemory is the plugin config name of card-unlimited cpu memory.
	cardUnlimitedCpuMemory = "cardUnlimitedCpuMemory"

	// nodeOrderWeight is the plugin config name for node order weight when multi-card types are present.
	// This weight is multiplied with the final node score.
	// Default value is 1.0 (no scaling). Can be any positive number.
	// Higher values increase the importance of card type priority in overall scheduling decisions.
	nodeOrderWeight = "nodeOrderWeight"

	// defaultNodeOrderWeight is the default weight for node order scoring.
	defaultNodeOrderWeight = 1.0

	// nodeOrderDecayRate is the fixed decay rate for calculating base scores.
	// Each subsequent card type gets this factor applied: score = maxScore * (decayRate ^ index)
	// Using 0.5 means: 1st card=100, 2nd card=50, 3rd card=25, etc.
	nodeOrderDecayRate = 0.5
)

// Plugin implements the capacity plugin.
type Plugin struct {
	queueOpts                map[api.QueueID]*queueAttr
	totalResource            *api.Resource
	totalGuarantee           *api.Resource
	nodeLister               v1.NodeLister
	nodeCardInfos            map[string]NodeCardResourceInfo
	cardNameToResourceName   map[corev1.ResourceName]corev1.ResourceName
	isCardUnlimitedCpuMemory bool
	nodeOrderWeight          float64
}

// New return capacity plugin.
func New(_ framework.Arguments) framework.Plugin {
	return &Plugin{
		queueOpts:              map[api.QueueID]*queueAttr{},
		totalResource:          api.EmptyResource(),
		totalGuarantee:         api.EmptyResource(),
		nodeCardInfos:          map[string]NodeCardResourceInfo{},
		cardNameToResourceName: map[corev1.ResourceName]corev1.ResourceName{},
	}
}

// Name returns name of the plugin.
func (p *Plugin) Name() string {
	return PluginName
}

// OnSessionOpen initializes the plugin state.
func (p *Plugin) OnSessionOpen(ssn *framework.Session) {
	p.buildEventRecorder(ssn)
	readyToSchedule := p.buildTotalResource(ssn)
	if readyToSchedule {
		readyToSchedule = p.buildQueueAttrs(ssn)
	}
	if readyToSchedule {
		p.buildQueueMetrics(ssn)
	}

	klog.V(4).Infof("Total resource is: %v", p.totalResource)
	klog.V(4).Infof("Total guarantee is: %v", p.totalGuarantee)

	p.isCardUnlimitedCpuMemory = p.IsCardUnlimitedCpuMemory(ssn)
	klog.V(4).Infof("IsCardUnlimitedCpuMemory: %v", p.isCardUnlimitedCpuMemory)

	p.nodeOrderWeight = p.GetNodeOrderWeight(ssn)
	klog.V(4).Infof("NodeOrderWeight: %v", p.nodeOrderWeight)

	// Job enqueueable check.
	ssn.AddJobEnqueueableFn(p.Name(), func(obj any) int {
		jobInfo := obj.(*api.JobInfo)
		if !readyToSchedule {
			klog.V(2).Infof(
				"Plugin <%s> is not ready to schedule, reject job <%s/%s>.",
				p.Name(), jobInfo.Namespace, jobInfo.Name,
			)
			return util.Reject
		}
		return p.JobEnqueueableFn(ssn, jobInfo)
	})

	// Task allocatable check.
	ssn.AddAllocatableFn(p.Name(), func(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		if !readyToSchedule {
			klog.V(2).Infof(
				"Plugin <%s> is not ready to schedule, reject task <%s/%s>.",
				p.Name(), candidate.Namespace, candidate.Name,
			)
			return false
		}
		return p.AllocatableFn(queue, candidate)
	})

	// It updates the queue's allocated resource and share
	// when a task is allocated or deallocated duration plugin executing.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			p.OnAllocate(ssn, event)
		},
		DeallocateFunc: func(event *framework.Event) {
			p.OnDeallocate(ssn, event)
		},
	})

	// add AddNodeOrderFn for job using multi-card resources. To support card selection by order.
	ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		return p.NodeOrderFn(task, node)
	})
}

// OnSessionClose cleans up the plugin state.
func (p *Plugin) OnSessionClose(_ *framework.Session) {
	p.queueOpts = nil
	p.totalResource = nil
	p.totalGuarantee = nil
	p.nodeCardInfos = nil
	p.cardNameToResourceName = nil
}

// IsCardUnlimitedCpuMemory checks if the card resource is unlimited cpu memory.
func (p *Plugin) IsCardUnlimitedCpuMemory(ssn *framework.Session) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != PluginName {
				continue
			}
			cardUnlimitedCpuMemoryVal, ok := plugin.Arguments[cardUnlimitedCpuMemory]
			if !ok {
				return false
			}
			cardUnlimitedCpuMemoryBool, ok := cardUnlimitedCpuMemoryVal.(bool)
			if !ok {
				return false
			}
			return cardUnlimitedCpuMemoryBool
		}
	}
	return false
}

// GetNodeOrderWeight gets the node order weight from plugin arguments.
// The weight is multiplied with the final score. Must be positive.
// Returns defaultNodeOrderWeight if not configured or invalid.
func (p *Plugin) GetNodeOrderWeight(ssn *framework.Session) float64 {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != PluginName {
				continue
			}
			weightVal, ok := plugin.Arguments[nodeOrderWeight]
			if !ok {
				return defaultNodeOrderWeight
			}

			// Try float64 first
			if weightFloat, ok := weightVal.(float64); ok {
				if weightFloat > 0 {
					return weightFloat
				}
				klog.Warningf("Invalid nodeOrderWeight value: %v, must be positive, using default: %v", weightFloat, defaultNodeOrderWeight)
				return defaultNodeOrderWeight
			}

			// Try int as fallback (in case config uses integer)
			if weightInt, ok := weightVal.(int); ok {
				weightFloat := float64(weightInt)
				if weightFloat > 0 {
					return weightFloat
				}
				klog.Warningf("Invalid nodeOrderWeight value: %v, must be positive, using default: %v", weightFloat, defaultNodeOrderWeight)
				return defaultNodeOrderWeight
			}

			klog.Warningf("Invalid nodeOrderWeight type: %T, using default: %v", weightVal, defaultNodeOrderWeight)
			return defaultNodeOrderWeight
		}
	}
	return defaultNodeOrderWeight
}

// HasCardResource checks whether the job has card resource.
// If job has card resource, return true.
// If job has scalar resource, check whether it has card resource name.
func (p *Plugin) HasCardResource(cardResource, scalarResource *api.Resource) bool {
	if cardResource != nil && len(cardResource.ScalarResources) > 0 {
		return true
	}

	if scalarResource == nil || len(scalarResource.ScalarResources) == 0 {
		return false
	}

	for _, resourceName := range p.cardNameToResourceName {
		if _, ok := scalarResource.ScalarResources[resourceName]; ok {
			return true
		}
	}

	return false
}
