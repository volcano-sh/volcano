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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"volcano.sh/volcano/pkg/controllers/sharding/policy/allocationrate"
)

const (
	// ConfigMapDataKey is the key inside the ConfigMap that holds the YAML configuration.
	ConfigMapDataKey = "sharding.yaml"
	// DefaultConfigMapName is the default name of the sharding ConfigMap.
	DefaultConfigMapName = "volcano-sharding-configmap"
	// DefaultConfigMapNamespace is the default namespace of the sharding ConfigMap.
	DefaultConfigMapNamespace = "volcano-system"
	// DefaultPolicyName is the policy used when none is specified (backward compatibility).
	DefaultPolicyName = "allocation-rate"
)

// preferWarmupNodesDeprecatedMsg is logged when a config sets the deprecated
// top-level preferWarmupNodes field. The flag is no longer honored by the
// allocation-rate policy after the pluggable-policy refactor; users who want
// warmup-aware ordering should switch to policy=warmup.
const preferWarmupNodesDeprecatedMsg = "scheduler %q: preferWarmupNodes is no longer honored by the allocation-rate policy; " +
	"use policy=warmup to prioritize warmup nodes"

const nodeLimitScalarsDeprecatedMsg = "scheduler %q: scheduler-level minNodes/maxNodes are deprecated; " +
	"configure the node-limit policy with minNodes/maxNodes arguments instead"

// PolicySpec is one policy entry in a scheduler's policy chain. Mirrors
// the user-facing YAML shape; converted to the internal PolicyRef at
// controller setup.
type PolicySpec struct {
	// Name is the registered policy name (e.g. "allocation-rate", "warmup").
	Name string `json:"name"`
	// Weight scales the policy's Score contribution; default 1. Only
	// meaningful for policies that implement the Scorer interface.
	Weight int `json:"weight,omitempty"`
	// Arguments holds policy-specific arguments. minNodes/maxNodes are only
	// valid on the node-limit policy.
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// SchedulerConfigSpec defines the per-scheduler sharding parameters.
// It is used both as the internal representation and as the YAML-serialisable
// form stored in the sharding ConfigMap.
type SchedulerConfigSpec struct {
	// Name is the scheduler name (must match the schedulerName field in NodeShard).
	Name string `json:"name"`
	// Type describes the workload class (e.g. "volcano", "agent").
	Type string `json:"type"`

	// Policies is the per-scheduler policy chain. When empty,
	// applyPolicyDefaults synthesizes a default chain from legacy fields.
	Policies []PolicySpec `json:"policies,omitempty"`
	// Arguments holds legacy single-policy arguments. Deprecated: use Policies.
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	// CPUUtilizationMin is the lower bound (inclusive) of the CPU utilisation range
	// [0.0, 1.0] that makes a node eligible for this scheduler's shard.
	//
	// Deprecated: use Policies instead.
	CPUUtilizationMin float64 `json:"cpuUtilizationMin"`
	// CPUUtilizationMax is the upper bound (inclusive) of the CPU utilisation range.
	//
	// Deprecated: use Policies instead.
	CPUUtilizationMax float64 `json:"cpuUtilizationMax"`
	// PreferWarmupNodes indicates whether warmup nodes should be sorted before
	// regular nodes when selecting shard members.
	//
	// Deprecated: use Policies (add a "warmup" entry) instead.
	PreferWarmupNodes bool `json:"preferWarmupNodes"`

	// MinNodes is informational only: the framework cannot synthesize nodes
	// that don't exist. If the policy chain returns fewer than MinNodes, the
	// shortfall is logged but the result is not padded. Scheduler-level
	// scalar; not a per-policy setting.
	//
	// Deprecated: use Policies (add a "node-limit" entry) instead.
	MinNodes int `json:"minNodes"`
	// MaxNodes is a hard cap. The policy-selected node list is truncated to
	// at most MaxNodes by the framework. Scheduler-level scalar; not a
	// per-policy setting. MaxNodes <= 0 means "no upper bound".
	//
	// Deprecated: use Policies (add a "node-limit" entry) instead.
	MaxNodes int `json:"maxNodes"`
}

// ShardingConfig is the top-level structure that is serialised as YAML into
// the sharding ConfigMap (key: sharding.yaml).
// See example/sharding/sharding-config-configmap.yaml for the full format.
type ShardingConfig struct {
	// SchedulerConfigs holds the per-scheduler shard specifications.
	SchedulerConfigs []SchedulerConfigSpec `json:"schedulerConfigs"`
	// ShardSyncPeriod overrides the periodic sync interval when set.
	// Accepts Go duration strings such as "60s", "2m".
	ShardSyncPeriod string `json:"shardSyncPeriod,omitempty"`
	// EnableNodeEventTrigger controls event-driven shard updates.
	EnableNodeEventTrigger *bool `json:"enableNodeEventTrigger,omitempty"`
}

// ParseShardingConfig deserialises YAML bytes into a ShardingConfig.
// Returns an error when the input is not valid YAML or violates basic
// constraints (e.g. empty scheduler list).
func ParseShardingConfig(data []byte) (*ShardingConfig, error) {
	cfg := &ShardingConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sharding config: %w", err)
	}
	if len(cfg.SchedulerConfigs) == 0 {
		return nil, fmt.Errorf("sharding config must contain at least one schedulerConfig entry")
	}
	for i, sc := range cfg.SchedulerConfigs {
		if sc.Name == "" {
			return nil, fmt.Errorf("schedulerConfigs[%d]: name must not be empty", i)
		}
		if sc.CPUUtilizationMin < 0 || sc.CPUUtilizationMin > 1 {
			return nil, fmt.Errorf("schedulerConfigs[%d] (%s): cpuUtilizationMin %.2f out of range [0,1]", i, sc.Name, sc.CPUUtilizationMin)
		}
		if sc.CPUUtilizationMax < 0 || sc.CPUUtilizationMax > 1 {
			return nil, fmt.Errorf("schedulerConfigs[%d] (%s): cpuUtilizationMax %.2f out of range [0,1]", i, sc.Name, sc.CPUUtilizationMax)
		}
		if sc.CPUUtilizationMin > sc.CPUUtilizationMax {
			return nil, fmt.Errorf("schedulerConfigs[%d] (%s): cpuUtilizationMin (%.2f) must be <= cpuUtilizationMax (%.2f)", i, sc.Name, sc.CPUUtilizationMin, sc.CPUUtilizationMax)
		}
		if sc.MinNodes < 0 {
			return nil, fmt.Errorf("schedulerConfigs[%d] (%s): minNodes must be >= 0", i, sc.Name)
		}
		if sc.MaxNodes < sc.MinNodes {
			return nil, fmt.Errorf("schedulerConfigs[%d] (%s): maxNodes (%d) must be >= minNodes (%d)", i, sc.Name, sc.MaxNodes, sc.MinNodes)
		}
		if err := validatePolicies(i, sc.Name, sc.Policies); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// validatePolicies checks per-policy constraints that apply regardless of
// whether the spec arrived via ConfigMap or CLI flags. Callable from any
// entry point that builds a SchedulerConfigSpec.
//
// node-limit is the one policy that legitimately consumes minNodes /
// maxNodes in its Arguments; the rejection rule below explicitly allows it.
func validatePolicies(specIdx int, schedName string, policies []PolicySpec) error {
	for j, p := range policies {
		if p.Name == "" {
			return fmt.Errorf("schedulerConfigs[%d] (%s): policies[%d].name must not be empty", specIdx, schedName, j)
		}
		if p.Weight < 0 {
			return fmt.Errorf("schedulerConfigs[%d] (%s): policies[%d] (%s): weight must be >= 0 (omit for default of 1)", specIdx, schedName, j, p.Name)
		}
		if p.Name == "node-limit" {
			continue
		}
		if _, ok := p.Arguments["minNodes"]; ok {
			return fmt.Errorf("schedulerConfigs[%d] (%s): policies[%d] (%s): minNodes is only supported by the node-limit policy", specIdx, schedName, j, p.Name)
		}
		if _, ok := p.Arguments["maxNodes"]; ok {
			return fmt.Errorf("schedulerConfigs[%d] (%s): policies[%d] (%s): maxNodes is only supported by the node-limit policy", specIdx, schedName, j, p.Name)
		}
	}
	return nil
}

// ShardingControllerOptions holds all runtime-configurable options for the
// ShardingController.
type ShardingControllerOptions struct {
	// SchedulerConfigsRaw holds the legacy colon-separated scheduler config
	// strings, provided via --scheduler-configs flag.
	SchedulerConfigsRaw []string
	// SchedulerConfigs is the parsed representation of SchedulerConfigsRaw.
	SchedulerConfigs []SchedulerConfigSpec
	// ShardSyncPeriod is the period between full shard reconciliations.
	ShardSyncPeriod time.Duration
	// EnableNodeEventTrigger controls whether node/pod events trigger immediate
	// shard reconciliation.
	EnableNodeEventTrigger bool
	// ConfigMapName is the name of the ConfigMap that holds the sharding
	// configuration.  When non-empty, the controller prefers ConfigMap-based
	// configuration over flag-based configuration and watches the ConfigMap
	// for live updates.
	ConfigMapName string
	// ConfigMapNamespace is the namespace of the sharding ConfigMap.
	ConfigMapNamespace string
}

// NewShardingControllerOptions returns a ShardingControllerOptions with
// sensible defaults.
func NewShardingControllerOptions() ShardingControllerOptions {
	controllerOptions := ShardingControllerOptions{
		ShardSyncPeriod:        60 * time.Second,
		EnableNodeEventTrigger: true,
		SchedulerConfigsRaw: []string{
			"volcano:volcano:0.0:0.6:false:2:100",
			"agent-scheduler:agent:0.7:1.0:true:2:100",
		},
		ConfigMapName:      DefaultConfigMapName,
		ConfigMapNamespace: DefaultConfigMapNamespace,
	}
	if err := controllerOptions.ParseConfig(); err != nil {
		klog.V(4).Infof("cannot parse scheduler configurations: %s correctly, please check and fix it correctly!", strings.Join(controllerOptions.SchedulerConfigsRaw, ", "))
	}
	return controllerOptions
}

// AddFlags adds flags to the flag set using pflag pattern.
func (opts *ShardingControllerOptions) AddFlags(fs *pflag.FlagSet) {
	defaultConfigs := []string{
		"volcano:volcano:0.0:0.6:false:2:100",
		"agent-scheduler:agent:0.7:1.0:true:2:100",
	}

	fs.StringSliceVar(&opts.SchedulerConfigsRaw, "scheduler-configs", defaultConfigs,
		"Deprecated: use a sharding ConfigMap (--sharding-configmap) instead. "+
			"Format: name:type:min_util:max_util:prefer_warmup:min_nodes:max_nodes. "+
			"Used only when no valid sharding ConfigMap is available.")

	fs.DurationVar(&opts.ShardSyncPeriod, "shard-sync-period", 60*time.Second,
		"Period for shard synchronization")

	fs.BoolVar(&opts.EnableNodeEventTrigger, "enable-node-event-trigger", true,
		"Enable node event trigger for shard updates")

	fs.StringVar(&opts.ConfigMapName, "sharding-configmap", DefaultConfigMapName,
		"Name of the ConfigMap that contains sharding configuration (key: "+ConfigMapDataKey+"). "+
			"When set, ConfigMap-based config takes precedence over --scheduler-configs flags and "+
			"the controller watches the ConfigMap for live updates.")

	fs.StringVar(&opts.ConfigMapNamespace, "sharding-configmap-namespace", DefaultConfigMapNamespace,
		"Namespace of the sharding configuration ConfigMap.")
}

// ParseConfig parses the raw colon-separated config strings into SchedulerConfigs.
// This is used only when ConfigMap-based configuration is not available.
// Warning: configuring sharding via command line is deprecated and will be
// removed in a future release; use the sharding ConfigMap instead.
func (opts *ShardingControllerOptions) ParseConfig() error {
	configs := make([]SchedulerConfigSpec, 0, len(opts.SchedulerConfigsRaw))

	for _, configStr := range opts.SchedulerConfigsRaw {
		parts := strings.Split(configStr, ":")

		if len(parts) != 7 {
			return fmt.Errorf("invalid scheduler config format: %s, expected 7 colon-separated parts", configStr)
		}

		config, err := parseOldFormat(parts)
		if err != nil {
			return fmt.Errorf("failed to parse scheduler config %s: %v", configStr, err)
		}
		klog.V(3).Infof("Parsed old format config for scheduler %s, converting to %s policy", config.Name, DefaultPolicyName)
		configs = append(configs, config)
	}
	klog.Warningf("--scheduler-configs is deprecated and will be removed in a future release; migrate to the sharding ConfigMap")
	opts.SchedulerConfigs = configs
	return nil
}

// parseOldFormat parses old format config: name:type:minUtil:maxUtil:preferWarmup:minNodes:maxNodes
func parseOldFormat(parts []string) (SchedulerConfigSpec, error) {
	minUtil, err := parseUtilization(parts[2])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid min utilization: %v", err)
	}
	maxUtil, err := parseUtilization(parts[3])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid max utilization: %v", err)
	}
	preferWarmup, err := strconv.ParseBool(parts[4])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid prefer warmup flag: %v", err)
	}
	minNodes, err := strconv.Atoi(parts[5])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid min nodes: %v", err)
	}
	maxNodes, err := strconv.Atoi(parts[6])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid max nodes: %v", err)
	}

	if preferWarmup {
		klog.Warningf(preferWarmupNodesDeprecatedMsg, parts[0])
	}

	return SchedulerConfigSpec{
		Name: parts[0],
		Type: parts[1],
		Arguments: map[string]interface{}{
			allocationrate.ArgMinCPUUtil: minUtil,
			allocationrate.ArgMaxCPUUtil: maxUtil,
		},
		Policies: []PolicySpec{{
			Name:   DefaultPolicyName,
			Weight: 1,
			Arguments: map[string]interface{}{
				allocationrate.ArgMinCPUUtil: minUtil,
				allocationrate.ArgMaxCPUUtil: maxUtil,
			},
		}},
		// Keep deprecated fields for backwards compatibility
		CPUUtilizationMin: minUtil,
		CPUUtilizationMax: maxUtil,
		PreferWarmupNodes: preferWarmup,
		MinNodes:          minNodes,
		MaxNodes:          maxNodes,
	}, nil
}

// applyPolicyDefaults populates spec.Policies from deprecated scalar fields
// for compatibility. Synthesis rules:
//
//   - spec.Policies empty: synthesize an allocation-rate entry from
//     spec.Arguments (or CPUUtilizationMin/Max when Arguments is empty).
//   - PreferWarmupNodes set: append a warmup entry.
//   - MinNodes/MaxNodes > 0 and node-limit not in chain: append a node-limit
//     entry and warn users to configure node-limit directly.
func applyPolicyDefaults(spec *SchedulerConfigSpec) {
	if len(spec.Policies) == 0 {
		if len(spec.Arguments) == 0 {
			spec.Arguments = map[string]interface{}{
				allocationrate.ArgMinCPUUtil: spec.CPUUtilizationMin,
				allocationrate.ArgMaxCPUUtil: spec.CPUUtilizationMax,
			}
		}
		spec.Policies = []PolicySpec{{
			Name:      DefaultPolicyName,
			Weight:    1,
			Arguments: spec.Arguments,
		}}
		if spec.PreferWarmupNodes {
			klog.Warningf(preferWarmupNodesDeprecatedMsg, spec.Name)
			spec.Policies = append(spec.Policies, PolicySpec{
				Name:   "warmup",
				Weight: 1,
			})
		}
	}

	if (spec.MinNodes > 0 || spec.MaxNodes > 0) && !hasPolicy(spec.Policies, "node-limit") {
		klog.Warningf(nodeLimitScalarsDeprecatedMsg, spec.Name)
		spec.Policies = append(spec.Policies, PolicySpec{
			Name: "node-limit",
			Arguments: map[string]interface{}{
				"minNodes": spec.MinNodes,
				"maxNodes": spec.MaxNodes,
			},
		})
	}
}

func hasPolicy(policies []PolicySpec, name string) bool {
	for _, p := range policies {
		if p.Name == name {
			return true
		}
	}
	return false
}

// parseUtilization parses a utilization string to float64.
func parseUtilization(s string) (float64, error) {
	if s == "inf" || s == "infinity" {
		return 1.0, nil
	}
	if s == "-inf" || s == "-infinity" {
		return 0.0, nil
	}
	return strconv.ParseFloat(s, 64)
}
