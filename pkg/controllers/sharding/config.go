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
)

type SchedulerConfigSpec struct {
	Name              string  // Scheduler name
	Type              string  // Workload type
	CPUUtilizationMin float64 // Minimum CPU utilization threshold
	CPUUtilizationMax float64 // Maximum CPU utilization threshold
	PreferWarmupNodes bool    // Whether to prefer warmup nodes
	MinNodes          int     // Minimum number of nodes
	MaxNodes          int     // Maximum number of nodes
}

type ShardingControllerOptions struct {
	SchedulerConfigsRaw    []string
	SchedulerConfigs       []SchedulerConfigSpec
	ShardSyncPeriod        time.Duration
	EnableNodeEventTrigger bool
}

func NewShardingControllerOptions() ShardingControllerOptions {
	controllerOptions := ShardingControllerOptions{
		// Default scheduler configs will be added by AddFlags
		ShardSyncPeriod:        60 * time.Second,
		EnableNodeEventTrigger: true,
		SchedulerConfigsRaw: []string{
			"volcano:volcano:0.0:0.6:false:2:100",
			"agent-scheduler:agent:0.7:1.0:true:2:100",
		},
	}
	if err := controllerOptions.ParseConfig(); err != nil {
		klog.V(4).Infof("cannot parse scheduler configurations: %s correctly, please check and fix it correctly!", strings.Join(controllerOptions.SchedulerConfigsRaw, ", "))
	}
	return controllerOptions
}

// AddFlags adds flags to the flag set using pflag pattern
func (opts *ShardingControllerOptions) AddFlags(fs *pflag.FlagSet) {
	// Default scheduler configs
	defaultConfigs := []string{
		"volcano:volcano:0.0:0.6:false:2:100",
		"agent-scheduler:agent:0.7:1.0:true:2:100",
	}

	fs.StringSliceVar(&opts.SchedulerConfigsRaw, "scheduler-configs", defaultConfigs,
		"Scheduler configurations in format: name:type:min_util:max_util:prefer_warmup:min_nodes:max_nodes")

	fs.DurationVar(&opts.ShardSyncPeriod, "shard-sync-period", 60*time.Second,
		"Period for shard synchronization")

	fs.BoolVar(&opts.EnableNodeEventTrigger, "enable-node-event-trigger", true,
		"Enable node event trigger for shard updates")
}

// ParseConfig parses the raw config strings into scheduler configs
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

// parseUtilization parses utilization string to float64
func parseUtilization(s string) (float64, error) {
	if s == "inf" || s == "infinity" {
		return 1.0, nil
	}
	if s == "-inf" || s == "-infinity" {
		return 0.0, nil
	}
	return strconv.ParseFloat(s, 64)
}
