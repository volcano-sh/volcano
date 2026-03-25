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
	if config1.Policy != "allocation-rate" {
		t.Errorf("config1.Policy = %v, want allocation-rate", config1.Policy)
	}
	if config1.Arguments["minCPUUtil"] != 0.0 {
		t.Errorf("config1.Arguments[minCPUUtil] = %v, want 0.0", config1.Arguments["minCPUUtil"])
	}
	if config1.Arguments["maxCPUUtil"] != 0.6 {
		t.Errorf("config1.Arguments[maxCPUUtil] = %v, want 0.6", config1.Arguments["maxCPUUtil"])
	}
	if config1.Arguments["preferWarmupNodes"] != false {
		t.Errorf("config1.Arguments[preferWarmupNodes] = %v, want false", config1.Arguments["preferWarmupNodes"])
	}
	if config1.Arguments["minNodes"] != 2 {
		t.Errorf("config1.Arguments[minNodes] = %v, want 2", config1.Arguments["minNodes"])
	}
	if config1.Arguments["maxNodes"] != 100 {
		t.Errorf("config1.Arguments[maxNodes] = %v, want 100", config1.Arguments["maxNodes"])
	}

	// Check second config
	config2 := opts.SchedulerConfigs[1]
	if config2.Name != "agent-scheduler" {
		t.Errorf("config2.Name = %v, want agent-scheduler", config2.Name)
	}
	if config2.Arguments["minCPUUtil"] != 0.7 {
		t.Errorf("config2.Arguments[minCPUUtil] = %v, want 0.7", config2.Arguments["minCPUUtil"])
	}
	if config2.Arguments["preferWarmupNodes"] != true {
		t.Errorf("config2.Arguments[preferWarmupNodes] = %v, want true", config2.Arguments["preferWarmupNodes"])
	}
}

func TestParseConfigNewFormat(t *testing.T) {
	opts := &ShardingControllerOptions{
		SchedulerConfigsRaw: []string{
			"volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6,preferWarmupNodes=false",
			"agent:agent:capability:2:50:maxCapacityPercent=0.30",
			"warmup-sched:warmup:warmup:5:100:allowNonWarmup=true",
		},
	}

	err := opts.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if len(opts.SchedulerConfigs) != 3 {
		t.Fatalf("ParseConfig() parsed %d configs, want 3", len(opts.SchedulerConfigs))
	}

	// Check first config (allocation-rate)
	config1 := opts.SchedulerConfigs[0]
	if config1.Name != "volcano" {
		t.Errorf("config1.Name = %v, want volcano", config1.Name)
	}
	if config1.Policy != "allocation-rate" {
		t.Errorf("config1.Policy = %v, want allocation-rate", config1.Policy)
	}
	if config1.Arguments["minNodes"] != 2 {
		t.Errorf("config1.Arguments[minNodes] = %v, want 2", config1.Arguments["minNodes"])
	}
	if config1.Arguments["maxNodes"] != 100 {
		t.Errorf("config1.Arguments[maxNodes] = %v, want 100", config1.Arguments["maxNodes"])
	}
	if config1.Arguments["minCPUUtil"] != 0.0 {
		t.Errorf("config1.Arguments[minCPUUtil] = %v, want 0.0", config1.Arguments["minCPUUtil"])
	}
	if config1.Arguments["maxCPUUtil"] != 0.6 {
		t.Errorf("config1.Arguments[maxCPUUtil] = %v, want 0.6", config1.Arguments["maxCPUUtil"])
	}

	// Check second config (capability)
	config2 := opts.SchedulerConfigs[1]
	if config2.Name != "agent" {
		t.Errorf("config2.Name = %v, want agent", config2.Name)
	}
	if config2.Policy != "capability" {
		t.Errorf("config2.Policy = %v, want capability", config2.Policy)
	}
	if config2.Arguments["maxCapacityPercent"] != 0.30 {
		t.Errorf("config2.Arguments[maxCapacityPercent] = %v, want 0.30", config2.Arguments["maxCapacityPercent"])
	}

	// Check third config (warmup)
	config3 := opts.SchedulerConfigs[2]
	if config3.Name != "warmup-sched" {
		t.Errorf("config3.Name = %v, want warmup-sched", config3.Name)
	}
	if config3.Policy != "warmup" {
		t.Errorf("config3.Policy = %v, want warmup", config3.Policy)
	}
	if config3.Arguments["allowNonWarmup"] != true {
		t.Errorf("config3.Arguments[allowNonWarmup] = %v, want true", config3.Arguments["allowNonWarmup"])
	}
}

func TestParseConfigMixedFormats(t *testing.T) {
	opts := &ShardingControllerOptions{
		SchedulerConfigsRaw: []string{
			"volcano:volcano:0.0:0.6:false:2:100",                 // Old format
			"agent:agent:capability:2:50:maxCapacityPercent=0.30", // New format
		},
	}

	err := opts.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if len(opts.SchedulerConfigs) != 2 {
		t.Fatalf("ParseConfig() parsed %d configs, want 2", len(opts.SchedulerConfigs))
	}

	// Old format should be converted to allocation-rate
	if opts.SchedulerConfigs[0].Policy != "allocation-rate" {
		t.Errorf("Old format config policy = %v, want allocation-rate", opts.SchedulerConfigs[0].Policy)
	}

	// New format should be parsed correctly
	if opts.SchedulerConfigs[1].Policy != "capability" {
		t.Errorf("New format config policy = %v, want capability", opts.SchedulerConfigs[1].Policy)
	}
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

func TestParseArgumentValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{"bool true", "true", true},
		{"bool false", "false", false},
		{"int positive", "42", 42},
		{"int negative", "-10", -10},
		{"float", "0.5", 0.5},
		{"string", "hello", "hello"},
		{"string with spaces", "hello world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArgumentValue(tt.input)
			if result != tt.expected {
				t.Errorf("parseArgumentValue(%s) = %v (type %T), want %v (type %T)",
					tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}
