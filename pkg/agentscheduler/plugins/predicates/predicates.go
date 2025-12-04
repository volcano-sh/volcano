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

package predicates

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	vcache "volcano.sh/volcano/pkg/scheduler/cache"
	vfwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "predicates"
)

type predicatesPlugin struct {
	*predicates.PredicatesPlugin
}

func New(arguments vfwk.Arguments) framework.Plugin {
	plugin := predicates.New(arguments).(*predicates.PredicatesPlugin)

	return &predicatesPlugin{
		PredicatesPlugin: plugin,
	}
}

func (pp *predicatesPlugin) Name() string {
	return PluginName
}

func (pp *predicatesPlugin) OnPluginInit(fwk *framework.Framework) {
	// 1. Set the framework handle to the predicates plugin
	pp.Handle = fwk
	// 2. Initialize all the plugin introduced from kube-scheduler ( Notice that Handle must be set before this step)
	pp.PredicatesPlugin.InitPlugin()
	// 3. Register predicates related extension points
	fwk.AddPrePredicateFn(PluginName, func(task *api.TaskInfo) error {
		state := fwk.GetCycleState(types.UID(task.UID))
		nodeInfoList, err := fwk.SnapshotSharedLister().NodeInfos().List()
		if err != nil {
			klog.Errorf("Failed to list nodes from snapshot: %v", err)
			return err
		}
		return pp.PredicatesPlugin.PrePredicate(task, state, nodeInfoList)
	})

	fwk.AddPredicateFn(PluginName, func(task *api.TaskInfo, node *api.NodeInfo) error {
		state := fwk.GetCycleState(types.UID(task.UID))
		return pp.PredicatesPlugin.Predicate(task, node, state)
	})

	fwk.AddBatchNodeOrderFn(PluginName, func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		state := fwk.GetCycleState(types.UID(task.UID))
		nodeInfoList, err := fwk.SnapshotSharedLister().NodeInfos().List()
		if err != nil {
			klog.Errorf("Failed to list nodes from snapshot: %v", err)
			return nil, err
		}
		return pp.BatchNodeOrder(task, nodeInfoList, state)
	})

	fwk.Cache.RegisterBinder(pp.Name(), pp)
}

func (pp *predicatesPlugin) PreBind(ctx context.Context, bindCtx *agentapi.BindContext) error {
	// Adapt the agent bind context to the core bind context
	coreBindCtx := toCoreBindContext(bindCtx)
	return pp.PredicatesPlugin.PreBind(ctx, coreBindCtx)
}

func (pp *predicatesPlugin) PreBindRollBack(ctx context.Context, bindCtx *agentapi.BindContext) {
	coreBindCtx := toCoreBindContext(bindCtx)
	pp.PredicatesPlugin.PreBindRollBack(ctx, coreBindCtx)
}

func (pp *predicatesPlugin) SetupBindContextExtension(state *k8sframework.CycleState, bindCtx *agentapi.BindContext) {
	vcBindCtx := toCoreBindContext(bindCtx)
	pp.PredicatesPlugin.SetupBindContextExtension(state, vcBindCtx)
}

func toCoreBindContext(bindCtx *agentapi.BindContext) *vcache.BindContext {
	return &vcache.BindContext{
		Extensions: bindCtx.Extensions,
		TaskInfo:   bindCtx.SchedCtx.Task,
	}
}

func (pp *predicatesPlugin) OnCycleStart(fwk *framework.Framework) {
}
func (pp *predicatesPlugin) OnCycleEnd(fwk *framework.Framework) {
}
