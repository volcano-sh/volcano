package predicates

import (
	"maps"
	"slices"

	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/cache"
	vfwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "predicates"
)

type predicatesPlugin struct {
	plugin *predicates.PredicatesPlugin
}

func New(arguments vfwk.Arguments) framework.Plugin {
	return &predicatesPlugin{plugin: predicates.New(arguments).(*predicates.PredicatesPlugin)}
}

func (pp *predicatesPlugin) Name() string {
	return PluginName
}

// Register plugin to framework
func (pp *predicatesPlugin) Register() {
	// s := framework.SchedulePluginRegistry{}
	// s.AddPrePredicateFn(ppa.plugin.GetPredicateFn(nodeMap map[string]fwk.NodeInfo, plugins map[string]k8sframework.FilterPlugin, predicate predicateEnable, pCache *predicateCache)))
}

func (pp *predicatesPlugin) OnSchedulingStart(sc *framework.ScheduleCycle) {
	pluginState := &predicates.PredicatesPluginState{}
	pluginState.NodeMap = sc.NodeMap
	pluginState.NodeInfoList = slices.Collect(maps.Values(sc.NodeMap))
	pluginState.KubeClient = sc.KubeClient()
	pluginState.InformerFactory = sc.InformerFactory()
	pluginState.SharedDRAManager = sc.SharedDRAManager()
	pluginState.GetCycleState = sc.GetCycleState
	pp.plugin.InitPluginState(pluginState)

	sc.AddPrePredicateFn(PluginName, pp.plugin.GetPrePredicateFn(pluginState))
	sc.AddPredicateFn(PluginName, pp.plugin.GetPredicateFn(pluginState))
}

func (pp *predicatesPlugin) OnSchedulingEnd(sc *framework.ScheduleCycle) {}

func (pp *predicatesPlugin) SetupBindContextExtension(state *k8sframework.CycleState, bindCtx *cache.BindContext) {
	pp.plugin.SetupBindContextExtension(state, bindCtx)
}
