package plugins

import (
	"testing"

	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/predicates"
)

func TestDefaultPluginBuildersAreRegistered(t *testing.T) {
	tests := []struct {
		name       string
		pluginName string
	}{
		{name: "predicates builder", pluginName: predicates.PluginName},
		{name: "nodeorder builder", pluginName: nodeorder.PluginName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, found := framework.GetPluginBuilder(tt.pluginName)
			if !found {
				t.Fatalf("expected plugin builder for %q to be registered", tt.pluginName)
			}
			plugin := builder(nil)
			if plugin.Name() != tt.pluginName {
				t.Fatalf("expected plugin name %q, got %q", tt.pluginName, plugin.Name())
			}
		})
	}
}
