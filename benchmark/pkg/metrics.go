package pkg

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// PodsCreationLatency records the latency from VCJob submission (client-side time.Now())
	// to all expected pods being created in the API server, in milliseconds.
	PodsCreationLatency = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "benchmark_pods_creation_latency_milliseconds",
		Help: "Latency from VCJob submission to all pods created (wall-clock), in milliseconds",
	})

	// E2eSchedulingLatency records per-pod end-to-end scheduling latency
	// from pod creation (time.Now() when pod count reaches expected) to pod
	// being bound to a node (spec.nodeName non-empty), in milliseconds.
	E2eSchedulingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "benchmark_e2e_scheduling_latency_milliseconds",
		Help:    "Per-pod scheduling latency from pod creation to node binding (wall-clock), in milliseconds",
		Buckets: prometheus.ExponentialBuckets(10, 2, 15),
	})
)

// StartMetricsServer starts an HTTP server exposing Prometheus metrics on /metrics.
func StartMetricsServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Printf("Warning: metrics server failed to start on %s: %v\n", addr, err)
		}
	}()
}
