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

package sharding

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// ShardStrategy defines hard boundaries for node assignment
type ShardStrategy struct {
	// CPUUtilizationRange specifies inclusive range [min, max] for CPU utilization
	CPUUtilizationRange struct {
		Min float64
		Max float64
	}

	// PreferWarmupNodes indicates preference for warmup nodes
	PreferWarmupNodes bool

	// MinNodes and MaxNodes define node count constraints
	MinNodes int
	MaxNodes int
}

// SchedulerConfig defines the configuration for a scheduler
type SchedulerConfig struct {
	Name          string
	Type          string // "volcano" or "agent"
	ShardStrategy ShardStrategy
}

// AssignmentCache stores the result of shard assignments with version control
type AssignmentCache struct {
	Version     string
	Timestamp   time.Time
	Assignments map[string]*ShardAssignment // scheduler name -> assignment
}

// NodeMetrics contains comprehensive metrics for a node
type NodeMetrics struct {
	NodeName        string
	ResourceVersion string
	LastUpdated     time.Time

	// Resource capacity and allocatable
	CPUCapacity       resource.Quantity
	CPUAllocatable    resource.Quantity
	MemoryCapacity    resource.Quantity
	MemoryAllocatable resource.Quantity

	// Resource utilization
	CPUUtilization    float64 // 0.0 to 1.0
	MemoryUtilization float64 // 0.0 to 1.0

	// Node characteristics
	IsWarmupNode bool
	PodCount     int
	Labels       map[string]string
	Annotations  map[string]string
}

// NodeMetricsProvider provides access to node metrics
type NodeMetricsProvider interface {
	GetNodeMetrics(nodeName string) *NodeMetrics
	GetAllNodeMetrics() map[string]*NodeMetrics
	UpdateNodeMetrics(nodeName string, metrics *NodeMetrics)
}

// NodeResourceInfo contains resource utilization information for a node
type NodeResourceInfo struct {
	NodeName          string
	CPUAllocatable    resource.Quantity
	CPUCapacity       resource.Quantity
	MemoryAllocatable resource.Quantity
	MemoryCapacity    resource.Quantity
	CPUUtilization    float64 // 0.0 to 1.0
	MemoryUtilization float64 // 0.0 to 1.0
	IsWarmupNode      bool
	PodCount          int
	Labels            map[string]string
	Annotations       map[string]string
}

// ShardAssignment represents assignment for a single scheduler
type ShardAssignment struct {
	SchedulerName string
	NodesDesired  []string
	StrategyUsed  string
	Version       string
	Reason        string
}

// AssignmentChangeEvent represents a change in assignment
type AssignmentChangeEvent struct {
	SchedulerName string
	OldNodes      []string
	NewNodes      []string
	NodesToAdd    []string
	NodesToRemove []string
	Version       string
	Timestamp     time.Time
}

// AssignmentContext contains context information for shard assignment
type AssignmentContext struct {
	AllNodes         []*corev1.Node
	CurrentShards    map[string]*shardv1alpha1.NodeShard
	SchedulerConfigs []SchedulerConfig
	AssignedNodes    map[string]string // node name -> scheduler name
	Timestamp        time.Time
}
