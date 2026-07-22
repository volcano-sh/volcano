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
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	defaultConfigMapName  = "fairshare-usage-state"
	defaultStateNamespace = "volcano-system"
	defaultFlushInterval  = 30 * time.Second
	stateDataKey          = "state.json"
)

// persistedState is the JSON structure stored in the ConfigMap.
type persistedState struct {
	LastCycle time.Time                     `json:"lastCycle"`
	Queues    map[string]map[string]float64 `json:"queues"`
}

// persistConfig holds persistence settings parsed from plugin arguments.
type persistConfig struct {
	enabled       bool
	namespace     string
	configMapName string
	flushInterval time.Duration
}

var (
	persistOnce   sync.Once
	persistClient kubernetes.Interface

	// flushMu guards lastFlushAt. Kept separate from globalMu so checking
	// the flush deadline never contends with the scheduling-critical usage
	// lock.
	flushMu     sync.Mutex
	lastFlushAt time.Time
)

// initPersistence loads existing state from the ConfigMap on the first
// scheduling cycle. Uses sync.Once so only the first call has effect.
// Must be called BEFORE acquiring globalMu to avoid deadlock.
func initPersistence(client kubernetes.Interface, cfg persistConfig) {
	if !cfg.enabled {
		return
	}
	persistOnce.Do(func() {
		persistClient = client
		if err := loadState(cfg); err != nil {
			klog.Warningf("fairshare: failed to load persisted state: %v (starting fresh)", err)
		}
		klog.V(2).Infof("fairshare: persistence enabled — namespace=%s configMap=%s flushInterval=%s",
			cfg.namespace, cfg.configMapName, cfg.flushInterval)
	})
}

// maybeFlush flushes globalUsage to the ConfigMap if at least
// cfg.flushInterval has elapsed since the last flush. Called from
// OnSessionClose (once per scheduling cycle, ~1s) rather than from a
// detached goroutine+ticker: a ticker outlives the plugin's config —
// removing `fairshare` from the tier list (or a scheduler config hot
// reload) doesn't stop it, so it keeps writing stale state forever. Tying
// the flush to OnSessionClose means persistence naturally stops the moment
// the plugin stops being invoked, with no separate shutdown path needed.
// Rate-limiting here keeps the ConfigMap write cadence the same as before
// (every flushInterval) rather than once per ~1s scheduling cycle.
func maybeFlush(cfg persistConfig) {
	if !cfg.enabled {
		return
	}

	// Hold flushMu across the due-check *and* the flush itself (not just
	// the check). Volcano's main scheduler calls OnSessionClose
	// sequentially today, but nothing here should assume that forever —
	// if maybeFlush were ever called concurrently (e.g. from an
	// Agent-Scheduler-style multi-worker setup), releasing the lock
	// between the check and the flush would let two callers both observe
	// due=true and both call flushState in parallel: duplicate API calls
	// and a possible resourceVersion conflict on Update. Holding the lock
	// for the whole sequence serializes flush attempts instead.
	flushMu.Lock()
	defer flushMu.Unlock()

	if time.Since(lastFlushAt) < cfg.flushInterval {
		return
	}

	// lastFlushAt tracks the last *successful* flush, not the last attempt.
	// Only advance it once flushState actually succeeds, so a transient
	// failure (e.g. a momentary apiserver error) is retried on the very
	// next cycle instead of being suppressed for a full flushInterval.
	if err := flushState(cfg); err != nil {
		klog.Warningf("fairshare: flush failed: %v", err)
		return
	}

	lastFlushAt = time.Now()
}

// apiCallTimeout returns a timeout for individual API calls. We use half the
// flush interval so a slow/stalled apiserver call can't hold flushMu (and
// therefore delay the next OnSessionClose-driven flush attempt) for longer
// than half of flushInterval.
func apiCallTimeout(cfg persistConfig) time.Duration {
	if cfg.flushInterval <= 0 {
		return 15 * time.Second
	}
	return cfg.flushInterval / 2
}

// loadState reads the ConfigMap and populates globalUsage / globalLastCycle.
func loadState(cfg persistConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiCallTimeout(cfg))
	defer cancel()
	cm, err := persistClient.CoreV1().ConfigMaps(cfg.namespace).Get(
		ctx, cfg.configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(2).Infof("fairshare: no persisted state found, starting fresh")
			return nil
		}
		return fmt.Errorf("get ConfigMap %s/%s: %w", cfg.namespace, cfg.configMapName, err)
	}

	data, ok := cm.Data[stateDataKey]
	if !ok || data == "" {
		klog.V(2).Infof("fairshare: ConfigMap exists but no %s key, starting fresh", stateDataKey)
		return nil
	}

	var state persistedState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	if state.Queues != nil {
		globalUsage = state.Queues
	} else {
		globalUsage = make(map[string]map[string]float64)
	}
	globalLastCycle = state.LastCycle

	totalNamespaces := 0
	for _, namespaces := range globalUsage {
		totalNamespaces += len(namespaces)
	}
	klog.V(2).Infof("fairshare: loaded persisted state — lastCycle=%s queues=%d namespaces=%d",
		state.LastCycle.Format(time.RFC3339), len(state.Queues), totalNamespaces)
	return nil
}

// flushState serializes the current globalUsage to the ConfigMap.
func flushState(cfg persistConfig) error {
	globalMu.Lock()
	state := persistedState{
		LastCycle: globalLastCycle,
		Queues:    snapshotUsage(),
	}
	globalMu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiCallTimeout(cfg))
	defer cancel()
	cm, err := persistClient.CoreV1().ConfigMaps(cfg.namespace).Get(
		ctx, cfg.configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return createStateConfigMap(ctx, cfg, string(data))
		}
		return fmt.Errorf("get ConfigMap: %w", err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[stateDataKey] = string(data)
	if _, err := persistClient.CoreV1().ConfigMaps(cfg.namespace).Update(
		ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update ConfigMap: %w", err)
	}

	klog.V(4).Infof("fairshare: flushed state to ConfigMap (%d bytes)", len(data))
	return nil
}

func createStateConfigMap(ctx context.Context, cfg persistConfig, data string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.configMapName,
			Namespace: cfg.namespace,
			Labels: map[string]string{
				"app":       "volcano-scheduler",
				"component": "fairshare",
			},
		},
		Data: map[string]string{
			stateDataKey: data,
		},
	}
	if _, err := persistClient.CoreV1().ConfigMaps(cfg.namespace).Create(
		ctx, cm, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create ConfigMap: %w", err)
	}
	klog.V(2).Infof("fairshare: created state ConfigMap %s/%s", cfg.namespace, cfg.configMapName)
	return nil
}
