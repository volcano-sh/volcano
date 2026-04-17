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
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testPersistConfig() persistConfig {
	return persistConfig{
		enabled:       true,
		namespace:     "test-ns",
		configMapName: "test-fairshare-state",
		flushInterval: 1 * time.Second,
	}
}

func saveAndResetGlobals(t *testing.T) func() {
	t.Helper()
	globalMu.Lock()
	savedUsage := globalUsage
	savedLastCycle := globalLastCycle
	globalUsage = make(map[string]map[string]float64)
	globalLastCycle = time.Time{}
	globalMu.Unlock()

	persistOnce = sync.Once{}
	savedClient := persistClient
	persistClient = nil

	return func() {
		globalMu.Lock()
		globalUsage = savedUsage
		globalLastCycle = savedLastCycle
		globalMu.Unlock()
		persistOnce = sync.Once{}
		persistClient = savedClient
	}
}

func TestFlushState_CreatesConfigMap(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	client := fake.NewSimpleClientset()
	persistClient = client
	cfg := testPersistConfig()

	now := time.Now().Truncate(time.Second)
	globalMu.Lock()
	globalUsage = map[string]map[string]float64{
		"gpu-queue": {"alice": 1000.0, "bob": 500.0},
	}
	globalLastCycle = now
	globalMu.Unlock()

	if err := flushState(cfg); err != nil {
		t.Fatalf("flushState: %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps(cfg.namespace).Get(
		context.TODO(), cfg.configMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}

	data, ok := cm.Data[stateDataKey]
	if !ok {
		t.Fatal("ConfigMap missing state.json key")
	}

	var state persistedState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if math.Abs(state.Queues["gpu-queue"]["alice"]-1000.0) > 0.01 {
		t.Errorf("alice: got %.2f, want 1000.0", state.Queues["gpu-queue"]["alice"])
	}
	if math.Abs(state.Queues["gpu-queue"]["bob"]-500.0) > 0.01 {
		t.Errorf("bob: got %.2f, want 500.0", state.Queues["gpu-queue"]["bob"])
	}
	if !state.LastCycle.Equal(now) {
		t.Errorf("lastCycle: got %v, want %v", state.LastCycle, now)
	}
}

func TestFlushState_UpdatesExistingConfigMap(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	cfg := testPersistConfig()
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.configMapName,
			Namespace: cfg.namespace,
		},
		Data: map[string]string{stateDataKey: `{"lastCycle":"2020-01-01T00:00:00Z","queues":{}}`},
	}
	client := fake.NewSimpleClientset(existing)
	persistClient = client

	now := time.Now().Truncate(time.Second)
	globalMu.Lock()
	globalUsage = map[string]map[string]float64{
		"gpu-queue": {"carol": 200.0},
	}
	globalLastCycle = now
	globalMu.Unlock()

	if err := flushState(cfg); err != nil {
		t.Fatalf("flushState: %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps(cfg.namespace).Get(
		context.TODO(), cfg.configMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}

	var state persistedState
	if err := json.Unmarshal([]byte(cm.Data[stateDataKey]), &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if math.Abs(state.Queues["gpu-queue"]["carol"]-200.0) > 0.01 {
		t.Errorf("carol: got %.2f, want 200.0", state.Queues["gpu-queue"]["carol"])
	}
}

func TestLoadState_PopulatesGlobals(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	cfg := testPersistConfig()
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	stateJSON, _ := json.Marshal(persistedState{
		LastCycle: now,
		Queues: map[string]map[string]float64{
			"gpu-queue": {"alice": 800.0, "bob": 300.0},
			"cpu-queue": {"carol": 50.0},
		},
	})

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.configMapName,
			Namespace: cfg.namespace,
		},
		Data: map[string]string{stateDataKey: string(stateJSON)},
	}
	client := fake.NewSimpleClientset(cm)
	persistClient = client

	if err := loadState(cfg); err != nil {
		t.Fatalf("loadState: %v", err)
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	if !globalLastCycle.Equal(now) {
		t.Errorf("globalLastCycle: got %v, want %v", globalLastCycle, now)
	}
	if math.Abs(globalUsage["gpu-queue"]["alice"]-800.0) > 0.01 {
		t.Errorf("alice: got %.2f, want 800.0", globalUsage["gpu-queue"]["alice"])
	}
	if math.Abs(globalUsage["gpu-queue"]["bob"]-300.0) > 0.01 {
		t.Errorf("bob: got %.2f, want 300.0", globalUsage["gpu-queue"]["bob"])
	}
	if math.Abs(globalUsage["cpu-queue"]["carol"]-50.0) > 0.01 {
		t.Errorf("carol: got %.2f, want 50.0", globalUsage["cpu-queue"]["carol"])
	}
}

func TestLoadState_NoConfigMap_StartsFresh(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	client := fake.NewSimpleClientset()
	persistClient = client
	cfg := testPersistConfig()

	if err := loadState(cfg); err != nil {
		t.Fatalf("loadState should not error on missing ConfigMap: %v", err)
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	if len(globalUsage) != 0 {
		t.Errorf("expected empty globalUsage, got %v", globalUsage)
	}
	if !globalLastCycle.IsZero() {
		t.Errorf("expected zero globalLastCycle, got %v", globalLastCycle)
	}
}

func TestLoadState_EmptyData_StartsFresh(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	cfg := testPersistConfig()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.configMapName,
			Namespace: cfg.namespace,
		},
		Data: map[string]string{},
	}
	client := fake.NewSimpleClientset(cm)
	persistClient = client

	if err := loadState(cfg); err != nil {
		t.Fatalf("loadState: %v", err)
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	if len(globalUsage) != 0 {
		t.Errorf("expected empty globalUsage, got %v", globalUsage)
	}
}

func TestRoundTrip_FlushThenLoad(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	client := fake.NewSimpleClientset()
	persistClient = client
	cfg := testPersistConfig()

	now := time.Now().Truncate(time.Second)
	globalMu.Lock()
	globalUsage = map[string]map[string]float64{
		"q1": {"user-a": 123.45, "user-b": 678.90},
		"q2": {"user-c": 42.0},
	}
	globalLastCycle = now
	globalMu.Unlock()

	if err := flushState(cfg); err != nil {
		t.Fatalf("flushState: %v", err)
	}

	// Clear globals to simulate restart
	globalMu.Lock()
	globalUsage = make(map[string]map[string]float64)
	globalLastCycle = time.Time{}
	globalMu.Unlock()

	if err := loadState(cfg); err != nil {
		t.Fatalf("loadState: %v", err)
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	if !globalLastCycle.Equal(now) {
		t.Errorf("lastCycle: got %v, want %v", globalLastCycle, now)
	}
	if math.Abs(globalUsage["q1"]["user-a"]-123.45) > 0.01 {
		t.Errorf("user-a: got %.2f, want 123.45", globalUsage["q1"]["user-a"])
	}
	if math.Abs(globalUsage["q1"]["user-b"]-678.90) > 0.01 {
		t.Errorf("user-b: got %.2f, want 678.90", globalUsage["q1"]["user-b"])
	}
	if math.Abs(globalUsage["q2"]["user-c"]-42.0) > 0.01 {
		t.Errorf("user-c: got %.2f, want 42.0", globalUsage["q2"]["user-c"])
	}
}

func TestInitPersistence_DisabledIsNoop(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	client := fake.NewSimpleClientset()
	cfg := persistConfig{enabled: false}

	initPersistence(client, cfg)

	if persistClient != nil {
		t.Error("persistClient should remain nil when disabled")
	}
}

func TestLoadState_CorruptJSON_ReturnsError(t *testing.T) {
	restore := saveAndResetGlobals(t)
	defer restore()

	cfg := testPersistConfig()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.configMapName,
			Namespace: cfg.namespace,
		},
		Data: map[string]string{stateDataKey: "not-valid-json{{{"},
	}
	client := fake.NewSimpleClientset(cm)
	persistClient = client

	err := loadState(cfg)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
}
