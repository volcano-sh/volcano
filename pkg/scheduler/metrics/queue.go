/*
Copyright 2020 The Volcano Authors.

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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
	v1 "k8s.io/api/core/v1"
)

var (
	queueAllocatedMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_allocated_milli_cpu",
			Help:      "Allocated CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueAllocatedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_allocated_memory_bytes",
			Help:      "Allocated memory for one queue",
		}, []string{"queue_name"},
	)

	queueAllocatedScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_allocated_scalar_resources",
			Help:      "Allocated scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueRequestMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_request_milli_cpu",
			Help:      "Request CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueRequestMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_request_memory_bytes",
			Help:      "Request memory for one queue",
		}, []string{"queue_name"},
	)

	queueRequestScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_request_scalar_resources",
			Help:      "Request scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueDeservedMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_deserved_milli_cpu",
			Help:      "Deserved CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueDeservedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_deserved_memory_bytes",
			Help:      "Deserved memory for one queue",
		}, []string{"queue_name"},
	)

	queueDeservedScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_deserved_scalar_resources",
			Help:      "Deserved scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_share",
			Help:      "Share for one queue",
		}, []string{"queue_name"},
	)

	queueWeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_weight",
			Help:      "Weight for one queue",
		}, []string{"queue_name"},
	)

	queueOverused = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_overused",
			Help:      "If one queue is overused",
		}, []string{"queue_name"},
	)

	queueCapacityMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueCapacityMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
	)

	queueCapacityScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueRealCapacityMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_real_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueRealCapacityMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_real_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
	)

	queueRealCapacityScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "queue_real_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)
)

// UpdateQueueAllocated records allocated resources for one queue
func UpdateQueueAllocated(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueAllocatedMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueAllocatedMemory.WithLabelValues(queueName).Set(memory)
	for resource, value := range scalarResources {
		queueAllocatedScalarResource.WithLabelValues(queueName, string(resource)).Set(value)
	}
}

// UpdateQueueRequest records request resources for one queue
func UpdateQueueRequest(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRequestMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueRequestMemory.WithLabelValues(queueName).Set(memory)
	for resource, value := range scalarResources {
		queueRequestScalarResource.WithLabelValues(queueName, string(resource)).Set(value)
	}
}

// UpdateQueueDeserved records deserved resources for one queue
func UpdateQueueDeserved(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueDeservedMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueDeservedMemory.WithLabelValues(queueName).Set(memory)
	for resource, value := range scalarResources {
		queueDeservedScalarResource.WithLabelValues(queueName, string(resource)).Set(value)
	}
}

// UpdateQueueShare records share for one queue
func UpdateQueueShare(queueName string, share float64) {
	queueShare.WithLabelValues(queueName).Set(share)
}

// UpdateQueueWeight records weight for one queue
func UpdateQueueWeight(queueName string, weight int32) {
	queueWeight.WithLabelValues(queueName).Set(float64(weight))
}

// UpdateQueueOverused records if one queue is overused
func UpdateQueueOverused(queueName string, overused bool) {
	var value float64
	if overused {
		value = 1
	} else {
		value = 0
	}
	queueOverused.WithLabelValues(queueName).Set(value)
}

func UpdateQueueCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueCapacityMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueCapacityMemory.WithLabelValues(queueName).Set(memory)
	for resource, value := range scalarResources {
		queueCapacityScalarResource.WithLabelValues(queueName, string(resource)).Set(value)
	}
}

func UpdateQueueRealCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRealCapacityMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueRealCapacityMemory.WithLabelValues(queueName).Set(memory)
	for resource, value := range scalarResources {
		queueRealCapacityScalarResource.WithLabelValues(queueName, string(resource)).Set(value)
	}
}

// DeleteQueueMetrics delete all metrics related to the queue
func DeleteQueueMetrics(queueName string) {
	queueAllocatedMilliCPU.DeleteLabelValues(queueName)
	queueAllocatedMemory.DeleteLabelValues(queueName)
	queueRequestMilliCPU.DeleteLabelValues(queueName)
	queueRequestMemory.DeleteLabelValues(queueName)
	queueDeservedMilliCPU.DeleteLabelValues(queueName)
	queueDeservedMemory.DeleteLabelValues(queueName)
	queueShare.DeleteLabelValues(queueName)
	queueWeight.DeleteLabelValues(queueName)
	queueOverused.DeleteLabelValues(queueName)
	queueCapacityMilliCPU.DeleteLabelValues(queueName)
	queueCapacityMemory.DeleteLabelValues(queueName)
	queueRealCapacityMilliCPU.DeleteLabelValues(queueName)
	queueRealCapacityMemory.DeleteLabelValues(queueName)
	partialLabelMap := map[string]string{"queue_name": queueName}
	queueAllocatedScalarResource.DeletePartialMatch(partialLabelMap)
	queueRequestScalarResource.DeletePartialMatch(partialLabelMap)
	queueDeservedScalarResource.DeletePartialMatch(partialLabelMap)
	queueCapacityScalarResource.DeletePartialMatch(partialLabelMap)
	queueRealCapacityScalarResource.DeletePartialMatch(partialLabelMap)
}
