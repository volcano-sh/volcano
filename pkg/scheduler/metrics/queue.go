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
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/util"
)

var (
	queueAllocatedMilliCPU          *util.TTLCollectorWrapper
	queueAllocatedMemory            *util.TTLCollectorWrapper
	queueAllocatedScalarResource    *util.TTLCollectorWrapper
	queueRequestMilliCPU            *util.TTLCollectorWrapper
	queueRequestMemory              *util.TTLCollectorWrapper
	queueRequestScalarResource      *util.TTLCollectorWrapper
	queueDeservedMilliCPU           *util.TTLCollectorWrapper
	queueDeservedMemory             *util.TTLCollectorWrapper
	queueDeservedScalarResource     *util.TTLCollectorWrapper
	queueShare                      *util.TTLCollectorWrapper
	queueWeight                     *util.TTLCollectorWrapper
	queueOverused                   *util.TTLCollectorWrapper
	queueCapacityMilliCPU           *util.TTLCollectorWrapper
	queueCapacityMemory             *util.TTLCollectorWrapper
	queueCapacityScalarResource     *util.TTLCollectorWrapper
	queueRealCapacityMilliCPU       *util.TTLCollectorWrapper
	queueRealCapacityMemory         *util.TTLCollectorWrapper
	queueRealCapacityScalarResource *util.TTLCollectorWrapper

	// Track all known scalar resources for each queue
	knownScalarResources     = make(map[string]map[string]struct{})
	knownScalarResourcesLock sync.RWMutex
)

func InitTTLQueueMetrics(ctx context.Context) {
	expirationTime := time.Hour * 2
	checkIntervalTime := time.Hour

	queueAllocatedMilliCPU = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_milli_cpu",
			Help:      "Allocated CPU count for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueAllocatedMemory = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_memory_bytes",
			Help:      "Allocated memory for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueAllocatedScalarResource = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_scalar_resources",
			Help:      "Allocated scalar resources for one queue",
		}, []string{"queue_name", "resource"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRequestMilliCPU = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_milli_cpu",
			Help:      "Request CPU count for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRequestMemory = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_memory_bytes",
			Help:      "Request memory for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRequestScalarResource = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_scalar_resources",
			Help:      "Request scalar resources for one queue",
		}, []string{"queue_name", "resource"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueDeservedMilliCPU = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_milli_cpu",
			Help:      "Deserved CPU count for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueDeservedMemory = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_memory_bytes",
			Help:      "Deserved memory for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueDeservedScalarResource = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_scalar_resources",
			Help:      "Deserved scalar resources for one queue",
		}, []string{"queue_name", "resource"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueShare = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_share",
			Help:      "Share for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueWeight = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_weight",
			Help:      "Weight for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueOverused = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_overused",
			Help:      "If one queue is overused",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueCapacityMilliCPU = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueCapacityMemory = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueCapacityScalarResource = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRealCapacityMilliCPU = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRealCapacityMemory = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)

	queueRealCapacityScalarResource = util.NewTTLGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
		expirationTime,
		checkIntervalTime,
		ctx,
	)
}

// helper to update knownScalarResources and delete metrics for removed resources
func updateScalarResourceMetrics(metric *util.TTLCollectorWrapper, queueName string, scalarResources map[v1.ResourceName]float64) {
	knownScalarResourcesLock.Lock()
	defer knownScalarResourcesLock.Unlock()
	if knownScalarResources[queueName] == nil {
		knownScalarResources[queueName] = make(map[string]struct{})
	}
	current := make(map[string]struct{})
	for resource := range scalarResources {
		name := string(resource)
		current[name] = struct{}{}
		knownScalarResources[queueName][name] = struct{}{}
		metric.ObserveWithLabelValues(scalarResources[resource], queueName, name)
	}
	// For all known resources, that are not present in the current update set the value to zero
	for name := range knownScalarResources[queueName] {
		if _, ok := current[name]; !ok {
			metric.ObserveWithLabelValues(0, queueName, name)
		}
	}
}

// UpdateQueueAllocated records allocated resources for one queue
func UpdateQueueAllocated(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueAllocatedMilliCPU.ObserveWithLabelValues(milliCPU, queueName)
	queueAllocatedMemory.ObserveWithLabelValues(float64(memory), queueName)
	updateScalarResourceMetrics(queueAllocatedScalarResource, queueName, scalarResources)
}

// UpdateQueueRequest records request resources for one queue
func UpdateQueueRequest(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRequestMilliCPU.ObserveWithLabelValues(milliCPU, queueName)
	queueRequestMemory.ObserveWithLabelValues(memory, queueName)
	updateScalarResourceMetrics(queueRequestScalarResource, queueName, scalarResources)
}

// UpdateQueueDeserved records deserved resources for one queue
func UpdateQueueDeserved(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueDeservedMilliCPU.ObserveWithLabelValues(milliCPU, queueName)
	queueDeservedMemory.ObserveWithLabelValues(memory, queueName)
	updateScalarResourceMetrics(queueDeservedScalarResource, queueName, scalarResources)
}

// UpdateQueueShare records share for one queue
func UpdateQueueShare(queueName string, share float64) {
	queueShare.ObserveWithLabelValues(share, queueName)
}

// UpdateQueueWeight records weight for one queue
func UpdateQueueWeight(queueName string, weight int32) {
	queueWeight.ObserveWithLabelValues(float64(weight), queueName)
}

// UpdateQueueOverused records if one queue is overused
func UpdateQueueOverused(queueName string, overused bool) {
	var value float64
	if overused {
		value = 1
	} else {
		value = 0
	}
	queueOverused.ObserveWithLabelValues(value, queueName)
}

// UpdateQueueCapacity records capacity resources for one queue
func UpdateQueueCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueCapacityMilliCPU.ObserveWithLabelValues(milliCPU, queueName)
	queueCapacityMemory.ObserveWithLabelValues(memory, queueName)
	updateScalarResourceMetrics(queueCapacityScalarResource, queueName, scalarResources)
}

// UpdateQueueRealCapacity records real capacity resources for one queue
func UpdateQueueRealCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRealCapacityMilliCPU.ObserveWithLabelValues(milliCPU, queueName)
	queueRealCapacityMemory.ObserveWithLabelValues(memory, queueName)
	updateScalarResourceMetrics(queueRealCapacityScalarResource, queueName, scalarResources)
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
	knownScalarResourcesLock.Lock()
	delete(knownScalarResources, queueName)
	knownScalarResourcesLock.Unlock()
}
