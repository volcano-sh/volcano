/*
Copyright 2017 The Kubernetes Authors.

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
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/controller/volume/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
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
	AddBindTask(task *api.TaskInfo) error

	// BindPodGroup Pod/PodGroup to cluster
	BindPodGroup(job *api.JobInfo, cluster string) error

	// Evict evicts the task to release resources.
	Evict(task *api.TaskInfo, reason string) error

	// RecordJobStatusEvent records related events according to job status.
	// Deprecated: remove it after removed PDB support.
	RecordJobStatusEvent(job *api.JobInfo)

	// UpdateJobStatus puts job in backlog for a while.
	UpdateJobStatus(job *api.JobInfo, updatePG bool) (*api.JobInfo, error)

	GetPodVolumes(task *api.TaskInfo, node *v1.Node) (*scheduling.PodVolumes, error)

	// AllocateVolumes allocates volume on the host to the task
	AllocateVolumes(task *api.TaskInfo, hostname string, podVolumes *scheduling.PodVolumes) error

	// BindVolumes binds volumes to the task
	BindVolumes(task *api.TaskInfo, volumes *scheduling.PodVolumes) error

	// Client returns the kubernetes clientSet, which can be used by plugins
	Client() kubernetes.Interface

	UpdateSchedulerNumaInfo(sets map[string]api.ResNumaSets) error

	// SharedInformerFactory return scheduler SharedInformerFactory
	SharedInformerFactory() informers.SharedInformerFactory
}

// VolumeBinder interface for allocate and bind volumes
type VolumeBinder interface {
	GetPodVolumes(task *api.TaskInfo, node *v1.Node) (*scheduling.PodVolumes, error)
	AllocateVolumes(task *api.TaskInfo, hostname string, podVolumes *scheduling.PodVolumes) error
	BindVolumes(task *api.TaskInfo, podVolumes *scheduling.PodVolumes) error
}

//Binder interface for binding task and hostname
type Binder interface {
	Bind(kubeClient *kubernetes.Clientset, tasks []*api.TaskInfo) (error, []*api.TaskInfo)
}

// Evictor interface for evict pods
type Evictor interface {
	Evict(pod *v1.Pod, reason string) error
}

// StatusUpdater updates pod with given PodCondition
type StatusUpdater interface {
	UpdatePodCondition(pod *v1.Pod, podCondition *v1.PodCondition) (*v1.Pod, error)
	UpdatePodGroup(pg *api.PodGroup) (*api.PodGroup, error)
}

// BatchBinder updates podgroup or job information
type BatchBinder interface {
	Bind(job *api.JobInfo, cluster string) (*api.JobInfo, error)
}
