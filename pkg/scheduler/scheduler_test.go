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
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/hami"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/mindcluster/ascend310p/vnpu"
	deviceconfig "volcano.sh/volcano/pkg/scheduler/api/devices/config"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	_ "volcano.sh/volcano/pkg/scheduler/plugins"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare"
	pkgutil "volcano.sh/volcano/pkg/util"
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	options.Default()
	// Mirror cmd/scheduler/main.go so Scheduler.Run() -> runSchedulerSocket() finds -v.
	if flag.CommandLine.Lookup("v") == nil {
		klog.InitFlags(nil)
	}
	if os.Getenv(pkgutil.SocketDirEnvName) == "" {
		if socketDir, err := os.MkdirTemp("", "volcano-scheduler-test-*"); err == nil {
			_ = os.Setenv(pkgutil.SocketDirEnvName, socketDir)
			defer func() {
				_ = os.RemoveAll(socketDir)
			}()
		}
	}
	return m.Run()
}

type rejectingReloadValidator struct {
	calls int
}

func (v *rejectingReloadValidator) ValidateReload(current, updated []framework.PluginConfiguration) error {
	v.calls++
	return errors.New("test reload rejected")
}

func TestValidatePluginReload(t *testing.T) {
	validator := &rejectingReloadValidator{}
	const pluginName = "test-reload-validator"
	framework.RegisterPluginReloadValidator(pluginName, validator)
	defer framework.UnregisterPluginReloadValidator(pluginName)

	err := validatePluginReload(
		[]conf.Tier{{Plugins: []conf.PluginOption{{Name: pluginName, Arguments: map[string]interface{}{"backend": "old"}}}}},
		[]conf.Tier{{Plugins: []conf.PluginOption{{Name: pluginName, Arguments: map[string]interface{}{"backend": "new"}}}}},
	)
	assert.ErrorContains(t, err, "test reload rejected")
	assert.Equal(t, 1, validator.calls)

	err = validatePluginReload(
		[]conf.Tier{{Plugins: []conf.PluginOption{{Name: "plugin-without-reload-validator", Arguments: map[string]interface{}{"old": true}}}}},
		[]conf.Tier{{Plugins: []conf.PluginOption{{Name: "plugin-without-reload-validator", Arguments: map[string]interface{}{"new": true}}}}},
	)
	assert.NoError(t, err, "plugins without the optional reload validator keep existing reload behavior")
}

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

func buildNvidiaVGPUTestNode(name string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				deviceconfig.VolcanoVGPURegister: "GPU-test-uuid,1,16384," + vgpu.NvidiaGPUDevice + ",true,hami-core:",
			},
		},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{
				v1.ResourceName(deviceconfig.VolcanoVGPUNumber): resource.MustParse("1"),
				v1.ResourceName(deviceconfig.VolcanoVGPUCores):  resource.MustParse("100"),
				v1.ResourceName(deviceconfig.VolcanoVGPUMemory): resource.MustParse("16384"),
			},
		},
	}
}

func resetDeviceShareTestState(t *testing.T) {
	t.Helper()

	origGPUSharing := gpushare.GpuSharingEnable
	origGPUNumber := gpushare.GpuNumberEnable
	origGPUShareNodeLock := gpushare.NodeLockEnable
	origVGPU := vgpu.VGPUEnable
	origVGPUNodeLock := vgpu.NodeLockEnable
	origVGPUPolicy := vgpu.SchedulePolicy
	origMindCluster := vnpu.AscendMindClusterVNPUEnable
	origHAMi := hami.AscendHAMiVNPUEnable
	origHAMiNodeLock := hami.NodeLockEnable
	origActionMap := conf.EnabledActionMap
	origIgnoredDevices := api.IgnoredDevicesList.List()

	deviceshare.ResetForTesting()
	api.IgnoredDevicesList.Set(nil)
	gpushare.GpuSharingEnable = false
	gpushare.GpuNumberEnable = false
	gpushare.NodeLockEnable = false
	vgpu.VGPUEnable = false
	vgpu.NodeLockEnable = false
	vgpu.SchedulePolicy = ""
	vnpu.AscendMindClusterVNPUEnable = false
	hami.AscendHAMiVNPUEnable = false
	hami.NodeLockEnable = false

	t.Cleanup(func() {
		deviceshare.ResetForTesting()
		api.IgnoredDevicesList.Set(origIgnoredDevices)
		gpushare.GpuSharingEnable = origGPUSharing
		gpushare.GpuNumberEnable = origGPUNumber
		gpushare.NodeLockEnable = origGPUShareNodeLock
		vgpu.VGPUEnable = origVGPU
		vgpu.NodeLockEnable = origVGPUNodeLock
		vgpu.SchedulePolicy = origVGPUPolicy
		vnpu.AscendMindClusterVNPUEnable = origMindCluster
		hami.AscendHAMiVNPUEnable = origHAMi
		hami.NodeLockEnable = origHAMiNodeLock
		conf.EnabledActionMap = origActionMap
	})
}

type initGatePlugin struct {
	framework.Plugin
	onInit func(realInit func() error) error
}

func (p *initGatePlugin) Init() error {
	initializer, ok := p.Plugin.(framework.PluginInitializer)
	if !ok {
		return fmt.Errorf("plugin %T does not implement framework.PluginInitializer", p.Plugin)
	}
	return p.onInit(initializer.Init)
}

type lifecycleOrderingCache struct {
	*cache.SchedulerCache
	targetNodeName  string
	onRun           func()
	onNodePopulated func(error)
	onSessionOpen   func()
	onSnapshot      func(*api.ClusterInfo)
	onSessionClose  func()
}

func (c *lifecycleOrderingCache) Run(stopCh <-chan struct{}) {
	if c.onRun != nil {
		c.onRun()
	}
	c.SchedulerCache.Run(stopCh)

	// NewDefaultMockSchedulerCache leaves resourceSyncTimeout=0 so SchedulerCache.Run
	// skips WaitForHandlerSync and returns before runNodeWorker finishes AddOrUpdateNode.
	// Wait for the node worker to populate NodeInfo so the first session sees it.
	ctx, cancel := context.WithTimeout(wait.ContextForChannel(stopCh), wait.ForeverTestTimeout)
	defer cancel()
	syncErr := wait.PollUntilContextCancel(ctx, 5*time.Millisecond, true, func(context.Context) (bool, error) {
		c.SchedulerCache.Mutex.Lock()
		_, exists := c.SchedulerCache.Nodes[c.targetNodeName]
		c.SchedulerCache.Mutex.Unlock()
		return exists, nil
	})
	if c.onNodePopulated != nil {
		c.onNodePopulated(syncErr)
	}
}

func (c *lifecycleOrderingCache) OnSessionOpen() {
	if c.onSessionOpen != nil {
		c.onSessionOpen()
	}
	c.SchedulerCache.OnSessionOpen()
}

func (c *lifecycleOrderingCache) Snapshot() *api.ClusterInfo {
	snap := c.SchedulerCache.Snapshot()
	if c.onSnapshot != nil {
		c.onSnapshot(snap)
	}
	return snap
}

func (c *lifecycleOrderingCache) OnSessionClose() {
	c.SchedulerCache.OnSessionClose()
	if c.onSessionClose != nil {
		c.onSessionClose()
	}
}

type deviceShareLifecycleSpec struct {
	nodeName       string
	node           *v1.Node
	pluginArgs     map[string]interface{}
	registeredName string
	isEnabled      func() bool
	assertCached   func(t *testing.T, cached *api.NodeInfo)
	assertCloned   func(t *testing.T, cached, cloned *api.NodeInfo)
}

func verifyDeviceShareRunOrdering(t *testing.T, spec deviceShareLifecycleSpec) {
	t.Helper()
	resetDeviceShareTestState(t)

	origBuilder, found := framework.GetPluginBuilder(deviceshare.PluginName)
	require.True(t, found, "deviceshare plugin builder must be registered")
	t.Cleanup(func() {
		framework.RegisterPluginBuilder(deviceshare.PluginName, origBuilder)
	})

	realCache := cache.NewDefaultMockSchedulerCache("volcano")
	origEnableCacheDumper := options.ServerOpts.EnableCacheDumper
	options.ServerOpts.EnableCacheDumper = false
	t.Cleanup(func() {
		options.ServerOpts.EnableCacheDumper = origEnableCacheDumper
	})

	_, err := realCache.Client().CoreV1().Nodes().Create(context.Background(), spec.node, metav1.CreateOptions{})
	require.NoError(t, err)
	require.False(t, spec.isEnabled(), "device flag must start false before Scheduler.Run()")

	var (
		stateMu           sync.Mutex
		events            []string
		initCalls         atomic.Int32
		initCompleted     atomic.Bool
		cacheRunCalled    atomic.Bool
		sessionOpened     atomic.Bool
		enabledAtCacheRun bool
		regAtCacheRun     bool
		nodesAtCacheRun   int
		nodeSyncErr       error
		clonedNode        *api.NodeInfo
	)
	recordEvent := func(event string) {
		stateMu.Lock()
		events = append(events, event)
		stateMu.Unlock()
	}

	initStarted := make(chan struct{})
	releaseInit := make(chan struct{})
	cacheRunStarted := make(chan struct{})
	sessionCompleted := make(chan struct{})
	runReturned := make(chan struct{})
	stopCh := make(chan struct{})

	var (
		initStartOnce    sync.Once
		releaseInitOnce  sync.Once
		cacheRunOnce     sync.Once
		sessionCloseOnce sync.Once
		stopOnce         sync.Once
		shutdownOnce     sync.Once
	)

	stopScheduler := func(waitForRun bool) {
		releaseInitOnce.Do(func() { close(releaseInit) })
		stopOnce.Do(func() { close(stopCh) })
		if waitForRun {
			select {
			case <-runReturned:
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("timed out waiting for Scheduler.Run() to exit")
			}
			if sessionOpened.Load() {
				select {
				case <-sessionCompleted:
				case <-time.After(wait.ForeverTestTimeout):
					t.Errorf("timed out waiting for active scheduling session to finish")
				}
			}
		}
		shutdownOnce.Do(func() { cache.ShutdownMockSchedulerCache(realCache) })
	}

	framework.RegisterPluginBuilder(deviceshare.PluginName, func(args framework.Arguments) framework.Plugin {
		realPlugin := origBuilder(args)
		return &initGatePlugin{
			Plugin: realPlugin,
			onInit: func(realInit func() error) error {
				initCalls.Add(1)
				if cacheRunCalled.Load() {
					t.Errorf("cache.Run() started before DeviceShare.Init() began")
				}
				recordEvent("init-started")
				initStartOnce.Do(func() { close(initStarted) })
				<-releaseInit

				if err := realInit(); err != nil {
					return err
				}
				initCompleted.Store(true)
				recordEvent("init-completed")
				return nil
			},
		}
	})

	wrappedCache := &lifecycleOrderingCache{
		SchedulerCache: realCache,
		targetNodeName: spec.nodeName,
		onRun: func() {
			realCache.Mutex.Lock()
			nodesCount := len(realCache.Nodes)
			realCache.Mutex.Unlock()

			stateMu.Lock()
			enabledAtCacheRun = spec.isEnabled()
			regAtCacheRun = slices.Contains(api.RegisteredDevices, spec.registeredName)
			nodesAtCacheRun = nodesCount
			stateMu.Unlock()

			if !initCompleted.Load() {
				t.Errorf("cache.Run() started before DeviceShare.Init() completed")
			}
			cacheRunCalled.Store(true)
			recordEvent("cache-run-started")
			cacheRunOnce.Do(func() { close(cacheRunStarted) })
		},
		onNodePopulated: func(syncErr error) {
			stateMu.Lock()
			nodeSyncErr = syncErr
			stateMu.Unlock()
			if syncErr == nil {
				recordEvent("cache-node-populated")
			}
		},
		onSessionOpen: func() {
			sessionOpened.Store(true)
		},
		onSnapshot: func(snap *api.ClusterInfo) {
			if ni, ok := snap.Nodes[spec.nodeName]; ok {
				stateMu.Lock()
				if clonedNode == nil {
					clonedNode = ni
				}
				stateMu.Unlock()
			}
		},
		onSessionClose: func() {
			sessionCloseOnce.Do(func() { close(sessionCompleted) })
		},
	}

	enablePredicate := true
	rawConf, err := yaml.Marshal(&conf.SchedulerConfiguration{
		Actions: "allocate",
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             deviceshare.PluginName,
						EnabledPredicate: &enablePredicate,
						Arguments:        spec.pluginArgs,
					},
				},
			},
		},
	})
	require.NoError(t, err)
	confPath := createTempConfigFile(t, string(rawConf))

	sched := &Scheduler{
		cache:              wrappedCache,
		schedulerConf:      confPath,
		disableDefaultConf: true,
		schedulePeriod:     time.Hour,
	}

	runStarted := false
	t.Cleanup(func() {
		stopScheduler(runStarted)
	})

	runStarted = true
	go func() {
		sched.Run(stopCh)
		close(runReturned)
	}()

	select {
	case <-initStarted:
	case <-cacheRunStarted:
		require.FailNow(t, "cache.Run() started before DeviceShare.Init() began")
	case <-time.After(wait.ForeverTestTimeout):
		require.FailNow(t, "timed out waiting for DeviceShare.Init() to start")
	}

	// While DeviceShare.Init() is still paused before completion, cache.Run()
	// must not start and device enablement must not be applied yet.
	select {
	case <-cacheRunStarted:
		require.FailNow(t, "cache.Run() started before DeviceShare.Init() completed")
	case <-time.After(30 * time.Millisecond):
	}
	assert.False(t, spec.isEnabled(), "device flag must remain false before DeviceShare.Init() completes")
	assert.Empty(t, api.RegisteredDevices, "devices must not be registered before DeviceShare.Init() completes")

	releaseInitOnce.Do(func() { close(releaseInit) })

	select {
	case <-runReturned:
	case <-time.After(wait.ForeverTestTimeout):
		require.FailNow(t, "timed out waiting for Scheduler.Run() startup to complete")
	}

	select {
	case <-sessionCompleted:
	case <-time.After(wait.ForeverTestTimeout):
		require.FailNow(t, "timed out waiting for scheduling session snapshot")
	}

	stopScheduler(true)

	stateMu.Lock()
	gotEvents := slices.Clone(events)
	gotEnabledAtCacheRun := enabledAtCacheRun
	gotRegAtCacheRun := regAtCacheRun
	gotNodesAtCacheRun := nodesAtCacheRun
	gotNodeSyncErr := nodeSyncErr
	gotClonedNode := clonedNode
	stateMu.Unlock()

	assert.Equal(t, int32(1), initCalls.Load(), "DeviceShare.Init() must run once at startup, not per session")
	assert.Equal(t, []string{"init-started", "init-completed", "cache-run-started", "cache-node-populated"}, gotEvents)
	assert.True(t, gotEnabledAtCacheRun, "device flag must be enabled before cache.Run()")
	assert.True(t, gotRegAtCacheRun, "device %q must be registered before cache.Run()", spec.registeredName)
	assert.Zero(t, gotNodesAtCacheRun, "cache must not hold nodes before cache.Run() starts")
	require.NoError(t, gotNodeSyncErr, "cache worker must populate NodeInfo after cache.Run()")

	realCache.Mutex.Lock()
	cachedNode := realCache.Nodes[spec.nodeName]
	realCache.Mutex.Unlock()

	require.NotNil(t, cachedNode, "cache must populate NodeInfo via AddOrUpdateNode")
	spec.assertCached(t, cachedNode)

	require.NotNil(t, gotClonedNode, "scheduling session snapshot must clone cached NodeInfo")
	assert.NotSame(t, cachedNode, gotClonedNode)
	spec.assertCloned(t, cachedNode, gotClonedNode)
}

// TestIssue5846_EarlyPluginInitFix verifies through Scheduler.Run() that
// DeviceShare initializes Ascend HAMi settings before cache.Run() starts,
// cache workers populate AscendDevices in NodeInfo, and session snapshots retain them.
func TestIssue5846_EarlyPluginInitFix(t *testing.T) {
	const nodeName = "ascend-node-fixed"
	verifyDeviceShareRunOrdering(t, deviceShareLifecycleSpec{
		nodeName:       nodeName,
		node:           buildAscendTestNode(nodeName),
		pluginArgs:     map[string]interface{}{deviceshare.AscendHAMiVNPUEnable: true},
		registeredName: "Ascend310P",
		isEnabled:      func() bool { return hami.AscendHAMiVNPUEnable },
		assertCached: func(t *testing.T, cached *api.NodeInfo) {
			cachedDevices, ok := cached.Others["Ascend310P"].(*hami.AscendDevices)
			require.True(t, ok, "cached node must contain Ascend310P devices")
			require.NotNil(t, cachedDevices)
		},
		assertCloned: func(t *testing.T, cached, cloned *api.NodeInfo) {
			cachedDevices, ok := cached.Others["Ascend310P"].(*hami.AscendDevices)
			require.True(t, ok)
			clonedDevices, ok := cloned.Others["Ascend310P"].(*hami.AscendDevices)
			require.True(t, ok, "cloned session node must retain Ascend310P devices")
			require.NotNil(t, clonedDevices)
			assert.NotSame(t, cachedDevices, clonedDevices)

			dev, ok := cloned.Others["Ascend310P"].(api.Devices)
			assert.True(t, ok, "device in cloned node must implement api.Devices")
			assert.NotNil(t, dev)
		},
	})
}

func requireNvidiaVGPUDevice(t *testing.T, node *api.NodeInfo, role string) (*vgpu.GPUDevices, *vgpu.GPUDevice) {
	t.Helper()

	devices, ok := node.Others[vgpu.DeviceName].(*vgpu.GPUDevices)
	require.True(t, ok, "%s node must contain NVIDIA vGPU devices", role)
	require.NotNil(t, devices)

	device, exists := devices.Device[0]
	require.True(t, exists, "%s NVIDIA vGPU devices must contain the registered GPU", role)
	require.NotNil(t, device)
	assert.Equal(t, "GPU-test-uuid", device.UUID)
	assert.Equal(t, uint(1), device.Number)
	assert.Equal(t, uint(16384), device.Memory)
	assert.Equal(t, vgpu.NvidiaGPUDevice, device.Type)
	assert.True(t, device.Health)
	return devices, device
}

// TestIssue5846_EarlyPluginInitFixNvidiaVGPU verifies through Scheduler.Run() that
// DeviceShare.Init() runs before cache.Run(), cannot be bypassed by starting the cache
// before initialization completes, populates NVIDIA vGPU NodeInfo when the cache syncs
// nodes, and keeps the deep-copied GPU info intact after Snapshot() cloning.
func TestIssue5846_EarlyPluginInitFixNvidiaVGPU(t *testing.T) {
	const nodeName = "nvidia-vgpu-node"
	verifyDeviceShareRunOrdering(t, deviceShareLifecycleSpec{
		nodeName:       nodeName,
		node:           buildNvidiaVGPUTestNode(nodeName),
		pluginArgs:     map[string]interface{}{deviceshare.VGPUEnable: true},
		registeredName: vgpu.DeviceName,
		isEnabled:      func() bool { return vgpu.VGPUEnable },
		assertCached: func(t *testing.T, cached *api.NodeInfo) {
			requireNvidiaVGPUDevice(t, cached, "cached")
		},
		assertCloned: func(t *testing.T, cached, cloned *api.NodeInfo) {
			cachedDevices, cachedDevice := requireNvidiaVGPUDevice(t, cached, "cached")
			clonedDevices, clonedDevice := requireNvidiaVGPUDevice(t, cloned, "cloned")
			assert.NotSame(t, cachedDevices, clonedDevices)
			assert.NotSame(t, cachedDevice, clonedDevice)
		},
	})
}

// TestIssue5846_DisabledConfiguration verifies that when AscendHAMiVNPUEnable is disabled,
// initPlugins() does not enable it and cached nodes do not receive Ascend devices.
func TestIssue5846_DisabledConfiguration(t *testing.T) {
	resetDeviceShareTestState(t)

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
	resetDeviceShareTestState(t)

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

func TestScheduler_InitPlugins_RejectsDuplicateDeviceShare(t *testing.T) {
	deviceshare.ResetForTesting()
	t.Cleanup(deviceshare.ResetForTesting)

	originalHAMiEnabled := hami.AscendHAMiVNPUEnable
	t.Cleanup(func() { hami.AscendHAMiVNPUEnable = originalHAMiEnabled })
	hami.AscendHAMiVNPUEnable = false

	sched := &Scheduler{
		plugins: []conf.Tier{{Plugins: []conf.PluginOption{
			{Name: deviceshare.PluginName},
			{Name: deviceshare.PluginName, Arguments: map[string]interface{}{deviceshare.AscendHAMiVNPUEnable: true}},
		}}},
	}

	err := sched.initPlugins()
	assert.ErrorContains(t, err, "multiple DeviceShare declarations")
	assert.False(t, sched.pluginsInitialized)
	assert.False(t, hami.AscendHAMiVNPUEnable, "startup validation must run before any DeviceShare global mutation")
	assert.Empty(t, api.RegisteredDevices, "startup validation must run before device registration")
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

// TestScheduler_Reload_RejectsMixedChangesAtomically verifies that an
// incompatible DeviceShare change rejects the complete reload, including an
// otherwise valid action change.
func TestScheduler_Reload_RejectsMixedChangesAtomically(t *testing.T) {
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
	sched := &Scheduler{schedulerConf: confPath, disableDefaultConf: true}
	sched.loadSchedulerConf()
	assert.NoError(t, sched.initPlugins())

	mixedConf := `
actions: "enqueue"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: false
`
	assert.NoError(t, os.WriteFile(confPath, []byte(mixedConf), 0644))
	sched.loadSchedulerConf()

	assert.Len(t, sched.actions, 1)
	assert.Equal(t, "allocate", sched.actions[0].Name(), "action change must not be partially applied")
	args := framework.Arguments(sched.plugins[0].Plugins[0].Arguments)
	var hamiEnabled bool
	args.GetBool(&hamiEnabled, deviceshare.AscendHAMiVNPUEnable)
	assert.True(t, hamiEnabled, "backend change must not be partially applied")
}

func TestScheduler_Reload_AcceptsActionChangeWithoutBackendChange(t *testing.T) {
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
	sched := &Scheduler{schedulerConf: confPath, disableDefaultConf: true}
	sched.loadSchedulerConf()
	assert.NoError(t, sched.initPlugins())

	updatedConf := `
actions: "enqueue"
tiers:
- plugins:
  - name: deviceshare
    enablePredicate: true
    arguments:
      deviceshare.AscendHAMiVNPUEnable: true
`
	assert.NoError(t, os.WriteFile(confPath, []byte(updatedConf), 0644))
	sched.loadSchedulerConf()

	assert.Len(t, sched.actions, 1)
	assert.Equal(t, "enqueue", sched.actions[0].Name())
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
	assert.NoError(t, sched.initPlugins(), "repeated scheduler initialization must be a no-op")
	assert.Equal(t, 1, calls, "a plugin initializer must run once per scheduler startup")
}
