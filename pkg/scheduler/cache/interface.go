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
	"k8s.io/api/core/v1"

	arbcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

// Cache collects pods/nodes/queues information
// and provides information snapshot
type Cache interface {
	// Run start informer
	Run(stopCh <-chan struct{})

	// Snapshot deep copy overall cache information into snapshot
	Snapshot() *api.ClusterInfo

	// SchedulerConf return the property of scheduler configuration
	LoadSchedulerConf(path string) (map[string]string, error)

	// WaitForCacheSync waits for all cache synced
	WaitForCacheSync(stopCh <-chan struct{}) bool

	// Bind binds Task to the target host.
	// TODO(jinzhej): clean up expire Tasks.
	Bind(task *api.TaskInfo, hostname string) error

	// Evict evicts the task to release resources.
	Evict(task *api.TaskInfo, reason string) error

	// Backoff puts job in backlog for a while.
	Backoff(job *api.JobInfo, event arbcorev1.Event, reason string) error
}

type Binder interface {
	Bind(task *v1.Pod, hostname string) error
}

type Evictor interface {
	Evict(pod *v1.Pod) error
}
