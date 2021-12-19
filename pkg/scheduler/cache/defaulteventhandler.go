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

import (
	"io"

	"k8s.io/client-go/rest"

	"volcano.sh/volcano/cmd/scheduler/app/options"
)

// defaultEventHandler is the default eventHandler implementation.
// If no external eventHandler is specified, the default eventHandler is used.
type defaultEventHandler struct {
	cache *SchedulerCache
}

var _ Interface = &defaultEventHandler{}

func init() {
	RegisterEventHandler(options.DefaultEventHandlerName,
		func(cache *SchedulerCache, config io.Reader, restConfig *rest.Config) (i Interface, err error) {
			return &defaultEventHandler{cache: cache}, nil
		})
}

// AddNode adds overall information about node.
func (eh *defaultEventHandler) AddNode(obj interface{}) {
	eh.cache.AddNode(obj)
}

// UpdateNode updates overall information about node.
func (eh *defaultEventHandler) UpdateNode(oldObj, newObj interface{}) {
	eh.cache.UpdateNode(oldObj, newObj)
}

// DeleteNode removes overall information about node.
func (eh *defaultEventHandler) DeleteNode(obj interface{}) {
	eh.cache.DeleteNode(obj)
}

// AddPod adds overall information about pod.
func (eh *defaultEventHandler) AddPod(obj interface{}) {
	eh.cache.AddPod(obj)
}

// UpdatePod updates overall information about pod.
func (eh *defaultEventHandler) UpdatePod(oldObj, newObj interface{}) {
	eh.cache.UpdatePod(oldObj, newObj)
}

// DeletePod removes overall information about pod.
func (eh *defaultEventHandler) DeletePod(obj interface{}) {
	eh.cache.DeletePod(obj)
}

// AddPriorityClass adds overall information about priorityClass.
func (eh *defaultEventHandler) AddPriorityClass(obj interface{}) {
	eh.cache.AddPriorityClass(obj)
}

// UpdatePriorityClass updates overall information about priorityClass.
func (eh *defaultEventHandler) UpdatePriorityClass(oldObj, newObj interface{}) {
	eh.cache.UpdatePriorityClass(oldObj, newObj)
}

// DeletePriorityClass removes overall information about priorityClass.
func (eh *defaultEventHandler) DeletePriorityClass(obj interface{}) {
	eh.cache.DeletePriorityClass(obj)
}

// AddResourceQuota adds overall information about resourceQuota.
func (eh *defaultEventHandler) AddResourceQuota(obj interface{}) {
	eh.cache.AddResourceQuota(obj)
}

// UpdateResourceQuota updates overall information about resourceQuota.
func (eh *defaultEventHandler) UpdateResourceQuota(oldObj, newObj interface{}) {
	eh.cache.UpdateResourceQuota(oldObj, newObj)
}

// DeleteResourceQuota removes overall information about resourceQuota.
func (eh *defaultEventHandler) DeleteResourceQuota(obj interface{}) {
	eh.cache.DeleteResourceQuota(obj)
}

// AddPodGroup adds overall information about podGroup.
func (eh *defaultEventHandler) AddPodGroup(obj interface{}) {
	eh.cache.AddPodGroupV1beta1(obj)
}

// UpdatePodGroup updates overall information about podGroup.
func (eh *defaultEventHandler) UpdatePodGroup(oldObj, newObj interface{}) {
	eh.cache.UpdatePodGroupV1beta1(oldObj, newObj)
}

// DeletePodGroup removes overall information about podGroup.
func (eh *defaultEventHandler) DeletePodGroup(obj interface{}) {
	eh.cache.DeletePodGroupV1beta1(obj)
}

// AddQueue adds overall information about queue.
func (eh *defaultEventHandler) AddQueue(obj interface{}) {
	eh.cache.AddQueueV1beta1(obj)
}

// UpdateQueue updates overall information about queue.
func (eh *defaultEventHandler) UpdateQueue(oldObj, newObj interface{}) {
	eh.cache.UpdateQueueV1beta1(oldObj, newObj)
}

// DeleteQueue removes overall information about queue.
func (eh *defaultEventHandler) DeleteQueue(obj interface{}) {
	eh.cache.DeleteQueueV1beta1(obj)
}

// AddNUMATopology adds overall information about numaTopology.
func (eh *defaultEventHandler) AddNUMATopology(obj interface{}) {
	eh.cache.AddNumaInfoV1alpha1(obj)
}

// UpdateNUMATopology updates overall information about numaTopology.
func (eh *defaultEventHandler) UpdateNUMATopology(oldObj, newObj interface{}) {
	eh.cache.UpdateNumaInfoV1alpha1(oldObj, newObj)
}

// DeleteNUMATopology removes overall information about numaTopology.
func (eh *defaultEventHandler) DeleteNUMATopology(obj interface{}) {
	eh.cache.DeleteNumaInfoV1alpha1(obj)
}
