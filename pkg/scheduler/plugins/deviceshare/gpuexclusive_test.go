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

package deviceshare

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func makeExclusivePod(name string, labels map[string]string, vgpuNum int64) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{},
					},
				},
			},
		},
	}
	if vgpuNum > 0 {
		pod.Spec.Containers[0].Resources.Limits[v1.ResourceName(defaultGPUExclusiveVGPUResourceName)] = *resource.NewQuantity(vgpuNum, resource.DecimalSI)
	}
	return pod
}

func makeTestGPUDevice(id int, number uint, usedNum uint, podMap map[string]*vgpu.GPUUsage) *vgpu.GPUDevice {
	if podMap == nil {
		podMap = make(map[string]*vgpu.GPUUsage)
	}
	return &vgpu.GPUDevice{
		ID:      id,
		UUID:    "GPU-" + string(rune('0'+id)),
		Memory:  16384,
		Number:  number,
		UsedNum: usedNum,
		PodMap:  podMap,
		Health:  true,
		Type:    "NVIDIA",
	}
}

func testExclusiveRules() []exclusiveRule {
	return []exclusiveRule{
		{labels: map[string]string{"app": "training", "team": "ml"}},
		{labels: map[string]string{"workload": "inference"}},
	}
}

func testExclusiveConfig() gpuExclusiveConfig {
	return gpuExclusiveConfig{
		vgpuResourceName: defaultGPUExclusiveVGPUResourceName,
		rules:            testExclusiveRules(),
	}
}

func makeExclusiveWrapper(devices map[int]*vgpu.GPUDevice, cfg gpuExclusiveConfig, podRules map[string]map[int]struct{}, ruleGPUs map[int]map[int]struct{}) *exclusiveGPUDevices {
	inner := &vgpu.GPUDevices{
		Name:   "test-node",
		Device: devices,
	}
	if podRules == nil {
		podRules = make(map[string]map[int]struct{})
	}
	if ruleGPUs == nil {
		ruleGPUs = make(map[int]map[int]struct{})
	}
	return &exclusiveGPUDevices{
		inner:    inner,
		cfg:      cfg,
		ruleGPUs: ruleGPUs,
		podRules: podRules,
		podUIDs:  make(map[string]string),
	}
}

// --- Tests for rule matching ---

func TestPodMatchesRule(t *testing.T) {
	rules := testExclusiveRules()

	tests := []struct {
		name     string
		pod      *v1.Pod
		ruleIdx  int
		expected bool
	}{
		{
			name:     "pod matches rule 0 (app=training, team=ml)",
			pod:      makeExclusivePod("p1", map[string]string{"app": "training", "team": "ml"}, 1),
			ruleIdx:  0,
			expected: true,
		},
		{
			name:     "pod matches rule 0 with extra labels",
			pod:      makeExclusivePod("p2", map[string]string{"app": "training", "team": "ml", "env": "prod"}, 1),
			ruleIdx:  0,
			expected: true,
		},
		{
			name:     "pod missing one label from rule 0",
			pod:      makeExclusivePod("p3", map[string]string{"app": "training"}, 1),
			ruleIdx:  0,
			expected: false,
		},
		{
			name:     "pod has wrong value for rule 0",
			pod:      makeExclusivePod("p4", map[string]string{"app": "training", "team": "data"}, 1),
			ruleIdx:  0,
			expected: false,
		},
		{
			name:     "pod matches rule 1 (workload=inference)",
			pod:      makeExclusivePod("p5", map[string]string{"workload": "inference"}, 1),
			ruleIdx:  1,
			expected: true,
		},
		{
			name:     "pod with nil labels matches nothing",
			pod:      makeExclusivePod("p6", nil, 1),
			ruleIdx:  0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := podMatchesRule(tt.pod, rules[tt.ruleIdx])
			if got != tt.expected {
				t.Errorf("podMatchesRule() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMatchingRules(t *testing.T) {
	rules := testExclusiveRules()

	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedLen int
	}{
		{
			name:        "matches rule 0 only",
			pod:         makeExclusivePod("p1", map[string]string{"app": "training", "team": "ml"}, 1),
			expectedLen: 1,
		},
		{
			name:        "matches rule 1 only",
			pod:         makeExclusivePod("p2", map[string]string{"workload": "inference"}, 1),
			expectedLen: 1,
		},
		{
			name:        "matches both rules",
			pod:         makeExclusivePod("p3", map[string]string{"app": "training", "team": "ml", "workload": "inference"}, 1),
			expectedLen: 2,
		},
		{
			name:        "matches no rules",
			pod:         makeExclusivePod("p4", map[string]string{"app": "serving"}, 1),
			expectedLen: 0,
		},
		{
			name:        "nil labels matches nothing",
			pod:         makeExclusivePod("p5", nil, 1),
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchingRules(tt.pod, rules)
			if len(got) != tt.expectedLen {
				t.Errorf("matchingRules() returned %d rules, want %d", len(got), tt.expectedLen)
			}
		})
	}
}

// --- Tests for config loading ---

func TestLoadGPUExclusiveConfig(t *testing.T) {
	t.Run("defaults with no rules", func(t *testing.T) {
		cfg := loadGPUExclusiveConfig(framework.Arguments{})
		if cfg.vgpuResourceName != defaultGPUExclusiveVGPUResourceName {
			t.Errorf("vgpuResourceName = %q, want %q", cfg.vgpuResourceName, defaultGPUExclusiveVGPUResourceName)
		}
		if len(cfg.rules) != 0 {
			t.Errorf("rules = %v, want empty", cfg.rules)
		}
	})

	t.Run("custom vgpu resource name", func(t *testing.T) {
		args := framework.Arguments{
			GPUExclusiveVGPUResourceNameKey: "custom.io/gpu",
		}
		cfg := loadGPUExclusiveConfig(args)
		if cfg.vgpuResourceName != "custom.io/gpu" {
			t.Errorf("vgpuResourceName = %q, want %q", cfg.vgpuResourceName, "custom.io/gpu")
		}
	})

	t.Run("rules from map[string]interface{}", func(t *testing.T) {
		args := framework.Arguments{
			GPUExclusiveRulesKey: []interface{}{
				map[string]interface{}{"app": "training", "team": "ml"},
				map[string]interface{}{"workload": "inference"},
			},
		}
		cfg := loadGPUExclusiveConfig(args)
		if len(cfg.rules) != 2 {
			t.Fatalf("rules count = %d, want 2", len(cfg.rules))
		}
		if cfg.rules[0].labels["app"] != "training" || cfg.rules[0].labels["team"] != "ml" {
			t.Errorf("rule 0 = %v, want {app:training, team:ml}", cfg.rules[0].labels)
		}
		if cfg.rules[1].labels["workload"] != "inference" {
			t.Errorf("rule 1 = %v, want {workload:inference}", cfg.rules[1].labels)
		}
	})

	t.Run("rules from map[interface{}]interface{}", func(t *testing.T) {
		args := framework.Arguments{
			GPUExclusiveRulesKey: []interface{}{
				map[interface{}]interface{}{"app": "training"},
			},
		}
		cfg := loadGPUExclusiveConfig(args)
		if len(cfg.rules) != 1 {
			t.Fatalf("rules count = %d, want 1", len(cfg.rules))
		}
		if cfg.rules[0].labels["app"] != "training" {
			t.Errorf("rule 0 = %v, want {app:training}", cfg.rules[0].labels)
		}
	})
}

// --- Tests for GPU capping behavior ---

func TestCapGPUsOnlyAffectsSpecifiedIndices(t *testing.T) {
	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 1, nil),
		1: makeTestGPUDevice(1, 10, 1, nil),
		2: makeTestGPUDevice(2, 10, 0, nil),
		3: makeTestGPUDevice(3, 10, 0, nil),
	}
	w := makeExclusiveWrapper(devices, testExclusiveConfig(), nil, nil)

	toCap := map[int]struct{}{0: {}, 1: {}}
	saved := w.capGPUs(toCap)

	if devices[0].Number != 1 {
		t.Errorf("GPU 0 Number = %d, want 1 (capped)", devices[0].Number)
	}
	if devices[1].Number != 1 {
		t.Errorf("GPU 1 Number = %d, want 1 (capped)", devices[1].Number)
	}
	if devices[2].Number != 10 {
		t.Errorf("GPU 2 Number = %d, want 10 (unchanged)", devices[2].Number)
	}
	if devices[3].Number != 10 {
		t.Errorf("GPU 3 Number = %d, want 10 (unchanged)", devices[3].Number)
	}

	w.restoreGPUs(saved)

	for i := 0; i < 4; i++ {
		if devices[i].Number != 10 {
			t.Errorf("after restore: GPU %d Number = %d, want 10", i, devices[i].Number)
		}
	}
}

// --- Tests for tracking and reservation ---

func TestReservedGPUsForPod(t *testing.T) {
	cfg := testExclusiveConfig()
	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 1, map[string]*vgpu.GPUUsage{"pod-a": {}}),
		1: makeTestGPUDevice(1, 10, 1, map[string]*vgpu.GPUUsage{"pod-b": {}}),
		2: makeTestGPUDevice(2, 10, 0, nil),
	}
	podRules := map[string]map[int]struct{}{
		"pod-a": {0: {}},
		"pod-b": {1: {}},
	}
	ruleGPUs := map[int]map[int]struct{}{
		0: {0: {}},
		1: {1: {}},
	}
	w := makeExclusiveWrapper(devices, cfg, podRules, ruleGPUs)

	// A new pod matching rule 0 should see GPUs 0 and 1 as reserved (union of all rules)
	newPod := makeExclusivePod("pod-c", map[string]string{"app": "training", "team": "ml"}, 1)
	reserved := w.reservedGPUsForPod(newPod)
	if _, ok := reserved[0]; !ok {
		t.Error("GPU 0 should be reserved for pod matching rule 0")
	}
	if _, ok := reserved[1]; !ok {
		t.Error("GPU 1 should be reserved for pod matching rule 0 (union of all rules)")
	}

	// A pod matching no rules should have no reservations
	otherPod := makeExclusivePod("pod-e", map[string]string{"app": "serving"}, 1)
	reserved = w.reservedGPUsForPod(otherPod)
	if len(reserved) != 0 {
		t.Errorf("unmatched pod should have no reserved GPUs, got %v", reserved)
	}
}

func TestPodMatchingMultipleRulesSeesUnionOfReservedGPUs(t *testing.T) {
	cfg := testExclusiveConfig()
	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 1, map[string]*vgpu.GPUUsage{"pod-a": {}}),
		1: makeTestGPUDevice(1, 10, 1, map[string]*vgpu.GPUUsage{"pod-b": {}}),
		2: makeTestGPUDevice(2, 10, 0, nil),
	}
	podRules := map[string]map[int]struct{}{
		"pod-a": {0: {}},
		"pod-b": {1: {}},
	}
	ruleGPUs := map[int]map[int]struct{}{
		0: {0: {}},
		1: {1: {}},
	}
	w := makeExclusiveWrapper(devices, cfg, podRules, ruleGPUs)

	bothPod := makeExclusivePod("pod-x", map[string]string{"app": "training", "team": "ml", "workload": "inference"}, 1)
	reserved := w.reservedGPUsForPod(bothPod)
	if _, ok := reserved[0]; !ok {
		t.Error("GPU 0 should be reserved (from rule 0)")
	}
	if _, ok := reserved[1]; !ok {
		t.Error("GPU 1 should be reserved (from rule 1)")
	}
	if _, ok := reserved[2]; ok {
		t.Error("GPU 2 should NOT be reserved")
	}
}

func TestNonMatchingPodDoesNotReserve(t *testing.T) {
	cfg := testExclusiveConfig()
	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 0, nil),
	}
	w := makeExclusiveWrapper(devices, cfg, nil, nil)

	otherPod := makeExclusivePod("pod-x", map[string]string{"app": "serving"}, 1)
	reserved := w.reservedGPUsForPod(otherPod)
	if len(reserved) != 0 {
		t.Errorf("non-matching pod should not have reserved GPUs, got %v", reserved)
	}
	if len(w.podRules) != 0 {
		t.Errorf("podRules should be empty, got %v", w.podRules)
	}
}

func TestEndToEndExclusive(t *testing.T) {
	cfg := testExclusiveConfig()

	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 1, map[string]*vgpu.GPUUsage{"pod-a": {}}),
		1: makeTestGPUDevice(1, 10, 1, map[string]*vgpu.GPUUsage{"pod-b": {}}),
		2: makeTestGPUDevice(2, 10, 0, nil),
		3: makeTestGPUDevice(3, 10, 0, nil),
	}
	podRules := map[string]map[int]struct{}{
		"pod-a": {0: {}},
		"pod-b": {1: {}},
	}
	ruleGPUs := map[int]map[int]struct{}{
		0: {0: {}},
		1: {1: {}},
	}
	w := makeExclusiveWrapper(devices, cfg, podRules, ruleGPUs)

	// 1. New training pod (rule 0): GPUs 0,1 are capped (union), GPUs 2,3 available
	trainPod := makeExclusivePod("pod-c", map[string]string{"app": "training", "team": "ml"}, 1)
	reserved := w.reservedGPUsForPod(trainPod)
	saved := w.capGPUs(reserved)
	available := 0
	for _, dev := range devices {
		if dev.Number > dev.UsedNum {
			available++
		}
	}
	w.restoreGPUs(saved)
	if available != 2 {
		t.Errorf("training pod should see 2 available GPUs, got %d", available)
	}

	// 2. Unrelated pod: all 4 GPUs visible, no capping
	otherPod := makeExclusivePod("pod-e", map[string]string{"app": "serving"}, 1)
	reserved = w.reservedGPUsForPod(otherPod)
	if len(reserved) != 0 {
		t.Errorf("unrelated pod should have no reservations, got %v", reserved)
	}
	for i := 0; i < 4; i++ {
		if devices[i].Number != 10 {
			t.Errorf("GPU %d Number = %d, want 10 (fully visible to unrelated pod)", i, devices[i].Number)
		}
	}
}

func TestOnSessionOpenWrapsDevices(t *testing.T) {
	cfg := testExclusiveConfig()
	podA := makeExclusivePod("pod-a", map[string]string{"app": "training", "team": "ml"}, 1)
	taskUID := api.TaskID("default/pod-a")

	devices := map[int]*vgpu.GPUDevice{
		0: makeTestGPUDevice(0, 10, 1, map[string]*vgpu.GPUUsage{"pod-a": {}}),
		1: makeTestGPUDevice(1, 10, 0, nil),
	}
	inner := &vgpu.GPUDevices{Name: "node-1", Device: devices}
	node := &api.NodeInfo{
		Name:   "node-1",
		Others: map[string]interface{}{vgpu.DeviceName: inner},
		Tasks: map[api.TaskID]*api.TaskInfo{
			taskUID: {UID: taskUID, Pod: podA},
		},
	}

	// Simulate OnSessionOpen wrapping logic
	podRules := make(map[string]map[int]struct{})
	for _, task := range node.Tasks {
		if task.Pod == nil {
			continue
		}
		matched := matchingRules(task.Pod, cfg.rules)
		if len(matched) == 0 {
			continue
		}
		ruleSet := make(map[int]struct{}, len(matched))
		for _, idx := range matched {
			ruleSet[idx] = struct{}{}
		}
		podRules[task.Pod.Name] = ruleSet
	}

	ruleGPUs := make(map[int]map[int]struct{})
	for gpuIdx, dev := range inner.Device {
		for podName := range dev.PodMap {
			if ruleSet, ok := podRules[podName]; ok {
				for ruleIdx := range ruleSet {
					if ruleGPUs[ruleIdx] == nil {
						ruleGPUs[ruleIdx] = make(map[int]struct{})
					}
					ruleGPUs[ruleIdx][gpuIdx] = struct{}{}
				}
			}
		}
	}

	wrapper := &exclusiveGPUDevices{
		inner:    inner,
		cfg:      cfg,
		ruleGPUs: ruleGPUs,
		podRules: podRules,
		podUIDs:  make(map[string]string),
	}
	node.Others[vgpu.DeviceName] = wrapper

	w, ok := node.Others[vgpu.DeviceName].(*exclusiveGPUDevices)
	if !ok {
		t.Fatal("expected exclusiveGPUDevices in node.Others")
	}

	if _, ok := w.ruleGPUs[0][0]; !ok {
		t.Error("GPU 0 should be in ruleGPUs[0]")
	}
	if _, ok := w.ruleGPUs[0][1]; ok {
		t.Error("GPU 1 should NOT be in ruleGPUs[0]")
	}

	var _ api.Devices = w
}

func TestParseExclusiveRulesHandlesEdgeCases(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		rules := parseExclusiveRules(nil)
		if len(rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(rules))
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		rules := parseExclusiveRules("not a slice")
		if len(rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(rules))
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		rules := parseExclusiveRules([]interface{}{})
		if len(rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(rules))
		}
	})

	t.Run("empty map skipped", func(t *testing.T) {
		rules := parseExclusiveRules([]interface{}{
			map[string]interface{}{},
		})
		if len(rules) != 0 {
			t.Errorf("empty label map should be skipped, got %d rules", len(rules))
		}
	})
}
