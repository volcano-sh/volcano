package predicates

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
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

func (pp *predicatesPlugin) OnSchedulingStart(fwk *framework.Framework) {
	pp.PredicatesPlugin.InitPlugin()

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
}

func (pp *predicatesPlugin) OnSchedulingEnd(fwk *framework.Framework) {
}
