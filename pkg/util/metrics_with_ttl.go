package util

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/klog/v2"
)

type CollectorWrapper struct {
	vec        prometheus.Collector
	labelNames []string
	Labels     sync.Map // key (joined label values) -> last update time
}

func NewCollectorWrapper(vec prometheus.Collector) *CollectorWrapper {
	return &CollectorWrapper{vec: vec}
}

func NewCollectorWrapperWithLabelNames(vec prometheus.Collector, labelNames []string) *CollectorWrapper {
	return &CollectorWrapper{vec: vec, labelNames: labelNames}
}

func (w *CollectorWrapper) ObserveWithLabelValues(value float64, lvs ...string) {
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		v.WithLabelValues(lvs...).Set(value)
	case *prometheus.CounterVec:
		v.WithLabelValues(lvs...).Add(value)
	case *prometheus.HistogramVec:
		v.WithLabelValues(lvs...).Observe(value)
	case *prometheus.SummaryVec:
		v.WithLabelValues(lvs...).Observe(value)
	}
	w.Labels.Store(LabelsAsKey(lvs...), time.Now())
}

func (w *CollectorWrapper) DeleteLabel(key string) {
	lvs := KeyToLabels(key)
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.CounterVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.HistogramVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.SummaryVec:
		v.DeleteLabelValues(lvs...)
	}
	w.Labels.Delete(key)
}

// DeleteLabelValues deletes all metrics where the variable labels match the provided label values.
// It uses the label key order to match correctly.
func (w *CollectorWrapper) DeleteLabelValues(lvs ...string) {
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.CounterVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.HistogramVec:
		v.DeleteLabelValues(lvs...)
	case *prometheus.SummaryVec:
		v.DeleteLabelValues(lvs...)
	}

	w.Labels.Delete(LabelsAsKey(lvs...))
}

// DeletePartialMatch deletes all metrics where the variable labels contain all of those passed in as labels.
// It uses the label key order to match correctly.
func (w *CollectorWrapper) DeletePartialMatch(labels prometheus.Labels) {
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		v.DeletePartialMatch(labels)
	case *prometheus.CounterVec:
		v.DeletePartialMatch(labels)
	case *prometheus.HistogramVec:
		v.DeletePartialMatch(labels)
	case *prometheus.SummaryVec:
		v.DeletePartialMatch(labels)
	}

	// Synchronously remove matching metrics from the cache
	w.Labels.Range(func(k, _ interface{}) bool {
		keyLabels := KeyToLabels(k.(string))
		if w.matchByLabelOrder(keyLabels, labels) {
			w.Labels.Delete(k)
		}
		return true
	})
}

func (w *CollectorWrapper) Vec() prometheus.Collector {
	return w.vec
}

func (w *CollectorWrapper) Reset() {
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		v.Reset()
	case *prometheus.CounterVec:
		v.Reset()
	case *prometheus.HistogramVec:
		v.Reset()
	case *prometheus.SummaryVec:
		v.Reset()
	}

	// Synchronously remove matching metrics from the cache
	w.Labels.Range(func(k, _ interface{}) bool {
		w.Labels.Delete(k)
		return true
	})
}

func (w *CollectorWrapper) LabelNames() []string {
	return w.labelNames
}

// matchByLabelOrder checks whether the given labels (map) match the stored label values
// according to the wrapper's labelNames order.
func (w *CollectorWrapper) matchByLabelOrder(keyLabels []string, matchLabels prometheus.Labels) bool {
	if len(w.labelNames) == 0 || len(keyLabels) != len(w.labelNames) {
		return false
	}

	for i, labelName := range w.labelNames {
		matchVal, ok := matchLabels[labelName]
		if ok && matchVal != keyLabels[i] {
			return false
		}
	}
	return true
}

// TTLCollectorWrapper prometheus.collector with ttl
type TTLCollectorWrapper struct {
	*CollectorWrapper
	expiration    time.Duration
	checkInterval time.Duration
	ctx           context.Context
}

func NewTTLCollectorWrapper(vec prometheus.Collector, labelNames []string, expiration, checkInterval time.Duration, ctx context.Context) *TTLCollectorWrapper {
	wrapper := &TTLCollectorWrapper{
		CollectorWrapper: NewCollectorWrapperWithLabelNames(vec, labelNames),
		expiration:       expiration,
		checkInterval:    checkInterval,
		ctx:              ctx,
	}
	go wrapper.Start()
	return wrapper
}

func (w *TTLCollectorWrapper) ObserveWithLabelValues(value float64, lvs ...string) {
	w.CollectorWrapper.ObserveWithLabelValues(value, lvs...)
}

func (w *TTLCollectorWrapper) DeletePartialMatch(labels prometheus.Labels) {
	w.CollectorWrapper.DeletePartialMatch(labels)
}

func (w *TTLCollectorWrapper) DeleteLabelValues(lvs ...string) {
	w.CollectorWrapper.DeleteLabelValues(lvs...)
}

func (w *TTLCollectorWrapper) Vec() prometheus.Collector {
	return w.vec
}

func (w *TTLCollectorWrapper) LabelNames() []string {
	return w.labelNames
}

func (w *TTLCollectorWrapper) checkTTL() {
	start := time.Now()
	count := 0
	w.Labels.Range(func(k, v interface{}) bool {
		if lastUpdated, ok := v.(time.Time); ok && start.Sub(lastUpdated) > w.expiration {
			w.CollectorWrapper.DeleteLabel(k.(string))
			count++
		}
		return true
	})
	cost := time.Since(start).Milliseconds()
	klog.V(3).Infof("clear %d timeouted metrics, cost %d(ms)", count, cost)
}

func (w *TTLCollectorWrapper) Start() {
	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkTTL()
		case <-w.ctx.Done():
			klog.V(3).Infof("stop check ttl for ttl metrics")
			return
		}
	}
}

func LabelsAsKey(lvs ...string) string {
	return strings.Join(lvs, ",")
}

func KeyToLabels(key string) []string {
	if key == "" {
		return []string{}
	}
	return strings.Split(key, ",")
}

func NewTTLGaugeVec(opts prometheus.GaugeOpts, labelNames []string, expiration, checkInterval time.Duration, ctx context.Context) *TTLCollectorWrapper {
	return NewTTLCollectorWrapper(promauto.NewGaugeVec(opts, labelNames), labelNames, expiration, checkInterval, ctx)
}

func NewTTLGaugeVecWithRegister(register prometheus.Registerer, opts prometheus.GaugeOpts, labelNames []string, expiration, checkInterval time.Duration, ctx context.Context) *TTLCollectorWrapper {
	return NewTTLCollectorWrapper(promauto.With(register).NewGaugeVec(opts, labelNames), labelNames, expiration, checkInterval, ctx)
}

func NewGaugeVecWithRegister(register prometheus.Registerer, opts prometheus.GaugeOpts, labelNames []string) *CollectorWrapper {
	return NewCollectorWrapperWithLabelNames(promauto.With(register).NewGaugeVec(opts, labelNames), labelNames)
}

func TestutilToFloat64(w *TTLCollectorWrapper, lvs ...string) float64 {
	switch v := w.vec.(type) {
	case *prometheus.GaugeVec:
		return testutil.ToFloat64(v.WithLabelValues(lvs...))
	case *prometheus.CounterVec:
		return testutil.ToFloat64(v.WithLabelValues(lvs...))
	}
	return -1
}

func TestutilToCollectAndCount(w *TTLCollectorWrapper, lvs ...string) int {
	return testutil.CollectAndCount(w.vec, lvs...)
}
