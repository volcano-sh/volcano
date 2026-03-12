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
	assert.Equal(t, DefaultConfigMapName, opts.ConfigMapName)
	assert.Equal(t, DefaultConfigMapNamespace, opts.ConfigMapNamespace)
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
