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
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
	v1 "k8s.io/api/core/v1"
)

// QueueNodeGroupResource contains allocated resources for one queue and nodegroup pair.
type QueueNodeGroupResource struct {
	QueueName       string
	NodeGroupName   string
	MilliCPU        float64
	Memory          float64
	ScalarResources map[v1.ResourceName]float64
}

type queueNodeGroupKey struct {
	queueName     string
	nodeGroupName string
}

type queueNodeGroupAllocation struct {
	milliCPU        float64
	memory          float64
	scalarResources map[v1.ResourceName]float64
	seen            bool
}

var (
	queueAllocatedMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_milli_cpu",
			Help:      "Allocated CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueAllocatedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_memory_bytes",
			Help:      "Allocated memory for one queue",
		}, []string{"queue_name"},
	)

	queueAllocatedScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_allocated_scalar_resources",
			Help:      "Allocated scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueNodeGroupAllocatedMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_nodegroup_allocated_milli_cpu",
			Help:      "Allocated CPU requests in millicores for one queue on one nodegroup",
		}, []string{"queue_name", "nodegroup_name"},
	)

	queueNodeGroupAllocatedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_nodegroup_allocated_memory_bytes",
			Help:      "Allocated memory requests in bytes for one queue on one nodegroup",
		}, []string{"queue_name", "nodegroup_name"},
	)

	queueNodeGroupAllocatedScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_nodegroup_allocated_scalar_resources",
			Help:      "Allocated scalar resource requests for one queue on one nodegroup",
		}, []string{"queue_name", "nodegroup_name", "resource"},
	)

	queueRequestMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_milli_cpu",
			Help:      "Request CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueRequestMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_memory_bytes",
			Help:      "Request memory for one queue",
		}, []string{"queue_name"},
	)

	queueRequestScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_request_scalar_resources",
			Help:      "Request scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueDeservedMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_milli_cpu",
			Help:      "Deserved CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueDeservedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_memory_bytes",
			Help:      "Deserved memory for one queue",
		}, []string{"queue_name"},
	)

	queueDeservedScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_deserved_scalar_resources",
			Help:      "Deserved scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_share",
			Help:      "Share for one queue",
		}, []string{"queue_name"},
	)

	queueWeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_weight",
			Help:      "Weight for one queue",
		}, []string{"queue_name"},
	)

	queueOverused = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_overused",
			Help:      "If one queue is overused",
		}, []string{"queue_name"},
	)

	queueCapacityMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueCapacityMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
	)

	queueCapacityScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueRealCapacityMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_milli_cpu",
			Help:      "Capacity CPU count for one queue",
		}, []string{"queue_name"},
	)

	queueRealCapacityMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_memory_bytes",
			Help:      "Capacity memory for one queue",
		}, []string{"queue_name"},
	)

	queueRealCapacityScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_real_capacity_scalar_resources",
			Help:      "Capacity scalar resources for one queue",
		}, []string{"queue_name", "resource"},
	)

	queueInqueueMilliCPU = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_inqueue_milli_cpu",
			Help:      "Inqueue CPU for admitted but not yet running jobs in one queue",
		}, []string{"queue_name"},
	)

	queueInqueueMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_inqueue_memory_bytes",
			Help:      "Inqueue memory for admitted but not yet running jobs in one queue",
		}, []string{"queue_name"},
	)

	queueInqueueScalarResource = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_inqueue_scalar_resources",
			Help:      "Inqueue scalar resources for admitted but not yet running jobs in one queue",
		}, []string{"queue_name", "resource"},
	)

	queueTaskCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "queue_session_start_task_count",
			Help:      "Number of tasks by status in jobs directly assigned to this queue at the beginning of the latest scheduling session",
		}, []string{"queue_name", "status"},
	)
	queueTaskStatuses = [...]string{
		"pending",
		"allocated",
		"pipelined",
		"binding",
		"bound",
		"running",
		"releasing",
		"succeeded",
		"failed",
		"unknown",
	}

	// Track all known scalar resources for each queue
	knownScalarResources     = make(map[string]map[string]struct{})
	knownScalarResourcesLock sync.RWMutex

	queueNodeGroupAllocatedMetricsLock sync.Mutex
	queueNodeGroupAllocatedResources   = make(map[queueNodeGroupKey]*queueNodeGroupAllocation)
)

// helper to update knownScalarResources and delete metrics for removed resources
func updateScalarResourceMetrics(metric *prometheus.GaugeVec, queueName string, scalarResources map[v1.ResourceName]float64) {
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
		metric.WithLabelValues(queueName, name).Set(scalarResources[resource])
	}
	// For all known resources, that are not present in the current update set the value to zero
	for name := range knownScalarResources[queueName] {
		if _, ok := current[name]; !ok {
			metric.WithLabelValues(queueName, name).Set(0)
		}
	}
}

// UpdateQueueAllocated records allocated resources for one queue
func UpdateQueueAllocated(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueAllocatedMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueAllocatedMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueAllocatedScalarResource, queueName, scalarResources)
}

// SyncQueueNodeGroupAllocated reconciles a complete scheduler session snapshot.
// Only changed values are written; missing pairs and scalar resources are deleted.
func SyncQueueNodeGroupAllocated(resources []QueueNodeGroupResource) {
	queueNodeGroupAllocatedMetricsLock.Lock()
	defer queueNodeGroupAllocatedMetricsLock.Unlock()

	for _, resource := range resources {
		updateQueueNodeGroupAllocatedLocked(resource)
	}
	for key, allocated := range queueNodeGroupAllocatedResources {
		if !allocated.seen {
			deleteQueueNodeGroupAllocatedLocked(key)
			continue
		}
		allocated.seen = false
	}
}

func updateQueueNodeGroupAllocatedLocked(resource QueueNodeGroupResource) {
	key := queueNodeGroupKey{queueName: resource.QueueName, nodeGroupName: resource.NodeGroupName}
	allocated, known := queueNodeGroupAllocatedResources[key]
	if !known {
		allocated = &queueNodeGroupAllocation{}
		queueNodeGroupAllocatedResources[key] = allocated
	}
	allocated.seen = true

	if !known || allocated.milliCPU != resource.MilliCPU {
		queueNodeGroupAllocatedMilliCPU.WithLabelValues(resource.QueueName, resource.NodeGroupName).Set(resource.MilliCPU)
		allocated.milliCPU = resource.MilliCPU
	}
	if !known || allocated.memory != resource.Memory {
		queueNodeGroupAllocatedMemory.WithLabelValues(resource.QueueName, resource.NodeGroupName).Set(resource.Memory)
		allocated.memory = resource.Memory
	}

	for name := range allocated.scalarResources {
		if _, found := resource.ScalarResources[name]; !found {
			queueNodeGroupAllocatedScalarResource.DeleteLabelValues(resource.QueueName, resource.NodeGroupName, string(name))
			delete(allocated.scalarResources, name)
		}
	}
	for name, value := range resource.ScalarResources {
		if previous, found := allocated.scalarResources[name]; !found || previous != value {
			queueNodeGroupAllocatedScalarResource.WithLabelValues(resource.QueueName, resource.NodeGroupName, string(name)).Set(value)
			if allocated.scalarResources == nil {
				allocated.scalarResources = make(map[v1.ResourceName]float64, len(resource.ScalarResources))
			}
			allocated.scalarResources[name] = value
		}
	}
}

func deleteQueueNodeGroupAllocatedLocked(key queueNodeGroupKey) {
	queueNodeGroupAllocatedMilliCPU.DeleteLabelValues(key.queueName, key.nodeGroupName)
	queueNodeGroupAllocatedMemory.DeleteLabelValues(key.queueName, key.nodeGroupName)
	for name := range queueNodeGroupAllocatedResources[key].scalarResources {
		queueNodeGroupAllocatedScalarResource.DeleteLabelValues(key.queueName, key.nodeGroupName, string(name))
	}
	delete(queueNodeGroupAllocatedResources, key)
}

// UpdateQueueRequest records request resources for one queue
func UpdateQueueRequest(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRequestMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueRequestMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueRequestScalarResource, queueName, scalarResources)
}

// UpdateQueueDeserved records deserved resources for one queue
func UpdateQueueDeserved(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueDeservedMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueDeservedMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueDeservedScalarResource, queueName, scalarResources)
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

// UpdateQueueCapacity records capacity resources for one queue
func UpdateQueueCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueCapacityMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueCapacityMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueCapacityScalarResource, queueName, scalarResources)
}

// UpdateQueueRealCapacity records real capacity resources for one queue
func UpdateQueueRealCapacity(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueRealCapacityMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueRealCapacityMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueRealCapacityScalarResource, queueName, scalarResources)
}

// UpdateQueueInqueue records resources for admitted but not yet running jobs in one queue
func UpdateQueueInqueue(queueName string, milliCPU, memory float64, scalarResources map[v1.ResourceName]float64) {
	queueInqueueMilliCPU.WithLabelValues(queueName).Set(milliCPU)
	queueInqueueMemory.WithLabelValues(queueName).Set(memory)
	updateScalarResourceMetrics(queueInqueueScalarResource, queueName, scalarResources)
}

// UpdateQueueTaskCounts records the number of tasks in each status for a queue
func UpdateQueueTaskCounts(queueName string, taskCounts map[string]int) {
	for _, status := range queueTaskStatuses {
		queueTaskCount.WithLabelValues(queueName, status).Set(float64(taskCounts[status]))
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
	queueInqueueMilliCPU.DeleteLabelValues(queueName)
	queueInqueueMemory.DeleteLabelValues(queueName)
	partialLabelMap := map[string]string{"queue_name": queueName}
	queueAllocatedScalarResource.DeletePartialMatch(partialLabelMap)
	queueRequestScalarResource.DeletePartialMatch(partialLabelMap)
	queueDeservedScalarResource.DeletePartialMatch(partialLabelMap)
	queueCapacityScalarResource.DeletePartialMatch(partialLabelMap)
	queueRealCapacityScalarResource.DeletePartialMatch(partialLabelMap)
	queueInqueueScalarResource.DeletePartialMatch(partialLabelMap)
	queueTaskCount.DeletePartialMatch(partialLabelMap)
	queueNodeGroupAllocatedMetricsLock.Lock()
	queueNodeGroupAllocatedMilliCPU.DeletePartialMatch(partialLabelMap)
	queueNodeGroupAllocatedMemory.DeletePartialMatch(partialLabelMap)
	queueNodeGroupAllocatedScalarResource.DeletePartialMatch(partialLabelMap)
	for key := range queueNodeGroupAllocatedResources {
		if key.queueName == queueName {
			delete(queueNodeGroupAllocatedResources, key)
		}
	}
	queueNodeGroupAllocatedMetricsLock.Unlock()
	knownScalarResourcesLock.Lock()
	delete(knownScalarResources, queueName)
	knownScalarResourcesLock.Unlock()
}
