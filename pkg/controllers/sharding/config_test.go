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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseShardingConfig_Valid(t *testing.T) {
	yaml := `
schedulerConfigs:
  - name: agent-scheduler
    type: agent
    cpuUtilizationMin: 0.7
    cpuUtilizationMax: 1.0
    preferWarmupNodes: true
    minNodes: 1
    maxNodes: 100
  - name: volcano
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.69
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 100
shardSyncPeriod: "60s"
enableNodeEventTrigger: true
`
	cfg, err := ParseShardingConfig([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, cfg.SchedulerConfigs, 2)

	// agent-scheduler entry
	agent := cfg.SchedulerConfigs[0]
	assert.Equal(t, "agent-scheduler", agent.Name)
	assert.Equal(t, "agent", agent.Type)
	assert.InDelta(t, 0.7, agent.CPUUtilizationMin, 1e-9)
	assert.InDelta(t, 1.0, agent.CPUUtilizationMax, 1e-9)
	assert.True(t, agent.PreferWarmupNodes)
	assert.Equal(t, 1, agent.MinNodes)
	assert.Equal(t, 100, agent.MaxNodes)

	// volcano entry
	vc := cfg.SchedulerConfigs[1]
	assert.Equal(t, "volcano", vc.Name)
	assert.Equal(t, "volcano", vc.Type)
	assert.InDelta(t, 0.0, vc.CPUUtilizationMin, 1e-9)
	assert.InDelta(t, 0.69, vc.CPUUtilizationMax, 1e-9)
	assert.False(t, vc.PreferWarmupNodes)

	assert.Equal(t, "60s", cfg.ShardSyncPeriod)
	require.NotNil(t, cfg.EnableNodeEventTrigger)
	assert.True(t, *cfg.EnableNodeEventTrigger)
}

func TestParseShardingConfig_OptionalFields(t *testing.T) {
	// shardSyncPeriod and enableNodeEventTrigger are optional
	yaml := `
schedulerConfigs:
  - name: my-scheduler
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.80
    preferWarmupNodes: false
    minNodes: 2
    maxNodes: 50
`
	cfg, err := ParseShardingConfig([]byte(yaml))
	require.NoError(t, err)
	assert.Empty(t, cfg.ShardSyncPeriod)
	assert.Nil(t, cfg.EnableNodeEventTrigger)
}

func TestParseShardingConfig_EmptySchedulerList(t *testing.T) {
	yaml := `
schedulerConfigs: []
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "at least one schedulerConfig")
}

func TestParseShardingConfig_MissingName(t *testing.T) {
	yaml := `
schedulerConfigs:
  - type: agent
    cpuUtilizationMin: 0.5
    cpuUtilizationMax: 1.0
    preferWarmupNodes: true
    minNodes: 1
    maxNodes: 10
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "name must not be empty")
}

func TestParseShardingConfig_CPUMinOutOfRange(t *testing.T) {
	yaml := `
schedulerConfigs:
  - name: bad
    type: volcano
    cpuUtilizationMin: -0.1
    cpuUtilizationMax: 0.9
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 10
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "cpuUtilizationMin")
}

func TestParseShardingConfig_CPUMaxOutOfRange(t *testing.T) {
	yaml := `
schedulerConfigs:
  - name: bad
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 1.1
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 10
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "cpuUtilizationMax")
}

func TestParseShardingConfig_MinGreaterThanMax(t *testing.T) {
	yaml := `
schedulerConfigs:
  - name: bad
    type: volcano
    cpuUtilizationMin: 0.9
    cpuUtilizationMax: 0.5
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 10
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "cpuUtilizationMin")
}

func TestParseShardingConfig_MaxNodesLessThanMinNodes(t *testing.T) {
	yaml := `
schedulerConfigs:
  - name: bad
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.9
    preferWarmupNodes: false
    minNodes: 10
    maxNodes: 5
`
	_, err := ParseShardingConfig([]byte(yaml))
	assert.ErrorContains(t, err, "maxNodes")
}

func TestParseShardingConfig_InvalidYAML(t *testing.T) {
	_, err := ParseShardingConfig([]byte("{invalid yaml{{"))
	assert.Error(t, err)
}

// TestNewShardingControllerOptions_Defaults checks that the flag-based defaults
// parse without error and produce sensible scheduler configs.
func TestNewShardingControllerOptions_Defaults(t *testing.T) {
	opts := NewShardingControllerOptions()
	assert.Equal(t, DefaultConfigMapName, opts.ConfigMapName, "ConfigMapName should default to "+DefaultConfigMapName)
	assert.Equal(t, DefaultConfigMapNamespace, opts.ConfigMapNamespace, "ConfigMapNamespace should default to "+DefaultConfigMapNamespace)
	require.Len(t, opts.SchedulerConfigs, 2)

	// volcano entry
	vc := opts.SchedulerConfigs[0]
	assert.Equal(t, "volcano", vc.Name)
	assert.InDelta(t, 0.0, vc.CPUUtilizationMin, 1e-9)
	assert.InDelta(t, 0.6, vc.CPUUtilizationMax, 1e-9)

	// agent-scheduler entry
	agent := opts.SchedulerConfigs[1]
	assert.Equal(t, "agent-scheduler", agent.Name)
	assert.InDelta(t, 0.7, agent.CPUUtilizationMin, 1e-9)
	assert.InDelta(t, 1.0, agent.CPUUtilizationMax, 1e-9)
	assert.True(t, agent.PreferWarmupNodes)
}

func TestParseConfigOldFormat(t *testing.T) {
	opts := &ShardingControllerOptions{
		SchedulerConfigsRaw: []string{
			"volcano:volcano:0.0:0.6:false:2:100",
			"agent-scheduler:agent:0.7:1.0:true:2:100",
		},
	}

	err := opts.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if len(opts.SchedulerConfigs) != 2 {
		t.Fatalf("ParseConfig() parsed %d configs, want 2", len(opts.SchedulerConfigs))
	}

	// Check first config
	config1 := opts.SchedulerConfigs[0]
	if config1.Name != "volcano" {
		t.Errorf("config1.Name = %v, want volcano", config1.Name)
	}
	if config1.Type != "volcano" {
		t.Errorf("config1.Type = %v, want volcano", config1.Type)
	}
	if len(config1.Policies) != 1 || config1.Policies[0].Name != "allocation-rate" {
		t.Errorf("config1.Policies = %#v, want [allocation-rate]", config1.Policies)
	}
	if config1.Arguments["minCPUUtil"] != 0.0 {
		t.Errorf("config1.Arguments[minCPUUtil] = %v, want 0.0", config1.Arguments["minCPUUtil"])
	}
	if config1.Arguments["maxCPUUtil"] != 0.6 {
		t.Errorf("config1.Arguments[maxCPUUtil] = %v, want 0.6", config1.Arguments["maxCPUUtil"])
	}
	if _, ok := config1.Arguments["preferWarmupNodes"]; ok {
		t.Errorf("config1.Arguments[preferWarmupNodes] should be absent for allocation-rate; "+
			"got %v", config1.Arguments["preferWarmupNodes"])
	}
	if config1.MinNodes != 2 {
		t.Errorf("config1.MinNodes = %v, want 2", config1.MinNodes)
	}
	if config1.MaxNodes != 100 {
		t.Errorf("config1.MaxNodes = %v, want 100", config1.MaxNodes)
	}

	// Check second config
	config2 := opts.SchedulerConfigs[1]
	if config2.Name != "agent-scheduler" {
		t.Errorf("config2.Name = %v, want agent-scheduler", config2.Name)
	}
	if len(config2.Policies) != 1 || config2.Policies[0].Name != "allocation-rate" {
		t.Errorf("config2.Policies = %#v, want [allocation-rate]", config2.Policies)
	}
	if config2.Arguments["minCPUUtil"] != 0.7 {
		t.Errorf("config2.Arguments[minCPUUtil] = %v, want 0.7", config2.Arguments["minCPUUtil"])
	}
	if _, ok := config2.Arguments["preferWarmupNodes"]; ok {
		t.Errorf("config2.Arguments[preferWarmupNodes] should be absent for allocation-rate; "+
			"got %v", config2.Arguments["preferWarmupNodes"])
	}
}

func TestParseConfigRejectsNewFormat(t *testing.T) {
	opts := &ShardingControllerOptions{
		SchedulerConfigsRaw: []string{
			"volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6,preferWarmupNodes=false",
			"agent:agent:capability:2:50:maxCapacityPercent=0.30",
			"warmup-sched:warmup:warmup:5:100:allowNonWarmup=true",
		},
	}

	err := opts.ParseConfig()
	if err == nil {
		t.Fatalf("ParseConfig() expected error for new format, got nil")
	}
	assert.ErrorContains(t, err, "expected 7 colon-separated parts")
}

func TestParseConfigMixedFormatsRejected(t *testing.T) {
	opts := &ShardingControllerOptions{
		SchedulerConfigsRaw: []string{
			"volcano:volcano:0.0:0.6:false:2:100",                 // Old format
			"agent:agent:capability:2:50:maxCapacityPercent=0.30", // New format
		},
	}

	err := opts.ParseConfig()
	if err == nil {
		t.Fatalf("ParseConfig() expected error for mixed formats, got nil")
	}
	assert.ErrorContains(t, err, "expected 7 colon-separated parts")
}

func TestParseConfigInvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "too few parts",
			config: "volcano:volcano:0.0:0.6",
		},
		{
			name:   "invalid min util",
			config: "volcano:volcano:invalid:0.6:false:2:100",
		},
		{
			name:   "invalid max util",
			config: "volcano:volcano:0.0:invalid:false:2:100",
		},
		{
			name:   "invalid prefer warmup",
			config: "volcano:volcano:0.0:0.6:invalid:2:100",
		},
		{
			name:   "invalid min nodes",
			config: "volcano:volcano:0.0:0.6:false:invalid:100",
		},
		{
			name:   "invalid max nodes",
			config: "volcano:volcano:0.0:0.6:false:2:invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ShardingControllerOptions{
				SchedulerConfigsRaw: []string{tt.config},
			}

			err := opts.ParseConfig()
			if err == nil {
				t.Errorf("ParseConfig() expected error for %s, got nil", tt.name)
			}
		})
	}
}
