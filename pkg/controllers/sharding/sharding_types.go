/*
Copyright 2026 The Volcano Authors.
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

// PolicyRef is one entry in a scheduler's policy chain. Multiple PolicyRefs
// drive the multi-policy pipeline: each Filterer contributes to the filter
// phase (OR-union) and each Scorer to the score phase (weighted sum).
type PolicyRef struct {
	Name      string                 // registered policy name (e.g. "allocation-rate")
	Weight    int                    // applied to Score output; default 1; only meaningful for Scorers
	Arguments map[string]interface{} // policy-specific arguments
}

// SchedulerConfig defines the configuration for a scheduler.
type SchedulerConfig struct {
	Name string
	Type string // "volcano" or "agent"

	// Policies is the per-scheduler policy chain. Filterers contribute to
	// filter (OR-union); Scorers to score (weighted sum). Empty means no
	// policy participation in those phases.
	Policies []PolicyRef

	// MinNodes and MaxNodes are common (non-policy-specific) bounds on the
	// number of nodes assigned to this scheduler. The sharding controller
	// clamps the policy output to this range; policies do not see them.
	MinNodes int
	MaxNodes int
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

// ShardAssignment represents assignment for a single scheduler.
type ShardAssignment struct {
	SchedulerName string
	NodesDesired  []string
	Version       string
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
