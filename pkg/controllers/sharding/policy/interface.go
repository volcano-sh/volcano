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

package policy

import (
	corev1 "k8s.io/api/core/v1"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// ShardPolicy defines the interface for shard assignment strategies
type ShardPolicy interface {
	// Name returns the policy name
	Name() string

	// Initialize initializes the policy with configuration arguments
	Initialize(args Arguments) error

	// Calculate calculates node assignments for the scheduler
	Calculate(ctx *PolicyContext) (*PolicyResult, error)

	// Cleanup performs any cleanup when the policy is no longer needed
	Cleanup()
}

// PolicyContext provides input for policy execution
type PolicyContext struct {
	// SchedulerName is the name of the scheduler
	SchedulerName string

	// SchedulerType is the type of scheduler (e.g., "volcano", "agent")
	SchedulerType string

	// AllNodes is the list of all nodes in the cluster
	AllNodes []*corev1.Node

	// NodeMetrics contains metrics for all nodes
	NodeMetrics map[string]*NodeMetrics

	// AssignedNodes tracks nodes already assigned to other schedulers
	// Key: node name, Value: scheduler name
	AssignedNodes map[string]string

	// CurrentShard is the current NodeShard for this scheduler (if exists)
	CurrentShard *shardv1alpha1.NodeShard

	// PolicyArguments contains policy-specific configuration
	PolicyArguments Arguments
}

// PolicyResult contains policy output
type PolicyResult struct {
	// SelectedNodes is the list of node names assigned to the scheduler
	SelectedNodes []string

	// Reason is a human-readable explanation for the assignment
	Reason string

	// Metadata contains additional information about the policy execution
	Metadata map[string]interface{}
}

// Arguments holds configuration parameters for policies
type Arguments map[string]interface{}

// PolicyBuilder is a factory function that creates policy instances
type PolicyBuilder func() ShardPolicy
