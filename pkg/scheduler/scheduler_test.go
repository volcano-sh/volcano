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

package scheduler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/hami"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	_ "volcano.sh/volcano/pkg/scheduler/plugins"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare"
)

func buildAscendTestNode(name string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"hami.io/node-register-Ascend310P": `[{"id":"dev-0","devmem":21527,"devcore":8,"type":"Ascend310P"}]`,
			},
		},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{
				v1.ResourceName("huawei.com/Ascend310P"): resource.MustParse("1"),
			},
		},
	}
}

// TestIssue5846_OriginalOrderingBug reproduces the ordering race in issue #5846:
// When NodeInfo is initialized before Plugin.Init() sets AscendHAMiVNPUEnable,
// cached nodes lack AscendDevices, and subsequent Clone() overwrites Others with the empty map.
func TestIssue5846_OriginalOrderingBug(t *testing.T) {
	// Simulate state before fix: HAMi is not enabled at startup
	hami.AscendHAMiVNPUEnable = false
	node := buildAscendTestNode("ascend-node-bug")

	// 1. Informer handler creates NodeInfo before plugin init
	cachedNode := api.NewNodeInfo(node)
	assert.Nil(t, cachedNode.Others["Ascend310P"], "pre-condition: cached node lacks Ascend310P devices when HAMi is disabled")

	// 2. Later, a scheduling session runs and sets HAMi enable
	hami.AscendHAMiVNPUEnable = true
	defer func() { hami.AscendHAMiVNPUEnable = false }()

	// 3. Cache snapshot clones the cached node
	clonedNode := cachedNode.Clone()

	// In the original bug, Clone() overwrote Others with CloneOthers() from cachedNode,
	// so clonedNode.Others["Ascend310P"] remained nil even though HAMi is now enabled.
	assert.Nil(t, clonedNode.Others["Ascend310P"], "original bug: cloned node still lacks Ascend devices because cached node had none")
}

// TestIssue5846_EarlyPluginInitFix verifies the fix:
// When initPlugins() is executed before cache.Run() / NewNodeInfo(),
// AscendHAMiVNPUEnable is set, cached nodes receive AscendDevices, and cloned session nodes retain them.
func TestIssue5846_EarlyPluginInitFix(t *testing.T) {
	hami.AscendHAMiVNPUEnable = false
	defer func() { hami.AscendHAMiVNPUEnable = false }()

	enablePredicate := true
	sched := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             deviceshare.PluginName,
						EnabledPredicate: &enablePredicate,
						Arguments: map[string]interface{}{
							deviceshare.AscendHAMiVNPUEnable: true,
						},
					},
				},
			},
		},
	}

	// 1. Run early plugin initialization before cache starts
	err := sched.initPlugins()
	assert.NoError(t, err, "sched.initPlugins() should succeed")
	assert.True(t, hami.AscendHAMiVNPUEnable, "AscendHAMiVNPUEnable should be true after initPlugins()")

	// 2. Cache informers create NodeInfo during startup sync
	node := buildAscendTestNode("ascend-node-fixed")
	cachedNode := api.NewNodeInfo(node)
	assert.NotNil(t, cachedNode.Others["Ascend310P"], "cached node must contain Ascend310P devices")

	// 3. Session opens and snapshots the cached node
	clonedNode := cachedNode.Clone()
	assert.NotNil(t, clonedNode.Others["Ascend310P"], "cloned session node must retain Ascend310P devices")

	dev, ok := clonedNode.Others["Ascend310P"].(api.Devices)
	assert.True(t, ok, "device in cloned node must implement api.Devices")
	assert.NotNil(t, dev)
}

// TestIssue5846_DisabledConfiguration verifies that when AscendHAMiVNPUEnable is disabled,
// initPlugins() does not enable it and cached nodes do not receive Ascend devices.
func TestIssue5846_DisabledConfiguration(t *testing.T) {
	hami.AscendHAMiVNPUEnable = false

	sched := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name: deviceshare.PluginName,
						Arguments: map[string]interface{}{
							deviceshare.AscendHAMiVNPUEnable: false,
						},
					},
				},
			},
		},
	}

	err := sched.initPlugins()
	assert.NoError(t, err)
	assert.False(t, hami.AscendHAMiVNPUEnable, "AscendHAMiVNPUEnable must remain false when disabled in config")

	node := buildAscendTestNode("ascend-node-disabled")
	cachedNode := api.NewNodeInfo(node)
	assert.Nil(t, cachedNode.Others["Ascend310P"], "cached node should not have Ascend310P when disabled")
}

// TestIssue5846_MissingAscendDevices_FailClosed verifies that when a pod requests Ascend
// devices, but the candidate node is missing AscendDevices in Others (or has 0 devices),
// DeviceShare fails closed (rejects the node) rather than failing open (allowing it without enforcement).
func TestIssue5846_MissingAscendDevices_FailClosed(t *testing.T) {
	deviceshare.ResetForTesting()
	hami.AscendHAMiVNPUEnable = false
	defer func() {
		deviceshare.ResetForTesting()
		hami.AscendHAMiVNPUEnable = false
	}()

	enablePredicate := true
	sched := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             deviceshare.PluginName,
						EnabledPredicate: &enablePredicate,
						Arguments: map[string]interface{}{
							deviceshare.AscendHAMiVNPUEnable: true,
						},
					},
				},
			},
		},
	}

	err := sched.initPlugins()
	assert.NoError(t, err)

	// 1. Pod requesting Ascend 310P device
	ascendPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ascend-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "worker",
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							"huawei.com/Ascend310P": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
	ascendTask := api.NewTaskInfo(ascendPod)

	// 2. Standard CPU pod without device requests
	cpuPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpu-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "worker",
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
	cpuTask := api.NewTaskInfo(cpuPod)

	// 3. Node missing Ascend devices in Others
	nodeWithoutAscend := api.NewNodeInfo(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cpu-node",
		},
	})
	delete(nodeWithoutAscend.Others, "Ascend310P")

	// 4. Open session to register predicate
	mockCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{nodeWithoutAscend.Name: nodeWithoutAscend},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
	}
	ssn := framework.OpenSession(mockCache, sched.plugins, nil)
	defer framework.CloseSession(ssn)

	// 5. Ascend pod on node missing AscendDevices MUST FAIL CLOSED (return error)
	err = ssn.PredicateFn(ascendTask, nodeWithoutAscend)
	assert.Error(t, err, "ascend pod must NOT fit on node missing AscendDevices (must fail closed)")

	// 6. CPU pod on node missing AscendDevices MUST FIT
	err = ssn.PredicateFn(cpuTask, nodeWithoutAscend)
	assert.NoError(t, err, "cpu pod should fit on node without AscendDevices")

	// 7. Node with Ascend310P entry but 0 devices MUST ALSO FAIL CLOSED
	emptyAscendNode := api.NewNodeInfo(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "empty-ascend-node",
		},
	})
	emptyAscendNode.Others["Ascend310P"] = &hami.AscendDevices{
		Type:    "Ascend310P",
		Devices: map[string]*hami.AscendDevice{},
	}
	err = ssn.PredicateFn(ascendTask, emptyAscendNode)
	assert.Error(t, err, "ascend pod must NOT fit on node with 0 ascend devices (must fail closed)")
}

// TestScheduler_InitPlugins_ErrorHandling verifies error handling in initPlugins.
func TestScheduler_InitPlugins_ErrorHandling(t *testing.T) {
	// Unknown plugin builder
	schedUnknown := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name: "non-existent-plugin-xyz",
					},
				},
			},
		},
	}
	err := schedUnknown.initPlugins()
	assert.NoError(t, err, "unknown plugins must be skipped, matching OpenSession")
	assert.True(t, schedUnknown.pluginsInitialized)

	// Conflicting arguments in deviceshare
	schedConflict := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name: deviceshare.PluginName,
						Arguments: map[string]interface{}{
							deviceshare.GPUSharingPredicate: true,
							deviceshare.GPUNumberPredicate:  true,
						},
					},
				},
			},
		},
	}
	errConflict := schedConflict.initPlugins()
	assert.Error(t, errConflict, "expected error on conflicting deviceshare arguments")
	assert.False(t, schedConflict.pluginsInitialized, "must stay false when Init fails")
}

// TestScheduler_SessionCreatesFreshPluginInstances verifies that OpenSession
// continues to create fresh plugin instances per session rather than reusing early instances.
func TestScheduler_SessionCreatesFreshPluginInstances(t *testing.T) {
	var createdInstances []framework.Plugin

	testPluginName := "test-fresh-instance-plugin"
	framework.RegisterPluginBuilder(testPluginName, func(args framework.Arguments) framework.Plugin {
		p := &testInstanceTrackerPlugin{id: len(createdInstances) + 1}
		createdInstances = append(createdInstances, p)
		return p
	})
	defer framework.UnregisterPluginBuilder(testPluginName)

	sched := &Scheduler{
		plugins: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: testPluginName},
				},
			},
		},
	}

	// 1. Startup phase calls initPlugins()
	err := sched.initPlugins()
	assert.NoError(t, err)
	assert.Equal(t, 1, len(createdInstances), "initPlugins() should instantiate one early instance")

	// 2. OpenSession should create a fresh instance
	mockCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
	}
	ssn := framework.OpenSession(mockCache, sched.plugins, nil)
	assert.Equal(t, 2, len(createdInstances), "OpenSession should create a second, fresh instance")
	assert.NotEqual(t, createdInstances[0], createdInstances[1], "session instance must not be identical to startup instance")

	framework.CloseSession(ssn)
}

type testInstanceTrackerPlugin struct {
	id int
}

func (p *testInstanceTrackerPlugin) Name() string                      { return "test-fresh-instance-plugin" }
func (p *testInstanceTrackerPlugin) Init() error                       { return nil }
func (p *testInstanceTrackerPlugin) OnSessionOpen(*framework.Session)  {}
func (p *testInstanceTrackerPlugin) OnSessionClose(*framework.Session) {}

func createTempConfigFile(t *testing.T, content string) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "volcano-scheduler.conf")
	err := os.WriteFile(path, []byte(content), 0644)
	assert.NoError(t, err)
	return path
}

// TestScheduler_Reload_RejectDeviceShareRemoval tests that hot-removing DeviceShare
// on configuration reload is rejected, retaining the previous configuration.
func TestScheduler_Reload_RejectDeviceShareRemoval(t *testing.T) {
	deviceshare.ResetForTesting()
	defer deviceshare.ResetForTesting()

	initialConf := `
actions: "allocate"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
`
	confPath := createTempConfigFile(t, initialConf)
	sched := &Scheduler{
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()
	err := sched.initPlugins()
	assert.NoError(t, err)
	assert.True(t, sched.pluginsInitialized)
	assert.Equal(t, 1, len(sched.plugins[0].Plugins))
	assert.Equal(t, deviceshare.PluginName, sched.plugins[0].Plugins[0].Name)

	// Overwrite config removing deviceshare
	newConf := `
actions: "allocate"
tiers:
- plugins:
  - name: binpack
`
	err = os.WriteFile(confPath, []byte(newConf), 0644)
	assert.NoError(t, err)

	// Attempt reload
	sched.loadSchedulerConf()

	// Verify reload was rejected and previous configuration is retained
	assert.Equal(t, 1, len(sched.plugins[0].Plugins), "previous configuration must be retained")
	assert.Equal(t, deviceshare.PluginName, sched.plugins[0].Plugins[0].Name, "deviceshare must not be removed on reload")
}

// TestScheduler_Reload_RejectDeviceShareAddition tests that hot-adding DeviceShare
// on configuration reload when it was not enabled at startup is rejected.
func TestScheduler_Reload_RejectDeviceShareAddition(t *testing.T) {
	deviceshare.ResetForTesting()
	defer deviceshare.ResetForTesting()

	initialConf := `
actions: "allocate"
tiers:
- plugins:
  - name: binpack
`
	confPath := createTempConfigFile(t, initialConf)
	sched := &Scheduler{
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()
	err := sched.initPlugins()
	assert.NoError(t, err)
	assert.True(t, sched.pluginsInitialized)

	// Overwrite config adding deviceshare
	newConf := `
actions: "allocate"
tiers:
- plugins:
  - name: binpack
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
`
	err = os.WriteFile(confPath, []byte(newConf), 0644)
	assert.NoError(t, err)

	sched.loadSchedulerConf()

	// Verify reload was rejected and previous configuration is retained
	assert.Equal(t, 1, len(sched.plugins[0].Plugins))
	assert.Equal(t, "binpack", sched.plugins[0].Plugins[0].Name, "deviceshare addition must be rejected on reload")
}

// TestScheduler_Reload_RejectBackendFlagModification tests that modifying device backend
// flags on configuration reload is rejected.
func TestScheduler_Reload_RejectBackendFlagModification(t *testing.T) {
	deviceshare.ResetForTesting()
	defer deviceshare.ResetForTesting()

	initialConf := `
actions: "allocate"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
`
	confPath := createTempConfigFile(t, initialConf)
	sched := &Scheduler{
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()
	err := sched.initPlugins()
	assert.NoError(t, err)

	// Overwrite config changing AscendHAMiVNPUEnable from true to false
	newConf := `
actions: "allocate"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: false
`
	err = os.WriteFile(confPath, []byte(newConf), 0644)
	assert.NoError(t, err)

	sched.loadSchedulerConf()

	// Verify reload was rejected and previous configuration is retained
	args := framework.Arguments(sched.plugins[0].Plugins[0].Arguments)
	var hamiVal bool
	args.GetBool(&hamiVal, deviceshare.AscendHAMiVNPUEnable)
	assert.True(t, hamiVal, "AscendHAMiVNPUEnable: true must be retained")
}

// TestScheduler_Reload_AcceptSessionSettingsModification tests that modifying session-only
// settings (e.g. SchedulePolicy, ScheduleWeight) on reload is accepted and applied.
func TestScheduler_Reload_AcceptSessionSettingsModification(t *testing.T) {
	deviceshare.ResetForTesting()
	defer deviceshare.ResetForTesting()

	initialConf := `
actions: "allocate"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
      deviceshare.SchedulePolicy: "binpack"
      deviceshare.ScheduleWeight: 10
`
	confPath := createTempConfigFile(t, initialConf)
	sched := &Scheduler{
		schedulerConf:      confPath,
		disableDefaultConf: true,
	}
	sched.loadSchedulerConf()
	err := sched.initPlugins()
	assert.NoError(t, err)

	// Overwrite config changing SchedulePolicy to "spread" and weight to 20
	newConf := `
actions: "allocate"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
      deviceshare.SchedulePolicy: "spread"
      deviceshare.ScheduleWeight: 20
`
	err = os.WriteFile(confPath, []byte(newConf), 0644)
	assert.NoError(t, err)

	sched.loadSchedulerConf()

	// Verify reload was accepted and new session settings are applied
	args := framework.Arguments(sched.plugins[0].Plugins[0].Arguments)
	var policy string
	var weight int
	args.GetString(&policy, deviceshare.SchedulePolicyArgument)
	args.GetInt(&weight, deviceshare.ScheduleWeight)
	assert.Equal(t, "spread", policy, "SchedulePolicy must be updated to spread")
	assert.Equal(t, 20, weight, "ScheduleWeight must be updated to 20")
}

type initCounterPlugin struct{ calls *int }

func (p *initCounterPlugin) Name() string                      { return "test-init-counter-plugin" }
func (p *initCounterPlugin) Init() error                       { *p.calls++; return nil }
func (p *initCounterPlugin) OnSessionOpen(*framework.Session)  {}
func (p *initCounterPlugin) OnSessionClose(*framework.Session) {}

// TestScheduler_InitPlugins_SkipsUnknownAndInitializesKnown verifies that an unknown
// plugin name is logged and skipped (as framework.OpenSession does) while later
// known plugins still get Init called exactly once.
func TestScheduler_InitPlugins_SkipsUnknownAndInitializesKnown(t *testing.T) {
	calls := 0
	framework.RegisterPluginBuilder("test-init-counter-plugin", func(framework.Arguments) framework.Plugin {
		return &initCounterPlugin{calls: &calls}
	})
	defer framework.UnregisterPluginBuilder("test-init-counter-plugin")

	sched := &Scheduler{
		plugins: []conf.Tier{{
			Plugins: []conf.PluginOption{
				{Name: "non-existent-plugin-xyz"},
				{Name: "test-init-counter-plugin"},
			},
		}},
	}

	assert.NoError(t, sched.initPlugins())
	assert.Equal(t, 1, calls, "known plugin Init must run once")
	assert.True(t, sched.pluginsInitialized)
}
