package removefailedpods

import (
	"context"

	"k8s.io/api/core/v1"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removefailedpods"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const PluginName = "RemoveFailedPods"

type removefailedpodsPlugin struct {
	pluginArguments  framework.Arguments
	RemoveFailedPods frameworktypes.DeschedulePlugin
}

func New(args framework.Arguments) framework.Plugin {
	removeFailedPods, _ := removefailedpods.New(nil, nil)
	return &removefailedpodsPlugin{
		pluginArguments:  args,
		RemoveFailedPods: removeFailedPods.(frameworktypes.DeschedulePlugin),
	}
}
func (re *removefailedpodsPlugin) Name() string {
	return PluginName
}

func (re *removefailedpodsPlugin) OnSessionOpen(ssn *framework.Session) {
	nodes := make([]*v1.Node, 0)
	for _, value := range ssn.Nodes {
		nodes = append(nodes, value.Node)
	}
	re.RemoveFailedPods.Deschedule(context.TODO(), nodes)
}

func (re *removefailedpodsPlugin) OnSessionClose(ssn *framework.Session) {

}
