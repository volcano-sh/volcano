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

package fairshare

import (
	"math"
	"testing"
	"time"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const eps = 1e-9

func assertShare(t *testing.T, shares map[string]float64, namespace string, expected float64) {
	t.Helper()
	got, ok := shares[namespace]
	if !ok {
		t.Errorf("namespace %q: not found in shares (expected %.1f)", namespace, expected)
		return
	}
	if math.Abs(got-expected) > eps {
		t.Errorf("namespace %q: got %.4f, want %.4f", namespace, got, expected)
	}
}

func totalShares(shares map[string]float64) float64 {
	total := 0.0
	for _, s := range shares {
		total += s
	}
	return total
}

// --- CalculateFairShares tests ---

func TestFairShares_SingleNamespace(t *testing.T) {
	demand := map[string]float64{"alice": 200}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "alice", 87)
}

func TestFairShares_SingleNamespaceLowDemand(t *testing.T) {
	demand := map[string]float64{"alice": 10}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "alice", 10)
}

func TestFairShares_TwoNamespacesEqual(t *testing.T) {
	demand := map[string]float64{
		"alice": 100,
		"bob":   100,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "alice", 43.5)
	assertShare(t, shares, "bob", 43.5)
}

func TestFairShares_TwoNamespacesOneLow(t *testing.T) {
	demand := map[string]float64{
		"alice": 200,
		"bob":   10,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "bob", 10)
	assertShare(t, shares, "alice", 77)
}

func TestFairShares_ThreeNamespacesAsymmetric(t *testing.T) {
	demand := map[string]float64{
		"A": 50,
		"B": 60,
		"C": 11,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "C", 11)
	assertShare(t, shares, "A", 38)
	assertShare(t, shares, "B", 38)

	if total := totalShares(shares); total > 87+eps {
		t.Errorf("total shares %.4f exceeds 87", total)
	}
}

func TestFairShares_ThreeNamespacesMixed(t *testing.T) {
	demand := map[string]float64{
		"A": 200,
		"B": 150,
		"C": 10,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "C", 10)
	assertShare(t, shares, "A", 38.5)
	assertShare(t, shares, "B", 38.5)
}

func TestFairShares_AllNamespacesLowDemand(t *testing.T) {
	demand := map[string]float64{
		"alice": 5,
		"bob":   10,
		"carol": 3,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "alice", 5)
	assertShare(t, shares, "bob", 10)
	assertShare(t, shares, "carol", 3)

	if total := totalShares(shares); total > 18+eps {
		t.Errorf("total shares %.4f exceeds sum of demands", total)
	}
}

func TestFairShares_EmptyDemand(t *testing.T) {
	shares := CalculateFairShares(map[string]float64{}, 87)
	if len(shares) != 0 {
		t.Errorf("expected empty shares, got %v", shares)
	}
}

func TestFairShares_ZeroResource(t *testing.T) {
	demand := map[string]float64{"alice": 10}
	shares := CalculateFairShares(demand, 0)
	if len(shares) != 0 {
		t.Errorf("expected empty shares with 0 resources, got %v", shares)
	}
}

func TestFairShares_ZeroDemandNamespaces(t *testing.T) {
	demand := map[string]float64{
		"alice": 0,
		"bob":   50,
	}
	shares := CalculateFairShares(demand, 87)
	assertShare(t, shares, "bob", 50)
	if _, ok := shares["alice"]; ok {
		t.Errorf("alice should not have a share (zero demand)")
	}
}

func TestFairShares_ManyNamespacesProgressiveElimination(t *testing.T) {
	demand := map[string]float64{
		"A": 50,
		"B": 40,
		"C": 25,
		"D": 15,
		"E": 5,
	}
	shares := CalculateFairShares(demand, 100)
	assertShare(t, shares, "E", 5)
	assertShare(t, shares, "D", 15)
	assertShare(t, shares, "C", 25)
	assertShare(t, shares, "A", 27.5)
	assertShare(t, shares, "B", 27.5)

	if total := totalShares(shares); total > 100+eps {
		t.Errorf("total shares %.4f exceeds 100", total)
	}
}

func TestFairShares_NeverExceedsTotalResource(t *testing.T) {
	testCases := []struct {
		name   string
		demand map[string]float64
		total  float64
	}{
		{"high_demand", map[string]float64{"a": 500, "b": 500}, 87},
		{"mixed", map[string]float64{"a": 5, "b": 500, "c": 1}, 87},
		{"exact_fit", map[string]float64{"a": 29, "b": 29, "c": 29}, 87},
		{"over_demand", map[string]float64{"a": 87, "b": 87, "c": 87}, 87},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shares := CalculateFairShares(tc.demand, tc.total)
			total := totalShares(shares)
			if total > tc.total+eps {
				t.Errorf("total shares %.4f exceeds %.0f; shares=%v", total, tc.total, shares)
			}
		})
	}
}

func TestFairShares_NeverExceedsDemand(t *testing.T) {
	demand := map[string]float64{
		"a": 5,
		"b": 10,
		"c": 200,
	}
	shares := CalculateFairShares(demand, 87)

	for namespace, share := range shares {
		if share > demand[namespace]+eps {
			t.Errorf("namespace %q: share %.4f exceeds demand %.4f", namespace, share, demand[namespace])
		}
	}
}

func TestFairShares_CPUResource(t *testing.T) {
	demand := map[string]float64{
		"A": 150000,
		"B": 100000,
		"C": 20000,
	}
	shares := CalculateFairShares(demand, 200000)
	assertShare(t, shares, "C", 20000)
	assertShare(t, shares, "A", 90000)
	assertShare(t, shares, "B", 90000)
}

// --- DecayFactor tests ---

func TestDecayFactor_OneHalfLife(t *testing.T) {
	factor := DecayFactor(4*time.Hour, 4*time.Hour)
	if math.Abs(factor-0.5) > eps {
		t.Errorf("one half-life: got %f, want 0.5", factor)
	}
}

func TestDecayFactor_TwoHalfLives(t *testing.T) {
	factor := DecayFactor(8*time.Hour, 4*time.Hour)
	if math.Abs(factor-0.25) > eps {
		t.Errorf("two half-lives: got %f, want 0.25", factor)
	}
}

func TestDecayFactor_ZeroElapsed(t *testing.T) {
	factor := DecayFactor(0, 4*time.Hour)
	if math.Abs(factor-1.0) > eps {
		t.Errorf("zero elapsed: got %f, want 1.0", factor)
	}
}

func TestDecayFactor_ZeroHalfLife(t *testing.T) {
	factor := DecayFactor(1*time.Hour, 0)
	if math.Abs(factor-1.0) > eps {
		t.Errorf("zero half-life: got %f, want 1.0", factor)
	}
}

func TestDecayFactor_SmallElapsed(t *testing.T) {
	factor := DecayFactor(1*time.Second, 4*time.Hour)
	expected := math.Pow(2.0, -1.0/14400.0)
	if math.Abs(factor-expected) > eps {
		t.Errorf("1s elapsed: got %f, want %f", factor, expected)
	}
	if factor < 0.999 {
		t.Errorf("1s elapsed should be nearly 1.0, got %f", factor)
	}
}

// --- decayAllUsage tests ---

func TestDecayAllUsage_HalvesAfterOneHalfLife(t *testing.T) {
	globalMu.Lock()
	oldUsage := globalUsage
	globalUsage = map[string]map[string]float64{
		"gpu-queue": {"alice": 1000.0, "bob": 500.0},
	}
	decayAllUsage(4*time.Hour, 4*time.Hour)
	alice := globalUsage["gpu-queue"]["alice"]
	bob := globalUsage["gpu-queue"]["bob"]
	globalUsage = oldUsage
	globalMu.Unlock()

	if math.Abs(alice-500.0) > 0.01 {
		t.Errorf("alice: got %.2f, want 500.0", alice)
	}
	if math.Abs(bob-250.0) > 0.01 {
		t.Errorf("bob: got %.2f, want 250.0", bob)
	}
}

func TestDecayAllUsage_CleansUpNegligible(t *testing.T) {
	globalMu.Lock()
	oldUsage := globalUsage
	globalUsage = map[string]map[string]float64{
		"q": {"ns": 0.005},
	}
	decayAllUsage(1*time.Hour, 1*time.Minute)
	_, exists := globalUsage["q"]["ns"]
	globalUsage = oldUsage
	globalMu.Unlock()

	if exists {
		t.Error("expected negligible usage to be cleaned up")
	}
}

func TestDecayAllUsage_MultipleQueues(t *testing.T) {
	globalMu.Lock()
	oldUsage := globalUsage
	globalUsage = map[string]map[string]float64{
		"gpu-queue": {"alice": 800.0},
		"cpu-queue": {"bob": 400.0},
	}
	decayAllUsage(4*time.Hour, 4*time.Hour)
	alice := globalUsage["gpu-queue"]["alice"]
	bob := globalUsage["cpu-queue"]["bob"]
	globalUsage = oldUsage
	globalMu.Unlock()

	if math.Abs(alice-400.0) > 0.01 {
		t.Errorf("alice: got %.2f, want 400.0", alice)
	}
	if math.Abs(bob-200.0) > 0.01 {
		t.Errorf("bob: got %.2f, want 200.0", bob)
	}
}

// --- Usage ordering simulation ---

func TestUsageOrdering_LowerUsageWins(t *testing.T) {
	usage := map[string]float64{
		"ns-a": 100.0,
		"ns-b": 0.0,
	}

	lUsage := usage["ns-b"]
	rUsage := usage["ns-a"]

	if lUsage >= rUsage-usageEpsilon {
		t.Errorf("ns-b (%.1f) should beat ns-a (%.1f)", lUsage, rUsage)
	}
}

func TestUsageOrdering_EqualUsageFallsToRunning(t *testing.T) {
	lUsage := 50.0
	rUsage := 50.0
	lRunning := 0.0
	rRunning := 1.0

	usageTied := math.Abs(lUsage-rUsage) <= usageEpsilon
	if !usageTied {
		t.Fatal("usage should be tied")
	}
	if lRunning >= rRunning {
		t.Error("l should win on running tiebreaker")
	}
}

func TestUsageOrdering_RealisticScenario(t *testing.T) {
	usage := map[string]float64{
		"ns-a": 180.0,
		"ns-b": 60.0,
		"ns-c": 0.0,
		"ns-d": 0.0,
	}

	if !(usage["ns-c"] < usage["ns-b"]-usageEpsilon) {
		t.Error("ns-c should beat ns-b")
	}
	if !(usage["ns-b"] < usage["ns-a"]-usageEpsilon) {
		t.Error("ns-b should beat ns-a")
	}
	if math.Abs(usage["ns-c"]-usage["ns-d"]) > usageEpsilon {
		t.Error("ns-c and ns-d should be tied")
	}
}

func TestDecayScenario_10HourJob(t *testing.T) {
	initial := 36000.0
	halfLife := 4 * time.Hour

	f4 := DecayFactor(4*time.Hour, halfLife)
	after4 := initial * f4
	if math.Abs(after4-18000.0) > 1.0 {
		t.Errorf("after 4h: got %.0f, want ~18000", after4)
	}

	f24 := DecayFactor(24*time.Hour, halfLife)
	after24 := initial * f24
	expected24 := 36000.0 / 64.0
	if math.Abs(after24-expected24) > 1.0 {
		t.Errorf("after 24h: got %.0f, want ~%.0f", after24, expected24)
	}
}

// --- Helper tests ---

func TestGetNamespaceFromJob(t *testing.T) {
	fsp := &fairSharePlugin{}

	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{"regular namespace", "team-ml", "team-ml"},
		{"system namespace", "kube-system", "kube-system"},
		{"empty namespace", "", defaultUnknownNamespace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &api.JobInfo{Namespace: tt.namespace}
			got := fsp.getNamespaceFromJob(job)
			if got != tt.want {
				t.Errorf("getNamespaceFromJob() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetResourceKey_Default(t *testing.T) {
	fsp := &fairSharePlugin{
		defaultResource:   "nvidia.com/gpu",
		queueResourceKeys: map[string]string{},
	}

	got := fsp.getResourceKey("gpu-queue")
	if got != "nvidia.com/gpu" {
		t.Errorf("getResourceKey() = %q, want %q", got, "nvidia.com/gpu")
	}
}

func TestGetResourceKey_PerQueueOverride(t *testing.T) {
	fsp := &fairSharePlugin{
		defaultResource: "nvidia.com/gpu",
		queueResourceKeys: map[string]string{
			"cpu-queue": "cpu",
		},
	}

	if got := fsp.getResourceKey("gpu-queue"); got != "nvidia.com/gpu" {
		t.Errorf("gpu-queue: got %q, want %q", got, "nvidia.com/gpu")
	}
	if got := fsp.getResourceKey("cpu-queue"); got != "cpu" {
		t.Errorf("cpu-queue: got %q, want %q", got, "cpu")
	}
}

// --- targetQueues default-to-all-queues tests ---

func TestNew_UnsetTargetQueues_DefaultsToAllQueues(t *testing.T) {
	p := New(framework.Arguments{})
	fsp := p.(*fairSharePlugin)

	if !fsp.targetAllQueues {
		t.Error("targetAllQueues = false, want true when fairshare.targetQueues is unset")
	}
	if len(fsp.targetQueueNames) != 0 {
		t.Errorf("targetQueueNames = %v, want empty when fairshare.targetQueues is unset", fsp.targetQueueNames)
	}
}

func TestNew_ExplicitTargetQueues_DisablesAllQueuesMode(t *testing.T) {
	p := New(framework.Arguments{"fairshare.targetQueues": "gpu-queue, cpu-queue"})
	fsp := p.(*fairSharePlugin)

	if fsp.targetAllQueues {
		t.Error("targetAllQueues = true, want false when fairshare.targetQueues is explicitly set")
	}
	for _, want := range []string{"gpu-queue", "cpu-queue"} {
		if _, ok := fsp.targetQueueNames[want]; !ok {
			t.Errorf("targetQueueNames missing %q: got %v", want, fsp.targetQueueNames)
		}
	}
}

func TestGetQueueName_TargetAllQueues_MatchesAnyQueue(t *testing.T) {
	fsp := &fairSharePlugin{targetAllQueues: true}
	ssn := &framework.Session{
		Queues: map[api.QueueID]*api.QueueInfo{
			"random-queue": {UID: "random-queue", Name: "random-queue"},
		},
	}
	job := &api.JobInfo{Queue: "random-queue"}

	name, targeted := fsp.getQueueName(ssn, job)
	if !targeted || name != "random-queue" {
		t.Errorf("getQueueName() = (%q, %v), want (%q, true)", name, targeted, "random-queue")
	}
}

func TestGetQueueName_ExplicitAllowlist_OnlyMatchesListedQueues(t *testing.T) {
	fsp := &fairSharePlugin{
		targetQueueNames: map[string]struct{}{"gpu-queue": {}},
	}
	ssn := &framework.Session{
		Queues: map[api.QueueID]*api.QueueInfo{
			"gpu-queue":   {UID: "gpu-queue", Name: "gpu-queue"},
			"other-queue": {UID: "other-queue", Name: "other-queue"},
		},
	}

	if _, targeted := fsp.getQueueName(ssn, &api.JobInfo{Queue: "gpu-queue"}); !targeted {
		t.Error("gpu-queue: targeted = false, want true (in allowlist)")
	}
	if _, targeted := fsp.getQueueName(ssn, &api.JobInfo{Queue: "other-queue"}); targeted {
		t.Error("other-queue: targeted = true, want false (not in allowlist)")
	}
}

func TestFormatShares(t *testing.T) {
	shares := map[string]float64{"alice": 43.5, "bob": 43.5}
	result := FormatShares(shares)
	if result == "" {
		t.Error("expected non-empty format result")
	}
}

func TestEnsureGlobalQueueUsage_CreatesMap(t *testing.T) {
	globalMu.Lock()
	oldUsage := globalUsage
	globalUsage = make(map[string]map[string]float64)
	globalMu.Unlock()

	usage := ensureGlobalQueueUsage("new-queue")
	if usage == nil {
		t.Fatal("expected non-nil map")
	}
	usage["alice"] = 42.0

	if globalUsage["new-queue"]["alice"] != 42.0 {
		t.Error("expected usage to be stored in global state")
	}

	globalMu.Lock()
	globalUsage = oldUsage
	globalMu.Unlock()
}
