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
