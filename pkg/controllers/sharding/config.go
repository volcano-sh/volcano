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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const (
	// ConfigMapDataKey is the key inside the ConfigMap that holds the YAML configuration.
	ConfigMapDataKey = "sharding.yaml"
	// DefaultConfigMapName is the default name of the sharding ConfigMap.
	DefaultConfigMapName = "volcano-sharding-configmap"
	// DefaultConfigMapNamespace is the default namespace of the sharding ConfigMap.
	DefaultConfigMapNamespace = "volcano-system"
)

// SchedulerConfigSpec defines the per-scheduler sharding parameters.
// It is used both as the internal representation and as the YAML-serialisable
// form stored in the sharding ConfigMap.
type SchedulerConfigSpec struct {
	// Name is the scheduler name (must match the schedulerName field in NodeShard).
	Name string `json:"name"`
	// Type describes the workload class (e.g. "volcano", "agent").
	Type string `json:"type"`
	// Policy is the policy name (e.g., "allocation-rate").
	Policy string `json:"policy,omitempty"`
	// Arguments holds policy-specific arguments.
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	// CPUUtilizationMin is the lower bound (inclusive) of the CPU utilisation range
	// [0.0, 1.0] that makes a node eligible for this scheduler's shard.
	// Deprecated: use Policy and Arguments instead.
	CPUUtilizationMin float64 `json:"cpuUtilizationMin"`
	// CPUUtilizationMax is the upper bound (inclusive) of the CPU utilisation range.
	// Deprecated: use Policy and Arguments instead.
	CPUUtilizationMax float64 `json:"cpuUtilizationMax"`
	// PreferWarmupNodes indicates whether warmup nodes should be sorted before
	// regular nodes when selecting shard members.
	// Deprecated: use Policy and Arguments instead.
	PreferWarmupNodes bool `json:"preferWarmupNodes"`
	// MinNodes is the minimum number of nodes the shard must contain.
	MinNodes int `json:"minNodes"`
	// MaxNodes is the maximum number of nodes the shard may contain.
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
	}
	return cfg, nil
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
			"Old format: name:type:min_util:max_util:prefer_warmup:min_nodes:max_nodes. "+
			"New format: name:type:policy:min_nodes:max_nodes[:key=val,key=val]. "+
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

// ParseConfig parses the raw config strings into SchedulerConfigs.
// This is used only when ConfigMap-based configuration is not available.
// Supports both old and new formats:
// OLD: "volcano:volcano:0.0:0.6:false:2:100"
// NEW: "volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6,preferWarmupNodes=false"
func (opts *ShardingControllerOptions) ParseConfig() error {
	configs := make([]SchedulerConfigSpec, 0, len(opts.SchedulerConfigsRaw))

	for _, configStr := range opts.SchedulerConfigsRaw {
		parts := strings.Split(configStr, ":")

		var config SchedulerConfigSpec
		var err error

		if len(parts) == 7 {
			// Old format - convert to new format
			config, err = parseOldFormat(parts)
			if err != nil {
				return fmt.Errorf("failed to parse old format config %s: %v", configStr, err)
			}
			klog.V(3).Infof("Parsed old format config for scheduler %s, converting to allocation-rate policy", config.Name)
		} else if len(parts) >= 5 {
			// New format: name:type:policy:minNodes:maxNodes[:args]
			config, err = parseNewFormat(parts)
			if err != nil {
				return fmt.Errorf("failed to parse new format config %s: %v", configStr, err)
			}
			klog.V(3).Infof("Parsed new format config for scheduler %s with policy %s", config.Name, config.Policy)
		} else {
			return fmt.Errorf("invalid scheduler config format: %s, expected 7 parts (old format) or 5+ parts (new format)", configStr)
		}

		configs = append(configs, config)
	}

	opts.SchedulerConfigs = configs
	return nil
}

// parseOldFormat parses old format config: name:type:minUtil:maxUtil:preferWarmup:minNodes:maxNodes
func parseOldFormat(parts []string) (SchedulerConfigSpec, error) {
	// Parse CPU utilization min
	minUtil, err := parseUtilization(parts[2])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid min utilization: %v", err)
	}

	// Parse CPU utilization max
	maxUtil, err := parseUtilization(parts[3])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid max utilization: %v", err)
	}

	// Parse prefer warmup
	preferWarmup, err := strconv.ParseBool(parts[4])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid prefer warmup flag: %v", err)
	}

	// Parse min nodes
	minNodes, err := strconv.Atoi(parts[5])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid min nodes: %v", err)
	}

	// Parse max nodes
	maxNodes, err := strconv.Atoi(parts[6])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid max nodes: %v", err)
	}

	// Convert to new format with allocation-rate policy
	return SchedulerConfigSpec{
		Name:   parts[0],
		Type:   parts[1],
		Policy: "allocation-rate", // Default policy for old format
		Arguments: map[string]interface{}{
			"minCPUUtil":        minUtil,
			"maxCPUUtil":        maxUtil,
			"preferWarmupNodes": preferWarmup,
			"minNodes":          minNodes,
			"maxNodes":          maxNodes,
		},
		// Keep deprecated fields for backwards compatibility
		CPUUtilizationMin: minUtil,
		CPUUtilizationMax: maxUtil,
		PreferWarmupNodes: preferWarmup,
		MinNodes:          minNodes,
		MaxNodes:          maxNodes,
	}, nil
}

// parseNewFormat parses new format config: name:type:policy:minNodes:maxNodes[:args]
func parseNewFormat(parts []string) (SchedulerConfigSpec, error) {
	// Parse min nodes
	minNodes, err := strconv.Atoi(parts[3])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid min nodes: %v", err)
	}

	// Parse max nodes
	maxNodes, err := strconv.Atoi(parts[4])
	if err != nil {
		return SchedulerConfigSpec{}, fmt.Errorf("invalid max nodes: %v", err)
	}

	config := SchedulerConfigSpec{
		Name:   parts[0],
		Type:   parts[1],
		Policy: parts[2],
		Arguments: map[string]interface{}{
			"minNodes": minNodes,
			"maxNodes": maxNodes,
		},
		MinNodes: minNodes,
		MaxNodes: maxNodes,
	}

	// Parse optional arguments: "key1=val1,key2=val2"
	if len(parts) > 5 {
		argPairs := strings.Split(parts[5], ",")
		for _, pair := range argPairs {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				klog.Warningf("Malformed argument pair: %s, expected format key=value", pair)
				continue
			}
			key := strings.TrimSpace(kv[0])
			value := parseArgumentValue(strings.TrimSpace(kv[1]))
			config.Arguments[key] = value
		}
	}

	return config, nil
}

// parseArgumentValue attempts to parse a string into the appropriate type
func parseArgumentValue(s string) interface{} {
	// Try bool
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	// Try int
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}

	// Try float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// Default to string
	return s
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
