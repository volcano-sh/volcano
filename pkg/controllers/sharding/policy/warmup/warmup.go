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

package warmup

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "warmup"

type warmupPolicy struct {
	warmupLabel      string
	warmupLabelValue string
}

// New creates a new warmup policy instance.
func New() policy.ShardPolicy {
	return &warmupPolicy{
		warmupLabel:      "node.volcano.sh/warmup",
		warmupLabelValue: "true",
	}
}

func (p *warmupPolicy) Name() string { return PolicyName }

func (p *warmupPolicy) Initialize(args policy.Arguments) error {
	args.GetString(&p.warmupLabel, "warmupLabel")
	args.GetString(&p.warmupLabelValue, "warmupLabelValue")

	if p.warmupLabel == "" {
		return fmt.Errorf("warmupLabel cannot be empty")
	}

	klog.V(3).Infof("Initialized warmup policy: label %s=%s",
		p.warmupLabel, p.warmupLabelValue)
	return nil
}

func (p *warmupPolicy) Cleanup() {}

// Score returns 1.0 if the node carries the configured warmup label/value,
// else 0.0. Combined with weight: N in config, warmup-labeled nodes receive
// a +N boost in the framework's weighted-sum sort — pulled ahead of
// non-warmup candidates without filtering non-warmup out entirely.
func (p *warmupPolicy) Score(ctx *policy.PolicyContext, node *corev1.Node) float64 {
	if node == nil || node.Labels[p.warmupLabel] != p.warmupLabelValue {
		return 0.0
	}
	return 1.0
}
