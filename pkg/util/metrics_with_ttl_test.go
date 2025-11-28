package util

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTTLCollectorWrapperBench100000(t *testing.T) {
	stopCh := make(chan struct{})
	w := NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "foo",
			Subsystem: "bar",
			Name:      "test",
			Help:      "test with ttl",
		},
		[]string{"id", "name"},
		time.Second*2,
		time.Second*3,
		context.Background(),
	)

	now := time.Now()
	for i := 0; i < 100000; i++ {
		w.ObserveWithLabelValues(float64(i), fmt.Sprintf("id-%d", i), fmt.Sprintf("name-%d", i))
	}
	fmt.Println("ObserveWithLabelValues cost: ", time.Since(now).Milliseconds())

	if v, ok := w.vec.(*prometheus.GaugeVec); ok {
		metricChan := make(chan prometheus.Metric)
		go func() {
			v.Collect(metricChan)
			close(metricChan)
		}()
		count := 0
		for range metricChan {
			count++
		}
		if count != 100000 {
			t.Errorf("Expected 100000 metrics, got %d", count)
		}
	} else {
		t.Errorf("Failed to cast vec to GaugeVec")
	}

	timer := time.NewTimer(time.Second * 6)
	<-timer.C

	if v, ok := w.vec.(*prometheus.GaugeVec); ok {
		metricChan := make(chan prometheus.Metric)
		go func() {
			v.Collect(metricChan)
			close(metricChan)
		}()
		count := 0
		for range metricChan {
			count++
		}
		if count != 0 {
			t.Errorf("Expected 0 metrics after 5 seconds, got %d", count)
		}
	}

	close(stopCh)
	time.Sleep(time.Second * 1)
}

func TestTTLCollectorWrapper_DeletePartialMatch(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	labelNames := []string{"region", "status"}

	// create TTLCollectorWrapper
	gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_metric",
		Help: "metric for testing DeletePartialMatch",
	}, labelNames)

	ttlWrapper := NewTTLCollectorWrapper(
		gaugeVec,
		labelNames,
		time.Minute,
		time.Second*5,
		context.Background(),
	)

	// observe three metrics with different label combinations
	ttlWrapper.ObserveWithLabelValues(1.0, "cn", "success")
	ttlWrapper.ObserveWithLabelValues(2.0, "us", "failed")
	ttlWrapper.ObserveWithLabelValues(3.0, "cn", "failed")

	// check initial number of metrics
	if count := countMapKeys(ttlWrapper.CollectorWrapper); count != 3 {
		t.Fatalf("expected 3 metrics, got %d", count)
	}

	// delete metrics partially matching region=cn
	matchLabels := prometheus.Labels{"region": "cn"}
	ttlWrapper.DeletePartialMatch(matchLabels)

	// verify that matched keys are deleted
	if hasKey(ttlWrapper.CollectorWrapper, "cn,success") {
		t.Errorf("expected metric 'cn,success' to be deleted, but it still exists")
	}
	if hasKey(ttlWrapper.CollectorWrapper, "cn,failed") {
		t.Errorf("expected metric 'cn,failed' to be deleted, but it still exists")
	}

	// verify that unmatched key still exists
	if !hasKey(ttlWrapper.CollectorWrapper, "us,failed") {
		t.Errorf("expected metric 'us,failed' to remain, but it was deleted")
	}

	// final number of metrics should be 1
	if count := countMapKeys(ttlWrapper.CollectorWrapper); count != 1 {
		t.Fatalf("expected 1 metric remaining, got %d", count)
	}
}

func TestDeleteLabelValues(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	labelNames := []string{"label1", "label2"}
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_metric",
		Help: "A test metric",
	}, labelNames)

	w := NewTTLCollectorWrapper(
		vec,
		labelNames,
		time.Minute,
		time.Second*5,
		context.Background(),
	)

	w.ObserveWithLabelValues(1.0, "a", "b")

	// Delete the metric
	w.DeleteLabelValues("a", "b")

	key := LabelsAsKey("a", "b")
	// Check cache is removed
	if _, ok := w.Labels.Load(key); ok {
		t.Errorf("expected key %s to be deleted from cache", key)
	}

	if val := testutil.ToFloat64(w.vec.(*prometheus.GaugeVec).WithLabelValues("a", "b")); val != 0 {
		t.Errorf("expected metric to be deleted or zero, got %v", val)
	}
}

// helper function: count the number of keys in CollectorWrapper's Labels map
func countMapKeys(m *CollectorWrapper) int {
	count := 0
	m.Labels.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// helper function: check whether a key exists in Labels map
func hasKey(m *CollectorWrapper, key string) bool {
	_, ok := m.Labels.Load(key)
	return ok
}
