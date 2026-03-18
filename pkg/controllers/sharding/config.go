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
)

// SchedulerConfigSpec defines the per-scheduler sharding parameters.
// It is used both as the internal representation and as the YAML-serialisable
// form stored in the sharding ConfigMap.
type SchedulerConfigSpec struct {
	// Name is the scheduler name (must match the schedulerName field in NodeShard).
	Name string `json:"name"`
	// Type describes the workload class (e.g. "volcano", "agent").
	Type string `json:"type"`
	// CPUUtilizationMin is the lower bound (inclusive) of the CPU utilisation range
	// [0.0, 1.0] that makes a node eligible for this scheduler's shard.
	CPUUtilizationMin float64 `json:"cpuUtilizationMin"`
	// CPUUtilizationMax is the upper bound (inclusive) of the CPU utilisation range.
	CPUUtilizationMax float64 `json:"cpuUtilizationMax"`
	// PreferWarmupNodes indicates whether warmup nodes should be sorted before
	// regular nodes when selecting shard members.
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
		ConfigMapName:      "",
		ConfigMapNamespace: "",
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
			"Scheduler configurations in format: name:type:min_util:max_util:prefer_warmup:min_nodes:max_nodes. "+
			"Used only when no valid sharding ConfigMap is available.")

	fs.DurationVar(&opts.ShardSyncPeriod, "shard-sync-period", 60*time.Second,
		"Period for shard synchronization")

	fs.BoolVar(&opts.EnableNodeEventTrigger, "enable-node-event-trigger", true,
		"Enable node event trigger for shard updates")

	fs.StringVar(&opts.ConfigMapName, "sharding-configmap", "",
		"Name of the ConfigMap that contains sharding configuration (key: "+ConfigMapDataKey+"). "+
			"When set, ConfigMap-based config takes precedence over --scheduler-configs flags and "+
			"the controller watches the ConfigMap for live updates.")

	fs.StringVar(&opts.ConfigMapNamespace, "sharding-configmap-namespace", "",
		"Namespace of the sharding configuration ConfigMap. Required when --sharding-configmap is set.")
}

// ParseConfig parses the raw colon-separated config strings into SchedulerConfigs.
// This is used only when ConfigMap-based configuration is not available.
func (opts *ShardingControllerOptions) ParseConfig() error {
	configs := make([]SchedulerConfigSpec, 0, len(opts.SchedulerConfigsRaw))

	for _, configStr := range opts.SchedulerConfigsRaw {
		parts := strings.Split(configStr, ":")
		if len(parts) != 7 {
			return fmt.Errorf("invalid scheduler config format: %s, expected 7 parts separated by ':'", configStr)
		}

		// Parse CPU utilization min
		minUtil, err := parseUtilization(parts[2])
		if err != nil {
			return fmt.Errorf("invalid min utilization in %s: %v", configStr, err)
		}

		// Parse CPU utilization max
		maxUtil, err := parseUtilization(parts[3])
		if err != nil {
			return fmt.Errorf("invalid max utilization in %s: %v", configStr, err)
		}

		// Parse prefer warmup
		preferWarmup, err := strconv.ParseBool(parts[4])
		if err != nil {
			return fmt.Errorf("invalid prefer warmup flag in %s: %v", configStr, err)
		}

		// Parse min nodes
		minNodes, err := strconv.Atoi(parts[5])
		if err != nil {
			return fmt.Errorf("invalid min nodes in %s: %v", configStr, err)
		}

		// Parse max nodes
		maxNodes, err := strconv.Atoi(parts[6])
		if err != nil {
			return fmt.Errorf("invalid max nodes in %s: %v", configStr, err)
		}

		configs = append(configs, SchedulerConfigSpec{
			Name:              parts[0],
			Type:              parts[1],
			CPUUtilizationMin: minUtil,
			CPUUtilizationMax: maxUtil,
			PreferWarmupNodes: preferWarmup,
			MinNodes:          minNodes,
			MaxNodes:          maxNodes,
		})
	}

	opts.SchedulerConfigs = configs
	return nil
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
