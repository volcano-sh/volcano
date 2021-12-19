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

// fakeEventHandler implements cache.Interface.
type fakeEventHandler struct {
	cache *SchedulerCache
}

var _ Interface = &fakeEventHandler{}

// newFakeEventHandler returns a fake event handler implementation.
// It shouldn't be considered a replacement for a real event handler
// and is mostly useful in simple unit tests.
func newFakeEventHandler(cache *SchedulerCache) Interface {
	return &fakeEventHandler{
		cache: cache,
	}
}

func (feh *fakeEventHandler) AddNode(obj interface{}) {
}

func (feh *fakeEventHandler) UpdateNode(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeleteNode(obj interface{}) {
}

func (feh *fakeEventHandler) AddPod(obj interface{}) {
}

func (feh *fakeEventHandler) UpdatePod(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeletePod(obj interface{}) {
}

func (feh *fakeEventHandler) AddPriorityClass(obj interface{}) {
}

func (feh *fakeEventHandler) UpdatePriorityClass(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeletePriorityClass(obj interface{}) {
}

func (feh *fakeEventHandler) AddResourceQuota(obj interface{}) {
}

func (feh *fakeEventHandler) UpdateResourceQuota(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeleteResourceQuota(obj interface{}) {
}

func (feh *fakeEventHandler) AddPodGroup(obj interface{}) {
}

func (feh *fakeEventHandler) UpdatePodGroup(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeletePodGroup(obj interface{}) {
}

func (feh *fakeEventHandler) AddQueue(obj interface{}) {
}

func (feh *fakeEventHandler) UpdateQueue(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeleteQueue(obj interface{}) {
}

func (feh *fakeEventHandler) AddNUMATopology(obj interface{}) {
}

func (feh *fakeEventHandler) UpdateNUMATopology(oldObj, newObj interface{}) {
}

func (feh *fakeEventHandler) DeleteNUMATopology(obj interface{}) {
}
