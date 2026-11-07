/*
Copyright 2017 The Kubernetes Authors.
Copyright 2017-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Extended cache interface with enhanced binding, client access and metrics support
- Added new interfaces for pre-binding, batch binding and status updating

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

package cache

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	fwk "k8s.io/kube-scheduler/framework"

	nodeshardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

// Cache collects pods/nodes/queues information
// and provides information snapshot
type Cache interface {
	// Run start informer
	Run(stopCh <-chan struct{})

	// Snapshot deep copy overall cache information into snapshot
	Snapshot() *api.ClusterInfo

	// WaitForCacheSync waits for all cache synced
	WaitForCacheSync(stopCh <-chan struct{})

	// AddBindTask binds Task to the target host.
	// TODO(jinzhej): clean up expire Tasks.
	AddBindTask(bindCtx *BindContext) error

	// BindPodGroup Pod/PodGroup to cluster
	BindPodGroup(job *api.JobInfo, cluster string) error

	// Evict evicts the task to release resources.
	Evict(task *api.TaskInfo, reason string) error

	// RecordJobStatusEvent records related events according to job status.
	// Deprecated: remove it after removed PDB support.
	RecordJobStatusEvent(job *api.JobInfo, updatePG bool)

	// UpdateJobStatus puts job in backlog for a while.
	UpdateJobStatus(job *api.JobInfo, updatePGStatus, updatePGAnnotations, updateJobInfo bool) (*api.JobInfo, error)

	// UpdateQueueStatus update queue status.
	UpdateQueueStatus(queue *api.QueueInfo) error

	// Client returns the kubernetes clientSet, which can be used by plugins
	Client() kubernetes.Interface

	// VCClient returns the volcano clientSet, which can be used by plugins
	VCClient() vcclient.Interface

	// ClientConfig returns the rest config
	ClientConfig() *rest.Config

	AddUnassignedNumaPods(allocatedSets map[api.PodMeta]map[string]api.ResNumaSets) error

	// SharedInformerFactory return scheduler SharedInformerFactory
	SharedInformerFactory() informers.SharedInformerFactory

	// SetMetricsConf set the metrics server related configuration
	SetMetricsConf(conf map[string]string)

	// GetMetricsConf returns the metrics server related configuration
	GetMetricsConf() map[string]string

	// EventRecorder returns the event recorder
	EventRecorder() record.EventRecorder

	// RegisterBinder registers the passed binder to the cache's binderRegistry
	RegisterBinder(name string, binder interface{})

	// SharedDRAManager returns the shared DRAManager
	SharedDRAManager() fwk.SharedDRAManager

	// IsJobTerminated returns if the job was terminated
	IsJobTerminated(jobId api.JobID) bool
	//UpdateNodeShardStatus update status in nodeshard
	UpdateNodeShardStatus(nodeShardName string) error

	//OnSessionOpen is called before session open
	OnSessionOpen()

	//OnSessionClose is called after session close
	OnSessionClose()
}

// DeviceTopologyReservationCache is an optional Cache extension used by the
// device-topology plugin to reserve exact selected device IDs. It is separate
// from Cache so existing cache implementations remain compatible until they
// opt into trusted topology allocation.
type DeviceTopologyReservationCache interface {
	ReserveDeviceTopologyPlan(ctx context.Context, plan api.TopologyPlacementPlan) (api.TopologyReservation, error)
	ReleaseDeviceTopologyReservation(ctx context.Context, reservation api.TopologyReservation) error
	// DeviceTopologySnapshot returns the current immutable topology view,
	// including availability changes made by reservations and releases.
	DeviceTopologySnapshot() *api.TopologySnapshot
}

// DeviceTopologyBindHandoff carries one resource-specific immutable adapter
// handoff for the exact Pod selected by a device topology reservation.
type DeviceTopologyBindHandoff struct {
	ResourceName v1.ResourceName
	Handoff      adapter.AllocationHandoff
}

// DeviceTopologyBindContextExtension carries the immutable exact-device
// handoffs that the device-topology plugin prepared for one Pod bind.
type DeviceTopologyBindContextExtension struct {
	Handoffs []DeviceTopologyBindHandoff
}

// DeviceTopologyBindHandoffCache is an optional Cache extension that prepares
// and commits exact-device handoffs after a reservation succeeds.
type DeviceTopologyBindHandoffCache interface {
	// DeviceTopologyReservationCache supplies the reservations this extension
	// hands off and the current topology view after bind state changes.
	DeviceTopologyReservationCache
	PrepareDeviceTopologyReservationBind(ctx context.Context, reservation api.TopologyReservation) (map[api.TaskID][]DeviceTopologyBindHandoff, error)
	CommitDeviceTopologyReservationBind(ctx context.Context, reservation api.TopologyReservation) error
	RollbackDeviceTopologyReservationBind(ctx context.Context, reservation api.TopologyReservation) error
}

// DeviceTopologyProviderCache is an optional Cache extension that receives
// startup-only topology provider configuration. It is intentionally separate
// from Cache so existing cache implementations do not need trusted topology
// source support until they opt in.
type DeviceTopologyProviderCache interface {
	ConfigureDeviceTopologyProviders(registry *provider.Registry, sources []provider.NodeTopologyProvider) error
}

// DeviceTopologyAdapterCache is an optional Cache extension that receives the
// startup-only allocation adapter registry for exact hard placement.
type DeviceTopologyAdapterCache interface {
	ConfigureDeviceTopologyAdapters(registry *adapter.Registry) error
}

// Binder interface for binding task and hostname
type Binder interface {
	Bind(kubeClient kubernetes.Interface, tasks []*api.TaskInfo) map[api.TaskID]string
}

// Evictor interface for evict pods
type Evictor interface {
	Evict(pod *v1.Pod, reason string) error
}

// StatusUpdater updates pod with given PodCondition
type StatusUpdater interface {
	UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error)
	UpdatePodGroup(pg *api.PodGroup) (*api.PodGroup, error)
	UpdateQueueStatus(queue *api.QueueInfo) error
	UpdateNodeShardStatus(nodeshard *nodeshardv1alpha1.NodeShard) (*nodeshardv1alpha1.NodeShard, error)
}

// BatchBinder updates podgroup or job information
type BatchBinder interface {
	Bind(job *api.JobInfo, cluster string) (*api.JobInfo, error)
}

type PreBinder interface {
	PreBind(ctx context.Context, bindCtx *BindContext) error

	// PreBindRollBack is called when the pre-bind or bind fails.
	PreBindRollBack(ctx context.Context, bindCtx *BindContext)
}

// BatchBindPreparation represents external state prepared for one complete
// bind batch. Commit makes the preparation visible to the bind path, while
// Rollback compensates all prepared state when any later step fails.
type BatchBindPreparation interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// BatchPreBinder prepares all bind contexts before SchedulerCache accepts any
// of them. This lets gang-scoped backends create immutable handoffs without
// exposing a partially prepared batch to Kubernetes binding.
type BatchPreBinder interface {
	PrepareBindBatch(ctx context.Context, bindContexts []*BindContext) (BatchBindPreparation, error)
}

// BatchBindCache is an optional Cache extension that supports an all-or-none
// pre-bind preparation phase followed by one atomic cache batch admission.
type BatchBindCache interface {
	HasBatchPreBinders() bool
	PrepareBindBatch(ctx context.Context, bindContexts []*BindContext) (BatchBindPreparation, error)
	AddBindBatch(bindContexts []*BindContext) error
}
