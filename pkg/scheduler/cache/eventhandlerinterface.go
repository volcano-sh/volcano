/*
Copyright 2021 The Volcano Authors.
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

// Interface is an abstract, pluggable interface for event handlers,
// that contains all involved object event processing interfaces.
// It is a combination of k8s.io/client-go/tools/cache.ResourceEventHandler
// for different objects.
type Interface interface {
	nodeResourceEventHandler
	podResourceEventHandler
	priorityClassResourceEventHandler
	resourceQuotaResourceEventHandler
	podGroupResourceEventHandler
	queueResourceEventHandler
	numaTopologyResourceEventHandler
}

// nodeResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the node resource.
type nodeResourceEventHandler interface {
	AddNode(obj interface{})
	UpdateNode(oldObj, newObj interface{})
	DeleteNode(obj interface{})
}

// podResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the pod resource.
type podResourceEventHandler interface {
	AddPod(obj interface{})
	UpdatePod(oldObj, newObj interface{})
	DeletePod(obj interface{})
}

// priorityClassResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the priority resource.
type priorityClassResourceEventHandler interface {
	AddPriorityClass(obj interface{})
	UpdatePriorityClass(oldObj, newObj interface{})
	DeletePriorityClass(obj interface{})
}

// resourceQuotaResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the resourceQuota resource.
type resourceQuotaResourceEventHandler interface {
	AddResourceQuota(obj interface{})
	UpdateResourceQuota(oldObj, newObj interface{})
	DeleteResourceQuota(obj interface{})
}

// podGroupResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the podGroup resource.
type podGroupResourceEventHandler interface {
	AddPodGroup(obj interface{})
	UpdatePodGroup(oldObj, newObj interface{})
	DeletePodGroup(obj interface{})
}

// queueResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the queue resource.
type queueResourceEventHandler interface {
	AddQueue(obj interface{})
	UpdateQueue(oldObj, newObj interface{})
	DeleteQueue(obj interface{})
}

// numaTopologyResourceEventHandler is a subset of k8s.io/client-go/tools/cache.ResourceEventHandler
// for the numaTopology resource.
type numaTopologyResourceEventHandler interface {
	AddNUMATopology(obj interface{})
	UpdateNUMATopology(oldObj, newObj interface{})
	DeleteNUMATopology(obj interface{})
}
