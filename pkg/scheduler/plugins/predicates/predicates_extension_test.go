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

package predicates

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	fwk "k8s.io/kube-scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

type interfaceStatus struct {
	supported bool
	ignored   bool
}

func TestUpstreamPluginExtensions(t *testing.T) {
	// Initialize predicates plugin to get instances of VolumeBinding and DynamicResources
	ResetVolumeBindingPluginForTest()
	pp := New(nil).(*PredicatesPlugin)
	pp.enabledPredicates.volumeBindingEnable = true
	pp.enabledPredicates.dynamicResourceAllocationEnable = true

	nodeMap := map[string]fwk.NodeInfo{}
	client := k8sfake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	pp.Handle = k8s.NewFramework(
		nodeMap,
		k8s.WithClientSet(client),
		k8s.WithInformerFactory(informerFactory),
	)
	pp.InitPlugin()

	// Retrieve the instantiated plugin wrappers
	vbPlugin, exists := pp.FilterPlugins["VolumeBinding"]
	if !exists {
		t.Fatalf("VolumeBinding plugin was not initialized")
	}

	draPlugin, exists := pp.FilterPlugins["DynamicResources"]
	if !exists {
		t.Fatalf("DynamicResources plugin was not initialized")
	}

	// 1. Declare all known scheduling framework extension interfaces
	interfaces := map[string]reflect.Type{
		"PreFilterPlugin":  reflect.TypeOf((*fwk.PreFilterPlugin)(nil)).Elem(),
		"FilterPlugin":     reflect.TypeOf((*fwk.FilterPlugin)(nil)).Elem(),
		"PostFilterPlugin": reflect.TypeOf((*fwk.PostFilterPlugin)(nil)).Elem(),
		"PreScorePlugin":   reflect.TypeOf((*fwk.PreScorePlugin)(nil)).Elem(),
		"ScorePlugin":      reflect.TypeOf((*fwk.ScorePlugin)(nil)).Elem(),
		"ReservePlugin":    reflect.TypeOf((*fwk.ReservePlugin)(nil)).Elem(),
		"PermitPlugin":     reflect.TypeOf((*fwk.PermitPlugin)(nil)).Elem(),
		"PreBindPlugin":    reflect.TypeOf((*fwk.PreBindPlugin)(nil)).Elem(),
		"BindPlugin":       reflect.TypeOf((*fwk.BindPlugin)(nil)).Elem(),
		"PostBindPlugin":   reflect.TypeOf((*fwk.PostBindPlugin)(nil)).Elem(),
		"QueueSortPlugin":  reflect.TypeOf((*fwk.QueueSortPlugin)(nil)).Elem(),
		"PreEnqueuePlugin": reflect.TypeOf((*fwk.PreEnqueuePlugin)(nil)).Elem(),
		"SignPlugin":       reflect.TypeOf((*fwk.SignPlugin)(nil)).Elem(),
	}

	// 2. Define static whitelists for each plugin
	// Every interface implemented by the plugin must be marked as supported or ignored.
	vbWhitelist := map[string]interfaceStatus{
		"PreFilterPlugin": {supported: true},
		"FilterPlugin":    {supported: true},
		"PreScorePlugin":  {supported: true},
		"ScorePlugin":     {supported: true},
		"ReservePlugin":   {supported: true},
		"PreBindPlugin":   {supported: true},
		"SignPlugin":      {ignored: true},
	}

	draWhitelist := map[string]interfaceStatus{
		"PreFilterPlugin":  {supported: true},
		"FilterPlugin":     {supported: true},
		"PostFilterPlugin": {ignored: true},
		"ScorePlugin":      {supported: true},
		"ReservePlugin":    {supported: true},
		"PreBindPlugin":    {supported: true},
		"PreEnqueuePlugin": {ignored: true},
		"SignPlugin":       {ignored: true},
	}

	// 3. Programmatically inspect VolumeBinding using reflection
	t.Run("VolumeBinding Extensions Guardrail", func(t *testing.T) {
		inspectPluginExtensions(t, vbPlugin, interfaces, vbWhitelist)
	})

	// 4. Programmatically inspect DynamicResources using reflection
	t.Run("DynamicResources Extensions Guardrail", func(t *testing.T) {
		inspectPluginExtensions(t, draPlugin, interfaces, draWhitelist)
	})
}

func inspectPluginExtensions(
	t *testing.T,
	plugin interface{},
	interfaces map[string]reflect.Type,
	whitelist map[string]interfaceStatus,
) {
	pluginType := reflect.TypeOf(plugin)

	// Check each registered interface
	for name, interfaceType := range interfaces {
		implements := pluginType.Implements(interfaceType)
		status, inWhitelist := whitelist[name]

		if implements {
			if !inWhitelist {
				t.Errorf("Upstream plugin %T implements framework interface %s which is NOT in the whitelist. Please verify if Volcano needs to adapt and support this interface, and update the whitelist accordingly.", plugin, name)
			} else if !status.supported && !status.ignored {
				t.Errorf("Upstream plugin %T implements framework interface %s, but its status in the whitelist is neither supported nor ignored.", plugin, name)
			}
		} else {
			if inWhitelist && status.supported {
				t.Errorf("Upstream plugin %T NO LONGER implements framework interface %s (marked as supported). This indicates a breaking change in the upstream dependency!", plugin, name)
			}
		}
	}
}

type mockScorePlugin struct {
	name string
}

func (m *mockScorePlugin) Name() string {
	return m.name
}

func (m *mockScorePlugin) Score(ctx context.Context, state fwk.CycleState, p *v1.Pod, nodeInfo fwk.NodeInfo) (int64, *fwk.Status) {
	return 10, nil
}

func (m *mockScorePlugin) ScoreExtensions() fwk.ScoreExtensions {
	return nil
}

type mockPreScoreAndScorePlugin struct {
	*mockScorePlugin
	preScoreCalled bool
	preScoreStatus *fwk.Status
}

func (m *mockPreScoreAndScorePlugin) PreScore(ctx context.Context, state fwk.CycleState, p *v1.Pod, nodes []fwk.NodeInfo) *fwk.Status {
	m.preScoreCalled = true
	return m.preScoreStatus
}

func TestScorePluginAdapter_PreScore(t *testing.T) {
	t.Run("ScorePlugin without PreScorePlugin", func(t *testing.T) {
		mock := &mockScorePlugin{name: "mock-plugin"}
		adapter := &scorePluginAdapter{ScorePlugin: mock}
		status := adapter.PreScore(context.Background(), nil, nil, nil)
		if status != nil {
			t.Errorf("Expected nil status, got %v", status)
		}
	})

	t.Run("ScorePlugin with PreScorePlugin", func(t *testing.T) {
		expectedStatus := fwk.NewStatus(fwk.Success, "custom status")
		mock := &mockPreScoreAndScorePlugin{
			mockScorePlugin: &mockScorePlugin{name: "mock-prescore-plugin"},
			preScoreStatus:  expectedStatus,
		}
		adapter := &scorePluginAdapter{ScorePlugin: mock}
		status := adapter.PreScore(context.Background(), nil, nil, nil)
		if status != expectedStatus {
			t.Errorf("Expected status %v, got %v", expectedStatus, status)
		}
		if !mock.preScoreCalled {
			t.Errorf("Expected PreScore to be called on the mock plugin")
		}
	})
}

type mockScoreExtensions struct {
	normalizeCalled bool
}

func (m *mockScoreExtensions) NormalizeScore(ctx context.Context, state fwk.CycleState, pod *v1.Pod, scores fwk.NodeScoreList) *fwk.Status {
	m.normalizeCalled = true
	return nil
}

type mockScorePluginWithExtensions struct {
	*mockScorePlugin
	ext *mockScoreExtensions
}

func (m *mockScorePluginWithExtensions) ScoreExtensions() fwk.ScoreExtensions {
	return m.ext
}

func TestScorePluginAdapter_ScoreAndNormalize(t *testing.T) {
	t.Run("Score delegation and nil-safety on NodeInfo", func(t *testing.T) {
		mock := &mockScorePlugin{name: "mock-plugin"}
		adapter := &scorePluginAdapter{ScorePlugin: mock}

		// Verify score delegation and nil safety on NodeInfo (nil nodeInfo)
		score, status := adapter.Score(context.Background(), nil, nil, nil)
		if score != 10 {
			t.Errorf("Expected score 10, got %d", score)
		}
		if status != nil {
			t.Errorf("Expected nil status, got %v", status)
		}

		// Verify ScoreExtensions nil safety
		if adapter.ScoreExtensions() != nil {
			t.Errorf("Expected ScoreExtensions to be nil when not implemented")
		}
	})

	t.Run("ScoreExtensions and Normalize delegation", func(t *testing.T) {
		mock := &mockScorePlugin{name: "mock-plugin"}
		ext := &mockScoreExtensions{}
		mockWithExt := &mockScorePluginWithExtensions{
			mockScorePlugin: mock,
			ext:             ext,
		}
		adapter := &scorePluginAdapter{ScorePlugin: mockWithExt}

		se := adapter.ScoreExtensions()
		if se == nil {
			t.Fatalf("Expected ScoreExtensions to be non-nil")
		}

		status := se.NormalizeScore(context.Background(), nil, nil, nil)
		if status != nil {
			t.Errorf("Expected nil status, got %v", status)
		}
		if !ext.normalizeCalled {
			t.Errorf("Expected NormalizeScore to be called on mock extensions")
		}
	})
}
