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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestReloadFromConfigMap_UpdatesSchedulerConfigs(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Verify initial state: 2 default scheduler configs
	controller.configMu.RLock()
	initialCount := len(controller.schedulerConfigs)
	controller.configMu.RUnlock()
	assert.Equal(t, 2, initialCount, "should start with 2 default scheduler configs")

	// Simulate a ConfigMap update with 3 schedulers
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `
schedulerConfigs:
  - name: volcano
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.5
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 50
  - name: agent-scheduler
    type: agent
    cpuUtilizationMin: 0.6
    cpuUtilizationMax: 1.0
    preferWarmupNodes: true
    minNodes: 1
    maxNodes: 50
  - name: batch-scheduler
    type: batch
    cpuUtilizationMin: 0.3
    cpuUtilizationMax: 0.7
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 20
`,
		},
	}

	controller.reloadFromConfigMap(cm)

	// Verify configs were updated
	controller.configMu.RLock()
	configs := make([]SchedulerConfig, len(controller.schedulerConfigs))
	copy(configs, controller.schedulerConfigs)
	controller.configMu.RUnlock()

	require.Len(t, configs, 3, "should have 3 scheduler configs after reload")
	assert.Equal(t, "volcano", configs[0].Name)
	assert.InDelta(t, 0.5, configs[0].ShardStrategy.CPUUtilizationRange.Max, 1e-9)
	assert.Equal(t, "agent-scheduler", configs[1].Name)
	assert.InDelta(t, 0.6, configs[1].ShardStrategy.CPUUtilizationRange.Min, 1e-9)
	assert.Equal(t, "batch-scheduler", configs[2].Name)
	assert.Equal(t, "batch", configs[2].Type)
}

func TestReloadFromConfigMap_UpdatesShardSyncPeriod(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Record initial period
	initialPeriod := controller.getShardSyncPeriod()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `
schedulerConfigs:
  - name: volcano
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.6
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 100
shardSyncPeriod: "30s"
`,
		},
	}

	controller.reloadFromConfigMap(cm)

	newPeriod := controller.getShardSyncPeriod()
	assert.Equal(t, 30*time.Second, newPeriod, "shardSyncPeriod should be updated to 30s")
	assert.NotEqual(t, initialPeriod, newPeriod, "shardSyncPeriod should differ from initial")
}

func TestReloadFromConfigMap_UpdatesEnableNodeEventTrigger(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Default is true
	controller.configMu.RLock()
	initialValue := controller.controllerOptions.EnableNodeEventTrigger
	controller.configMu.RUnlock()
	assert.True(t, initialValue, "EnableNodeEventTrigger should default to true")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `
schedulerConfigs:
  - name: volcano
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.6
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 100
enableNodeEventTrigger: false
`,
		},
	}

	controller.reloadFromConfigMap(cm)

	controller.configMu.RLock()
	updatedValue := controller.controllerOptions.EnableNodeEventTrigger
	controller.configMu.RUnlock()
	assert.False(t, updatedValue, "EnableNodeEventTrigger should be updated to false")
}

func TestReloadFromConfigMap_InvalidYAMLKeepsPreviousConfig(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Record initial configs
	controller.configMu.RLock()
	initialConfigs := make([]SchedulerConfig, len(controller.schedulerConfigs))
	copy(initialConfigs, controller.schedulerConfigs)
	controller.configMu.RUnlock()

	// Send invalid YAML
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `{invalid yaml{{`,
		},
	}

	controller.reloadFromConfigMap(cm)

	// Configs should remain unchanged
	controller.configMu.RLock()
	currentConfigs := make([]SchedulerConfig, len(controller.schedulerConfigs))
	copy(currentConfigs, controller.schedulerConfigs)
	controller.configMu.RUnlock()

	assert.Equal(t, len(initialConfigs), len(currentConfigs), "config count should not change on invalid YAML")
	for i := range initialConfigs {
		assert.Equal(t, initialConfigs[i].Name, currentConfigs[i].Name, "config names should remain unchanged")
	}
}

func TestReloadFromConfigMap_MissingKeySkipsReload(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	controller.configMu.RLock()
	initialCount := len(controller.schedulerConfigs)
	controller.configMu.RUnlock()

	// ConfigMap without the expected key
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			"wrong-key.yaml": `schedulerConfigs: []`,
		},
	}

	controller.reloadFromConfigMap(cm)

	controller.configMu.RLock()
	currentCount := len(controller.schedulerConfigs)
	controller.configMu.RUnlock()
	assert.Equal(t, initialCount, currentCount, "config should not change when key is missing")
}

func TestReloadFromConfigMap_InvalidatesAssignmentCache(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Populate the assignment cache with a dummy entry
	controller.cacheMutex.Lock()
	controller.assignmentCache.Assignments["volcano"] = &ShardAssignment{
		SchedulerName: "volcano",
		NodesDesired:  []string{"node-1"},
	}
	controller.cacheMutex.Unlock()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `
schedulerConfigs:
  - name: volcano
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.6
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 100
`,
		},
	}

	controller.reloadFromConfigMap(cm)

	// Assignment cache should be cleared
	controller.cacheMutex.Lock()
	cacheLen := len(controller.assignmentCache.Assignments)
	controller.cacheMutex.Unlock()
	assert.Equal(t, 0, cacheLen, "assignment cache should be invalidated after reload")
}

func TestReloadFromConfigMap_RecreatesShardingManager(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	// Record original manager pointer
	controller.configMu.RLock()
	originalManager := controller.shardingManager
	controller.configMu.RUnlock()
	require.NotNil(t, originalManager)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `
schedulerConfigs:
  - name: new-scheduler
    type: volcano
    cpuUtilizationMin: 0.0
    cpuUtilizationMax: 0.8
    preferWarmupNodes: false
    minNodes: 1
    maxNodes: 200
`,
		},
	}

	controller.reloadFromConfigMap(cm)

	controller.configMu.RLock()
	newManager := controller.shardingManager
	controller.configMu.RUnlock()
	assert.NotSame(t, originalManager, newManager, "shardingManager should be recreated on config reload")
}

func TestReloadFromConfigMap_ValidationRejectsEmptySchedulerList(t *testing.T) {
	opt := &TestControllerOption{
		InitialObjects: []runtime.Object{
			CreateTestNode("node-1", "8", false, nil),
		},
	}
	testCtrl := newTestShardingController(t, opt)
	defer closeTestShardingController(testCtrl)
	controller := testCtrl.Controller

	controller.configMu.RLock()
	initialCount := len(controller.schedulerConfigs)
	controller.configMu.RUnlock()

	// Empty scheduler list should fail validation
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			ConfigMapDataKey: `schedulerConfigs: []`,
		},
	}

	controller.reloadFromConfigMap(cm)

	// Config should remain unchanged
	controller.configMu.RLock()
	currentCount := len(controller.schedulerConfigs)
	controller.configMu.RUnlock()
	assert.Equal(t, initialCount, currentCount, "config should not change on validation failure")
}
