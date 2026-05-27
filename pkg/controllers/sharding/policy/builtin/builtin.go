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

// Package builtin registers all built-in shard policies with the policy
// registry. It lives in its own package (sibling to policy/allocationrate,
// policy/nodelimit, policy/warmup) so that the parent `policy` package has
// no inbound imports from its subpackages, which would create an import
// cycle (each subpackage imports `policy` for the ShardPolicy interface).
//
// Importers should blank-import this package once, before any policy is
// looked up via policy.GetPolicy:
//
//	import _ "volcano.sh/volcano/pkg/controllers/sharding/policy/builtin"
package builtin

import (
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
	"volcano.sh/volcano/pkg/controllers/sharding/policy/allocationrate"
	"volcano.sh/volcano/pkg/controllers/sharding/policy/nodelimit"
	"volcano.sh/volcano/pkg/controllers/sharding/policy/warmup"
)

func init() {
	policy.RegisterPolicy(allocationrate.PolicyName, allocationrate.New)
	policy.RegisterPolicy(nodelimit.PolicyName, nodelimit.New)
	policy.RegisterPolicy(warmup.PolicyName, warmup.New)

	klog.V(2).Infof("Registered %d shard policies: %v", len(policy.ListPolicies()), policy.ListPolicies())
}
