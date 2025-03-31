package metrics

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
)

func forMetricMap(fn func(map[string]float64)) error {
	url := "http://127.0.0.1:8081/metrics"
	method := "GET"

	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)

	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "8cbdb37a-b880-4f2e-844c-e420858ea7eb")

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	split := strings.Split(string(body), "\n")
	metricSet := map[string]float64{}
	for _, v := range split {
		if !strings.Contains(v, "#") && v != "" {
			pair := strings.Split(v, " ")
			if len(pair) < 2 {
				metricSet[pair[0]] = 0
			}
			data, _ := strconv.ParseFloat(pair[1], 64)
			metricSet[pair[0]] = data
		}
	}
	fn(metricSet)
	return nil
}

func retryOnConnectionRefused(fn func() error) error {
	isConnectionRefused := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "connection refused")
	}
	return retry.OnError(retry.DefaultBackoff, isConnectionRefused, fn)
}

func TestQueueResourceMetric(t *testing.T) {
	UpdateQueueAllocated("queue1", 100, 1024, map[v1.ResourceName]float64{"nvidia.com/gpu": 0})
	UpdateQueueRequest("queue2", 200, 2048, map[v1.ResourceName]float64{"nvidia.com/gpu": 1})
	UpdateQueueDeserved("queue3", 300, 3096, map[v1.ResourceName]float64{"nvidia.com/gpu": 2})
	UpdateQueueCapacity("queue1", 200., 2048., map[v1.ResourceName]float64{v1.ResourceName("nvidia.com/gpu"): 10.})
	UpdateQueueRealCapacity("queue2", 200., 2048., map[v1.ResourceName]float64{v1.ResourceName("nvidia.com/gpu"): 10.})
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(":8081", nil)
		if err != nil {
			t.Errorf("ListenAndServe() err = %v", err.Error())
		}
	}()
	err := retryOnConnectionRefused(func() error {
		return forMetricMap(func(m map[string]float64) {
			assert.Equal(t, 100., m["volcano_queue_allocated_milli_cpu{queue_name=\"queue1\"}"])
			assert.Equal(t, 1024., m["volcano_queue_allocated_memory_bytes{queue_name=\"queue1\"}"])
			assert.Contains(t, m, "volcano_queue_allocated_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_allocated_scalar_resources for queue1 and resource nvidia.com/gpu should be present")
			assert.Equal(t, 0., m["volcano_queue_allocated_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}"])
			assert.Equal(t, 200., m["volcano_queue_request_milli_cpu{queue_name=\"queue2\"}"])
			assert.Equal(t, 2048., m["volcano_queue_request_memory_bytes{queue_name=\"queue2\"}"])
			assert.Contains(t, m, "volcano_queue_request_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_request_scalar_resources for queue2 and resource nvidia.com/gpu should be present")
			assert.Equal(t, 1., m["volcano_queue_request_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}"])
			assert.Equal(t, 300., m["volcano_queue_deserved_milli_cpu{queue_name=\"queue3\"}"])
			assert.Equal(t, 3096., m["volcano_queue_deserved_memory_bytes{queue_name=\"queue3\"}"])
			assert.Contains(t, m, "volcano_queue_deserved_scalar_resources{queue_name=\"queue3\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_deserved_scalar_resources for queue3 and resource nvidia.com/gpu should be present")
			assert.Equal(t, 2., m["volcano_queue_deserved_scalar_resources{queue_name=\"queue3\",resource=\"nvidia.com/gpu\"}"])
			assert.Equal(t, 200., m["volcano_queue_capacity_milli_cpu{queue_name=\"queue1\"}"])
			assert.Equal(t, 2048., m["volcano_queue_capacity_memory_bytes{queue_name=\"queue1\"}"])
			assert.Contains(t, m, "volcano_queue_capacity_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_capacity_scalar_resources for queue1 and resource nvidia.com/gpu should be present")
			assert.Equal(t, 10., m["volcano_queue_capacity_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}"])
			assert.Equal(t, 200., m["volcano_queue_real_capacity_milli_cpu{queue_name=\"queue2\"}"])
			assert.Equal(t, 2048., m["volcano_queue_real_capacity_memory_bytes{queue_name=\"queue2\"}"])
			assert.Contains(t, m, "volcano_queue_real_capacity_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_real_capacity_scalar_resources for queue1 and resource nvidia.com/gpu should be present")
			assert.Equal(t, 10., m["volcano_queue_real_capacity_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}"])
		})
	})
	if err != nil {
		t.Fatalf("getLocalMetric() err = %v", err.Error())
	}
	DeleteQueueMetrics("queue1")
	DeleteQueueMetrics("queue2")
	err = retryOnConnectionRefused(func() error {
		return forMetricMap(func(m map[string]float64) {
			assert.NotContains(t, m, "volcano_queue_allocated_milli_cpu{queue_name=\"queue1\"}",
				"volcano_queue_allocated_milli_cpu for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_allocated_memory_bytes{queue_name=\"queue1\"}",
				"volcano_queue_allocated_memory_bytes for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_allocated_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_allocated_scalar_resources for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_capacity_milli_cpu{queue_name=\"queue1\"}",
				"volcano_queue_capacity_milli_cpu for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_capacity_memory_bytes{queue_name=\"queue1\"}",
				"volcano_queue_capacity_memory_bytes for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_capacity_scalar_resources{queue_name=\"queue1\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_capacity_scalar_resources for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_real_capacity_milli_cpu{queue_name=\"queue2\"}",
				"volcano_queue_real_capacity_milli_cpu for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_real_capacity_memory_bytes{queue_name=\"queue2\"}",
				"volcano_queue_real_capacity_memory_bytes for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_real_capacity_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_real_capacity_scalar_resources for queue1 should be removed")
			assert.NotContains(t, m, "volcano_queue_request_milli_cpu{queue_name=\"queue2\"}",
				"volcano_queue_request_milli_cpu for queue2 should be removed")
			assert.NotContains(t, m, "volcano_queue_request_memory_bytes{queue_name=\"queue2\"}",
				"volcano_queue_request_memory_bytes for queue2 should be removed")
			assert.NotContains(t, m, "volcano_queue_request_scalar_resources{queue_name=\"queue2\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_request_scalar_resources for queue2 should be removed")
			assert.Contains(t, m, "volcano_queue_deserved_milli_cpu{queue_name=\"queue3\"}",
				"volcano_queue_deserved_milli_cpu for queue3 should not be removed")
			assert.Equal(t, 300., m["volcano_queue_deserved_milli_cpu{queue_name=\"queue3\"}"])
			assert.Contains(t, m, "volcano_queue_deserved_memory_bytes{queue_name=\"queue3\"}",
				"volcano_queue_deserved_memory_bytes for queue3 should not be removed")
			assert.Equal(t, 3096., m["volcano_queue_deserved_memory_bytes{queue_name=\"queue3\"}"])
			assert.Contains(t, m, "volcano_queue_deserved_scalar_resources{queue_name=\"queue3\",resource=\"nvidia.com/gpu\"}",
				"volcano_queue_deserved_scalar_resources for queue3 & nvidia.com/gpu should not be removed")
			assert.Equal(t, 2., m["volcano_queue_deserved_scalar_resources{queue_name=\"queue3\",resource=\"nvidia.com/gpu\"}"])

		})
	})
	if err != nil {
		t.Fatalf("getLocalMetric() err = %v", err.Error())
	}
}
