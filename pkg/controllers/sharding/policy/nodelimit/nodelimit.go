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

package nodelimit

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

// PolicyName is the user-visible name of this policy.
const PolicyName = "node-limit"

type nodeLimitPolicy struct {
	minNodes int
	maxNodes int
}

// New creates a new node-limit policy instance.
func New() policy.ShardPolicy {
	return &nodeLimitPolicy{}
}

func (p *nodeLimitPolicy) Name() string { return PolicyName }

func (p *nodeLimitPolicy) Initialize(args policy.Arguments) error {
	args.GetInt(&p.minNodes, "minNodes")
	args.GetInt(&p.maxNodes, "maxNodes")

	if p.minNodes < 0 {
		return fmt.Errorf("invalid minNodes %d: must be >= 0", p.minNodes)
	}
	if p.maxNodes > 0 && p.maxNodes < p.minNodes {
		return fmt.Errorf("invalid bounds [%d, %d]: maxNodes must be >= minNodes",
			p.minNodes, p.maxNodes)
	}

	klog.V(3).Infof("Initialized node-limit policy: bounds [%d, %d]",
		p.minNodes, p.maxNodes)
	return nil
}

func (p *nodeLimitPolicy) Cleanup() {}

// Select truncates the sorted candidate list to at most maxNodes. minNodes
// is informational: the framework cannot synthesize nodes that don't
// exist. A shortfall is logged but the result is not padded.
//
// maxNodes <= 0 means "no upper bound".
func (p *nodeLimitPolicy) Select(ctx *policy.PolicyContext, candidates []*corev1.Node) []*corev1.Node {
	if p.maxNodes > 0 && len(candidates) > p.maxNodes {
		candidates = candidates[:p.maxNodes]
	}
	if p.minNodes > 0 && len(candidates) < p.minNodes {
		klog.V(4).Infof("Pipeline returned %d nodes for scheduler %s, below minNodes=%d",
			len(candidates), ctx.SchedulerName, p.minNodes)
	}
	return candidates
}
