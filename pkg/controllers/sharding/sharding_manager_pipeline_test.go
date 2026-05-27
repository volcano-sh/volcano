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

package sharding

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// stubMetricsProvider satisfies NodeMetricsProvider for pipeline tests. The
// metric map is supplied by the caller; the pipeline reads it via
// GetAllNodeMetrics.
type stubMetricsProvider struct {
	metrics map[string]*NodeMetrics
}

func (s *stubMetricsProvider) GetNodeMetrics(name string) *NodeMetrics { return s.metrics[name] }
func (s *stubMetricsProvider) GetAllNodeMetrics() map[string]*NodeMetrics {
	return s.metrics
}
func (s *stubMetricsProvider) UpdateNodeMetrics(string, *NodeMetrics) {}

func nodeWithLabels(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
}

// TestPipelineFilterScoreClamp exercises the full Filter → Score → Clamp
// pipeline through ShardingManager.runPipeline.
func TestPipelineFilterScoreClamp(t *testing.T) {
	// Jesse's Apr 15 example: allocation-rate weight 1, warmup weight 2.
	// Filter (allocation-rate only) keeps util ∈ [0, 0.6].
	// Score combines: alloc.Score (= util) × 1 + warmup.Score (= 0/1) × 2.
	cfg := SchedulerConfig{
		Name:     "vol",
		Type:     "volcano",
		MinNodes: 0,
		MaxNodes: 5,
		Policies: []PolicyRef{
			{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 0.6,
			}},
			{Name: "warmup", Weight: 2, Arguments: map[string]interface{}{
				"warmupLabel":      "warmup",
				"warmupLabelValue": "true",
			}},
		},
	}

	warmupYes := map[string]string{"warmup": "true"}
	warmupNo := map[string]string{"warmup": "false"}

	nodes := []*corev1.Node{
		nodeWithLabels("n1", warmupYes),
		nodeWithLabels("n2", warmupNo),
		nodeWithLabels("n3", warmupYes),
		nodeWithLabels("n4", warmupNo),
		nodeWithLabels("n5", warmupYes), // util 0.65 → filtered out
		nodeWithLabels("n6", warmupNo),  // util 0.90 → filtered out
		nodeWithLabels("n7", warmupYes),
		nodeWithLabels("n8", warmupNo), // missing metrics → filtered out
	}

	metrics := map[string]*NodeMetrics{
		"n1": {NodeName: "n1", CPUUtilization: 0.10},
		"n2": {NodeName: "n2", CPUUtilization: 0.55},
		"n3": {NodeName: "n3", CPUUtilization: 0.05},
		"n4": {NodeName: "n4", CPUUtilization: 0.40},
		"n5": {NodeName: "n5", CPUUtilization: 0.65},
		"n6": {NodeName: "n6", CPUUtilization: 0.90},
		"n7": {NodeName: "n7", CPUUtilization: 0.30},
		// n8 has no metrics
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired

	// Expected weighted scores for survivors:
	//   n1: 0.10 + 2.0 = 2.10
	//   n2: 0.55 + 0.0 = 0.55
	//   n3: 0.05 + 2.0 = 2.05
	//   n4: 0.40 + 0.0 = 0.40
	//   n7: 0.30 + 2.0 = 2.30
	// Sort desc: n7, n1, n3, n2, n4. Clamp to maxNodes=5 → unchanged.
	want := []string{"n7", "n1", "n3", "n2", "n4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pipeline output\n  got:  %v\n  want: %v", got, want)
	}
}

// TestPipelineFilterAndIntersection confirms multiple Filterers AND-combine.
// Two allocation-rate policies with overlapping but distinct CPU ranges:
//   - policy A accepts util ∈ [0.0, 0.5]
//   - policy B accepts util ∈ [0.3, 1.0]
//
// Only nodes accepted by *both* (i.e. util ∈ [0.3, 0.5]) survive filter.
func TestPipelineFilterAndIntersection(t *testing.T) {
	cfg := SchedulerConfig{
		Name:     "vol",
		MaxNodes: 10,
		Policies: []PolicyRef{
			{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
				"minCPUUtil": 0.0, "maxCPUUtil": 0.5,
			}},
			{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
				"minCPUUtil": 0.3, "maxCPUUtil": 1.0,
			}},
		},
	}
	nodes := []*corev1.Node{
		nodeWithLabels("low", nil),  // 0.2 — passes A, fails B
		nodeWithLabels("mid", nil),  // 0.4 — passes both
		nodeWithLabels("high", nil), // 0.7 — fails A, passes B
	}
	metrics := map[string]*NodeMetrics{
		"low":  {NodeName: "low", CPUUtilization: 0.2},
		"mid":  {NodeName: "mid", CPUUtilization: 0.4},
		"high": {NodeName: "high", CPUUtilization: 0.7},
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired
	want := []string{"mid"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AND-intersection should keep only nodes accepted by every Filterer\n  got:  %v\n  want: %v", got, want)
	}
}

// TestPipelineNoFiltererSkipsFilterPhase confirms Q7 — when zero policies
// implement Filterer, the filter phase is a pass-through.
func TestPipelineNoFiltererSkipsFilterPhase(t *testing.T) {
	cfg := SchedulerConfig{
		Name:     "vol",
		Type:     "volcano",
		MaxNodes: 10,
		Policies: []PolicyRef{
			// warmup is Scorer-only; no Filterer in this chain.
			{Name: "warmup", Weight: 1},
		},
	}

	nodes := []*corev1.Node{
		nodeWithLabels("a", map[string]string{"node.volcano.sh/warmup": "true"}),
		nodeWithLabels("b", map[string]string{"node.volcano.sh/warmup": "false"}),
		nodeWithLabels("c", nil), // no labels — should still pass filter
	}
	metrics := map[string]*NodeMetrics{
		"a": {NodeName: "a", CPUUtilization: 0.5},
		"b": {NodeName: "b", CPUUtilization: 0.5},
		"c": {NodeName: "c", CPUUtilization: 0.5},
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired
	if len(got) != 3 {
		t.Errorf("expected all 3 nodes to survive (no filterer), got %v", got)
	}
	// warmup-only sort: 'a' should be first (score 1.0), b/c after (score 0).
	if got[0] != "a" {
		t.Errorf("expected 'a' first via warmup score, got %v", got)
	}
}

// TestPipelineWeightedSum confirms Q1 — Σ (weight × score), not tier ordering.
// A high-util non-warmup node should outrank a low-util warmup node when
// allocation-rate's weight is high enough relative to warmup's.
func TestPipelineWeightedSum(t *testing.T) {
	// allocation-rate weight=10 dominates warmup weight=1. A non-warmup
	// node with util 0.6 (score 6.0) beats a warmup node with util 0.0
	// (score 0.0 + 1.0 = 1.0).
	cfg := SchedulerConfig{
		Name:     "vol",
		Type:     "volcano",
		MaxNodes: 10,
		Policies: []PolicyRef{
			{Name: "allocation-rate", Weight: 10, Arguments: map[string]interface{}{
				"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
			}},
			{Name: "warmup", Weight: 1, Arguments: map[string]interface{}{
				"warmupLabel": "warmup", "warmupLabelValue": "true",
			}},
		},
	}

	nodes := []*corev1.Node{
		nodeWithLabels("warmlow", map[string]string{"warmup": "true"}),
		nodeWithLabels("regulhi", map[string]string{"warmup": "false"}),
	}
	metrics := map[string]*NodeMetrics{
		"warmlow": {NodeName: "warmlow", CPUUtilization: 0.0},
		"regulhi": {NodeName: "regulhi", CPUUtilization: 0.6},
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired
	want := []string{"regulhi", "warmlow"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("weighted sum should let high-weight policy dominate\n  got:  %v\n  want: %v", got, want)
	}
}

// TestPipelineClampHonored confirms maxNodes truncation runs only when a
// node-limit Selector is present in the policy chain.
func TestPipelineClampHonored(t *testing.T) {
	cfg := SchedulerConfig{
		Name:     "vol",
		MaxNodes: 2, // for documentation; the node-limit policy below enforces it
		Policies: []PolicyRef{
			{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
				"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
			}},
			{Name: "node-limit", Arguments: map[string]interface{}{
				"maxNodes": 2,
			}},
		},
	}

	nodes := []*corev1.Node{
		nodeWithLabels("a", nil),
		nodeWithLabels("b", nil),
		nodeWithLabels("c", nil),
		nodeWithLabels("d", nil),
	}
	metrics := map[string]*NodeMetrics{
		"a": {NodeName: "a", CPUUtilization: 0.4},
		"b": {NodeName: "b", CPUUtilization: 0.8},
		"c": {NodeName: "c", CPUUtilization: 0.2},
		"d": {NodeName: "d", CPUUtilization: 0.6},
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired
	if len(got) != 2 {
		t.Errorf("clamp should trim to maxNodes=2, got %d nodes: %v", len(got), got)
	}
	// Sorted by util desc: b(0.8), d(0.6), a(0.4), c(0.2). After clamp: [b, d].
	want := []string{"b", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected top-2 by score after clamp\n  got:  %v\n  want: %v", got, want)
	}
}

// TestPipelineNodeLimitSynthesizedFromScalars verifies deprecated
// MinNodes/MaxNodes still synthesize node-limit for compatibility.
func TestPipelineNodeLimitSynthesizedFromScalars(t *testing.T) {
	spec := SchedulerConfigSpec{
		Name:     "vol",
		Type:     "volcano",
		MinNodes: 1,
		MaxNodes: 2,
		Policies: []PolicySpec{
			{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
				"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
			}},
		},
	}
	applyPolicyDefaults(&spec)

	if !hasPolicy(spec.Policies, "node-limit") {
		t.Fatalf("expected node-limit to be synthesized; got policies: %v", spec.Policies)
	}

	cfg := schedulerConfigFromSpec(spec)
	nodes := []*corev1.Node{
		nodeWithLabels("a", nil),
		nodeWithLabels("b", nil),
		nodeWithLabels("c", nil),
		nodeWithLabels("d", nil),
	}
	metrics := map[string]*NodeMetrics{
		"a": {NodeName: "a", CPUUtilization: 0.4},
		"b": {NodeName: "b", CPUUtilization: 0.8},
		"c": {NodeName: "c", CPUUtilization: 0.2},
		"d": {NodeName: "d", CPUUtilization: 0.6},
	}

	mgr := NewShardingManager([]SchedulerConfig{cfg}, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	got := assignments["vol"].NodesDesired
	want := []string{"b", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("synthesized node-limit should clamp to top-2 by score\n  got:  %v\n  want: %v", got, want)
	}
}

// TestPipelineDropAssigned confirms cross-scheduler exclusivity — the
// hoisted dropAssigned step removes nodes already claimed by an earlier
// scheduler in the iteration.
func TestPipelineDropAssigned(t *testing.T) {
	cfgs := []SchedulerConfig{
		{
			Name:     "first",
			MaxNodes: 10,
			Policies: []PolicyRef{
				{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
					"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
				}},
			},
		},
		{
			Name:     "second",
			MaxNodes: 10,
			Policies: []PolicyRef{
				{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
					"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
				}},
			},
		},
	}

	nodes := []*corev1.Node{
		nodeWithLabels("x", nil),
		nodeWithLabels("y", nil),
	}
	metrics := map[string]*NodeMetrics{
		"x": {NodeName: "x", CPUUtilization: 0.9},
		"y": {NodeName: "y", CPUUtilization: 0.1},
	}

	mgr := NewShardingManager(cfgs, &stubMetricsProvider{metrics: metrics})
	assignments, err := mgr.CalculateShardAssignments(nodes, nil)
	if err != nil {
		t.Fatalf("CalculateShardAssignments error: %v", err)
	}

	first := assignments["first"].NodesDesired
	second := assignments["second"].NodesDesired

	// First scheduler claims both (no max). Second sees zero candidates.
	if len(first) != 2 {
		t.Errorf("first scheduler expected 2 nodes, got %v", first)
	}
	if len(second) != 0 {
		t.Errorf("second scheduler expected 0 nodes (all assigned), got %v", second)
	}
}

// TestApplyPolicyDefaultsSynthesisLegacy checks back-compat synthesis: when
// Policies is empty and only legacy fields are set, applyPolicyDefaults
// produces an equivalent Policies entry.
func TestApplyPolicyDefaultsSynthesisLegacy(t *testing.T) {
	tests := []struct {
		name   string
		spec   SchedulerConfigSpec
		expect []PolicySpec
	}{
		{
			name: "explicit Policies untouched",
			spec: SchedulerConfigSpec{
				Policies: []PolicySpec{{Name: "allocation-rate", Weight: 1}},
			},
			expect: []PolicySpec{{Name: "allocation-rate", Weight: 1}},
		},
		{
			name: "empty Policies, default policy synthesized",
			spec: SchedulerConfigSpec{
				CPUUtilizationMin: 0.2,
				CPUUtilizationMax: 0.7,
			},
			expect: []PolicySpec{
				{
					Name:   "allocation-rate",
					Weight: 1,
					Arguments: map[string]interface{}{
						"minCPUUtil": 0.2,
						"maxCPUUtil": 0.7,
					},
				},
			},
		},
		{
			name: "preferWarmupNodes appends warmup entry",
			spec: SchedulerConfigSpec{
				Arguments:         map[string]interface{}{"minCPUUtil": 0.0, "maxCPUUtil": 1.0},
				PreferWarmupNodes: true,
			},
			expect: []PolicySpec{
				{Name: "allocation-rate", Weight: 1, Arguments: map[string]interface{}{
					"minCPUUtil": 0.0, "maxCPUUtil": 1.0,
				}},
				{Name: "warmup", Weight: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			applyPolicyDefaults(&spec)
			if !reflect.DeepEqual(spec.Policies, tt.expect) {
				t.Errorf("\n  got:  %#v\n  want: %#v", spec.Policies, tt.expect)
			}
		})
	}
}
