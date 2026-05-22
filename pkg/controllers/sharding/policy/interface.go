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

package policy

import (
	corev1 "k8s.io/api/core/v1"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// ShardPolicy is the lifecycle contract every policy implements. A policy
// participates in pipeline phases by additionally implementing Filterer
// and/or Scorer; the framework uses type assertions to dispatch.
type ShardPolicy interface {
	Name() string
	Initialize(args Arguments) error
	Cleanup()
}

// Filterer participates in the filter phase. A node is eligible only when
// every configured Filterer accepts it (AND-intersection). Implementations
// must be deterministic and side-effect free. Nodes already assigned to
// another scheduler are excluded by the framework before Filter is called.
type Filterer interface {
	Filter(ctx *PolicyContext, node *corev1.Node) bool
}

// Scorer participates in the score phase. Score returns a value in [0, 1];
// higher is better. The framework combines Scorers via weighted sum:
//
//	final[node] = Σ (policy.weight × policy.Score(node))
//
// Implementations must clamp/normalize their output to [0, 1]; the framework
// does not enforce.
type Scorer interface {
	Score(ctx *PolicyContext, node *corev1.Node) float64
}

// Selector participates in the select phase (clamp). It receives candidates
// that have already been Filtered and ordered by Score, and returns the
// final shard membership. Selectors enforce collection-level constraints
// such as max node count or resource budgets.
//
// Multiple Selectors run in config order; each receives the previous one's
// output. Returning an empty slice stops the chain. A Selector must not
// reorder candidates — it returns the input slice unchanged or a prefix
// thereof.
type Selector interface {
	Select(ctx *PolicyContext, candidates []*corev1.Node) []*corev1.Node
}

// PolicyContext provides input for policy execution.
type PolicyContext struct {
	SchedulerName string
	SchedulerType string

	AllNodes      []*corev1.Node
	NodeMetrics   map[string]*NodeMetrics
	AssignedNodes map[string]string // node name → scheduler name
	CurrentShard  *shardv1alpha1.NodeShard
}

// Arguments holds configuration parameters for policies.
type Arguments map[string]interface{}

// PolicyBuilder is a factory function that creates policy instances.
type PolicyBuilder func() ShardPolicy
