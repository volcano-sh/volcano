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

package kubescheduler

import (
	"context"
	"maps"
	"slices"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/names"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	vbcap "volcano.sh/volcano/pkg/scheduler/capabilities/volumebinding"
	"volcano.sh/volcano/pkg/scheduler/framework"
	volcanometrics "volcano.sh/volcano/pkg/scheduler/metrics"
)

const (
	PluginName           = "kubescheduler"
	defaultSchedulerName = "volcano"
)

var (
	kubeFwk     k8sframework.Framework
	kubeProfile *kubeschedulerconfig.KubeSchedulerProfile
)

type kubeSchedulerPlugin struct {
	args          *PluginArgs
	schedulingCtx context.Context
	cancelFn      context.CancelFunc
}

// bind context extension information of kubeSchedulerPlugin
type bindContextExtension struct {
	State *k8sframework.CycleState
}

func New(arguments framework.Arguments) framework.Plugin {
	plugin := &kubeSchedulerPlugin{
		args: &PluginArgs{
			VolumeBinding: NewDefaultVolumeBindingArgs(),
		},
	}

	// set up args for each plugin
	SetUpVolumeBindingArgs(plugin.args, arguments)

	return plugin
}

func (k *kubeSchedulerPlugin) Name() string {
	return PluginName
}

func (k *kubeSchedulerPlugin) OnSessionOpen(ssn *framework.Session) {
	logger := klog.Background()
	logger.V(5).Info("Enter kube scheduler plugin ...")
	defer func() {
		logger.V(5).Info("Leaving kube scheduler plugin ...")
	}()
	ctx := klog.NewContext(context.Background(), logger)
	k.schedulingCtx, k.cancelFn = context.WithCancel(ctx)
	// register necessary metrics
	volcanometrics.RegisterMetrics()
	// kube scheduler framework initialization
	k.setUpOrResetFramework(ssn)
	// init cycleStatesMap for each pending pod
	k.initCycleState(ssn)

	// register extension points
	ssn.AddPrePredicateFn(k.Name(), func(task *api.TaskInfo) error {
		// It is safe here to directly use the state to run plugins because we have already initialized the cycle state
		// for each pending pod and will not meet nil state
		state := ssn.GetCycleState(task.UID)

		_, status, _ := kubeFwk.RunPreFilterPlugins(k.schedulingCtx, state, task.Pod)
		if !status.IsSuccess() {
			return status.AsError()
		}

		return nil
	})

	ssn.AddPredicateFn(k.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		state := ssn.GetCycleState(task.UID)

		status := kubeFwk.RunFilterPlugins(k.schedulingCtx, state, task.Pod, ssn.NodeMap[node.Name])
		if !status.IsSuccess() {
			return status.AsError()
		}

		return nil
	})

	ssn.AddBatchNodeOrderFn(k.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		state := ssn.GetCycleState(task.UID)
		nodeList := slices.Collect(maps.Values(ssn.NodeMap))

		// PreScore
		preScoreStatus := kubeFwk.RunPreScorePlugins(k.schedulingCtx, state, task.Pod, nodeList)
		if !preScoreStatus.IsSuccess() {
			return nil, preScoreStatus.AsError()
		}

		// Score
		nodesScores, scoreStatus := kubeFwk.RunScorePlugins(k.schedulingCtx, state, task.Pod, nodeList)
		if !scoreStatus.IsSuccess() {
			return nil, scoreStatus.AsError()
		}

		result := make(map[string]float64, len(nodesScores))
		for _, nodeScore := range nodesScores {
			for _, pluginScore := range nodeScore.Scores {
				logger.V(10).Info("Plugin scored node for pod", "pod", klog.KObj(task.Pod), "plugin", pluginScore.Name, "node", nodeScore.Name, "score", pluginScore.Score)
			}
			result[nodeScore.Name] += float64(nodeScore.TotalScore)
		}

		return result, nil
	})

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			state := ssn.GetCycleState(event.Task.UID)

			status := kubeFwk.RunReservePluginsReserve(k.schedulingCtx, state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
			if !status.IsSuccess() {
				event.Err = status.AsError()
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			state := ssn.GetCycleState(event.Task.UID)

			kubeFwk.RunReservePluginsUnreserve(k.schedulingCtx, state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		},
	})

	// Register bind handler
	ssn.RegisterBindHandler(k.Name(), k)
}

func (k *kubeSchedulerPlugin) OnSessionClose(ssn *framework.Session) {
	k.cancelFn()
}

func (k *kubeSchedulerPlugin) PreBind(ctx context.Context, bindCtx *cache.BindContext) error {
	if !k.needsPreBind(bindCtx.TaskInfo) {
		return nil
	}

	state := bindCtx.Extensions[k.Name()].(*bindContextExtension).State
	logger := klog.FromContext(ctx)

	status := kubeFwk.RunPreBindPlugins(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
	if !status.IsSuccess() {
		return status.AsError()
	}

	logger.V(5).Info("Successfully executed PreBind plugins", "plugin", k.Name(), "pod", klog.KObj(bindCtx.TaskInfo.Pod), "node", bindCtx.TaskInfo.Pod.Spec.NodeName)
	return nil
}

func (k *kubeSchedulerPlugin) RollBack(ctx context.Context, bindCtx *cache.BindContext) {
	if !k.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	state := bindCtx.Extensions[k.Name()].(*bindContextExtension).State

	kubeFwk.RunReservePluginsUnreserve(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
}

func (k *kubeSchedulerPlugin) SetupBindContextExtension(ssn *framework.Session, bindCtx *cache.BindContext) {
	if !k.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	bindCtx.Extensions[k.Name()] = &bindContextExtension{State: ssn.GetCycleState(bindCtx.TaskInfo.UID)}
}

// needsPreBind judges whether the pod needs set up extension information in bind context
// to execute extra extension points like PreBind.
// Currently, if a pod has any of the following resources, we assume that the pod needs to pass extension information:
// 1. With volumes
// 2. With resourceClaims
// ...
// If a pod doesn't contain any of the above resources, the pod doesn't need to set up extension information in bind context.
// If necessary, more refined filtering can be added in this func.
func (k *kubeSchedulerPlugin) needsPreBind(task *api.TaskInfo) bool {
	// 1. With volumes
	if len(task.Pod.Spec.Volumes) > 0 && k.args.VolumeBinding.Enabled {
		return true
	}

	// 2. With resourceClaims

	return false
}

func (k *kubeSchedulerPlugin) initCycleState(ssn *framework.Session) {
	// init cycleStatesMap for each pending pod
	for _, job := range ssn.Jobs {
		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> is not valid, reason: %s, message %s, skip initing cycle state for tasks in it",
				job.Namespace, job.Name, vr.Reason, vr.Message)
			continue
		}

		for _, task := range job.TaskStatusIndex[api.Pending] {
			// Skip tasks whose pod are scheduling gated
			if task.SchGated {
				continue
			}

			// Init cycle state for the pod to share it with other extension points
			state := k8sframework.NewCycleState()
			ssn.CycleStatesMap.Store(task.UID, state)
		}
	}
}

func (k *kubeSchedulerPlugin) generateKubeSchedulerProfile() *kubeschedulerconfig.KubeSchedulerProfile {
	enabledPlugins := &kubeschedulerconfig.Plugins{
		MultiPoint: kubeschedulerconfig.PluginSet{
			Enabled: []kubeschedulerconfig.Plugin{
				{Name: names.PrioritySort},
				{Name: names.DefaultBinder},
			},
		},
	}
	pluginConfigs := make([]kubeschedulerconfig.PluginConfig, 0)

	// init args
	// volume binding args
	if k.args.VolumeBinding.Enabled {
		enabledPlugins.MultiPoint.Enabled = append(enabledPlugins.MultiPoint.Enabled,
			kubeschedulerconfig.Plugin{
				Name: names.VolumeBinding,
			},
		)

		args := &kubeschedulerconfig.VolumeBindingArgs{
			BindTimeoutSeconds: k.args.VolumeBinding.BindTimeoutSeconds,
			Shape:              k.args.VolumeBinding.Shape,
		}
		pluginConfig := kubeschedulerconfig.PluginConfig{
			Name: names.VolumeBinding,
			Args: args,
		}
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	profile := &kubeschedulerconfig.KubeSchedulerProfile{
		SchedulerName: defaultSchedulerName,
		Plugins:       enabledPlugins,
		PluginConfig:  pluginConfigs,
	}

	return profile
}

// pluginConfigUpdated checks if the plugin configuration has been updated.
func (k *kubeSchedulerPlugin) pluginConfigUpdated(newProfile *kubeschedulerconfig.KubeSchedulerProfile) bool {
	if kubeProfile == nil {
		return true
	}

	if len(newProfile.PluginConfig) != len(kubeProfile.PluginConfig) {
		return true
	}

	// convert plugin config slice to map
	oldPluginConfigsMap := make(map[string]runtime.Object)
	newPluginConfigsMap := make(map[string]runtime.Object)
	for _, pluginConfig := range kubeProfile.PluginConfig {
		oldPluginConfigsMap[pluginConfig.Name] = pluginConfig.Args
	}
	for _, pluginConfig := range newProfile.PluginConfig {
		newPluginConfigsMap[pluginConfig.Name] = pluginConfig.Args
	}

	// compare plugin config map
	for name, newArgs := range newPluginConfigsMap {
		if oldArgs, ok := oldPluginConfigsMap[name]; !ok || !cmp.Equal(newArgs, oldArgs) {
			return true
		}
	}

	return false
}

func (k *kubeSchedulerPlugin) setUpOrResetFramework(ssn *framework.Session) {
	r := NewRegistry()
	newProfile := k.generateKubeSchedulerProfile()
	if kubeFwk == nil || k.pluginConfigUpdated(newProfile) {
		var err error
		kubeFwk, err = frameworkruntime.NewFramework(k.schedulingCtx, r, newProfile,
			frameworkruntime.WithClientSet(ssn.KubeClient()),
			frameworkruntime.WithInformerFactory(ssn.InformerFactory()),
		)
		if err != nil {
			klog.Fatalf("failed to init kube scheduler plugin: %v", err)
		}
		kubeProfile = newProfile
	}
}

func NewRegistry() frameworkruntime.Registry {
	fts := feature.Features{
		EnableVolumeCapacityPriority: utilfeature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority),
	}

	registry := frameworkruntime.Registry{
		names.VolumeBinding: frameworkruntime.FactoryAdapter(fts, vbcap.New),
		// PrioritySort has no actual effect here, volcano has its own implementation of pods priority.
		// The reason why adding PrioritySort here is kube scheduler framework initialization requires it.
		names.PrioritySort: queuesort.New,
		// Just like QueueSort, DefaultBinder here is just a placeholder and does not actually perform the binding.
		names.DefaultBinder: defaultbinder.New,
	}

	return registry
}
