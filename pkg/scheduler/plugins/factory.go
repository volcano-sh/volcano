/*
Copyright 2019 The Kubernetes Authors.

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

package plugins

import (
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/binpack"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/cdp"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/deviceshare"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/extender"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	networktopologyaware "volcano.sh/volcano/pkg/scheduler/plugins/network-topology-aware"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodegroup"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware"
	"volcano.sh/volcano/pkg/scheduler/plugins/overcommit"
	"volcano.sh/volcano/pkg/scheduler/plugins/pdb"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/plugins/rescheduling"
	resourcestrategyfit "volcano.sh/volcano/pkg/scheduler/plugins/resource-strategy-fit"
	"volcano.sh/volcano/pkg/scheduler/plugins/resourcequota"
	"volcano.sh/volcano/pkg/scheduler/plugins/sla"
	tasktopology "volcano.sh/volcano/pkg/scheduler/plugins/task-topology"
	"volcano.sh/volcano/pkg/scheduler/plugins/tdm"
	"volcano.sh/volcano/pkg/scheduler/plugins/usage"
)

func init() {
	// Plugins for Jobs
	framework.RegisterPluginBuilder(drf.PluginName, drf.New)
	framework.RegisterPluginBuilder(gang.PluginName, gang.New)
	framework.RegisterPluginBuilder(deviceshare.PluginName, deviceshare.New)
	framework.RegisterPluginBuilder(predicates.PluginName, predicates.New)
	framework.RegisterPluginBuilder(priority.PluginName, priority.New)
	framework.RegisterPluginBuilder(nodeorder.PluginName, nodeorder.New)
	framework.RegisterPluginBuilder(conformance.PluginName, conformance.New)
	framework.RegisterPluginBuilder(binpack.PluginName, binpack.New)
	framework.RegisterPluginBuilder(resourcestrategyfit.PluginName, resourcestrategyfit.New)
	framework.RegisterPluginBuilder(tdm.PluginName, tdm.New)
	framework.RegisterPluginBuilder(overcommit.PluginName, overcommit.New)
	framework.RegisterPluginBuilder(sla.PluginName, sla.New)
	framework.RegisterPluginBuilder(tasktopology.PluginName, tasktopology.New)
	framework.RegisterPluginBuilder(numaaware.PluginName, numaaware.New)
	framework.RegisterPluginBuilder(cdp.PluginName, cdp.New)
	framework.RegisterPluginBuilder(rescheduling.PluginName, rescheduling.New)
	framework.RegisterPluginBuilder(usage.PluginName, usage.New)
	framework.RegisterPluginBuilder(pdb.PluginName, pdb.New)
	framework.RegisterPluginBuilder(nodegroup.PluginName, nodegroup.New)
	framework.RegisterPluginBuilder(networktopologyaware.PluginName, networktopologyaware.New)

	// Plugins for Queues
	framework.RegisterPluginBuilder(proportion.PluginName, proportion.New)
	framework.RegisterPluginBuilder(capacity.PluginName, capacity.New)

	// Plugins for Extender
	framework.RegisterPluginBuilder(extender.PluginName, extender.New)

	// Plugins for ResourceQuota
	framework.RegisterPluginBuilder(resourcequota.PluginName, resourcequota.New)
}
