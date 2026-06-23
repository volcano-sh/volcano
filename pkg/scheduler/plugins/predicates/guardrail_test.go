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
	"testing"

	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"

	vbcap "volcano.sh/volcano/pkg/scheduler/capabilities/volumebinding"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

func TestGuardrailExtensionPoints(t *testing.T) {
	// A list of known kube-scheduler framework extension point interfaces
	checkInterfaces := func(plugin fwk.Plugin) map[string]bool {
		interfaces := make(map[string]bool)

		if _, ok := plugin.(fwk.PreEnqueuePlugin); ok {
			interfaces["PreEnqueue"] = true
		}
		if _, ok := plugin.(fwk.EnqueueExtensions); ok {
			interfaces["EnqueueExtensions"] = true
		}
		if _, ok := plugin.(fwk.PreFilterPlugin); ok {
			interfaces["PreFilter"] = true
		}
		if _, ok := plugin.(fwk.FilterPlugin); ok {
			interfaces["Filter"] = true
		}
		if _, ok := plugin.(fwk.PostFilterPlugin); ok {
			interfaces["PostFilter"] = true
		}
		if _, ok := plugin.(fwk.PreScorePlugin); ok {
			interfaces["PreScore"] = true
		}
		if scorePlugin, ok := plugin.(fwk.ScorePlugin); ok {
			interfaces["Score"] = true
			if scorePlugin.ScoreExtensions() != nil {
				interfaces["NormalizeScore"] = true
			}
		}
		if _, ok := plugin.(fwk.ReservePlugin); ok {
			interfaces["Reserve"] = true
		}
		if _, ok := plugin.(fwk.PreBindPlugin); ok {
			interfaces["PreBind"] = true
		}
		if _, ok := plugin.(fwk.BindPlugin); ok {
			interfaces["Bind"] = true
		}
		if _, ok := plugin.(fwk.PostBindPlugin); ok {
			interfaces["PostBind"] = true
		}
		if _, ok := plugin.(fwk.PermitPlugin); ok {
			interfaces["Permit"] = true
		}
		return interfaces
	}

	client := k8sfake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	handle := k8s.NewFramework(nil, k8s.WithClientSet(client), k8s.WithInformerFactory(informerFactory))

	testCases := []struct {
		name      string
		getPlugin func() fwk.Plugin
		adapted   []string
		ignored   []string
	}{
		{
			name: "DynamicResources",
			getPlugin: func() fwk.Plugin {
				args := &config.DynamicResourcesArgs{}
				p, err := dynamicresources.New(context.TODO(), args, handle, feature.Features{})
				if err != nil {
					t.Fatalf("Failed to create DynamicResources plugin: %v", err)
				}
				return p
			},
			adapted: []string{
				"PreFilter",
				"Filter",
				"Score",
				"NormalizeScore",
				"Reserve",
				"PreBind",
			},
			ignored: []string{
				"PreEnqueue",
				"EnqueueExtensions",
				"PostFilter",
				"PostBind",
			},
		},
		{
			name: "VolumeBinding",
			getPlugin: func() fwk.Plugin {
				args := &config.VolumeBindingArgs{
					BindTimeoutSeconds: 600,
				}
				p, err := vbcap.New(context.TODO(), args, handle, feature.Features{})
				if err != nil {
					t.Fatalf("Failed to create VolumeBinding plugin: %v", err)
				}
				return p
			},
			adapted: []string{
				"PreFilter",
				"Filter",
				"PreScore",
				"Score",
				"Reserve",
				"PreBind",
			},
			ignored: []string{
				"EnqueueExtensions",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := tc.getPlugin()
			implemented := checkInterfaces(plugin)

			// Combine adapted and ignored to form the allowed list
			allowed := make(map[string]bool)
			for _, ext := range tc.adapted {
				allowed[ext] = true
			}
			for _, ext := range tc.ignored {
				allowed[ext] = true
			}

			// Check for missing explicitly defined interfaces
			for ext := range implemented {
				if !allowed[ext] {
					t.Errorf("%s implements %s, but Volcano has not marked it as adapted or intentionally ignored.\nPlease decide whether this extension point should be supported in Volcano.", tc.name, ext)
				}
			}

			// Check for explicitly defined adapted interfaces that are missing
			for _, ext := range tc.adapted {
				if !implemented[ext] {
					t.Errorf("%s is expected to adapt %s, but it does not implement it.", tc.name, ext)
				}
			}

		})
	}
}
