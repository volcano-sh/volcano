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

package schedulercache

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	clientv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

// New returns a Cache implementation.
// It automatically starts a go routine that manages expiration of assumed pods.
// "ttl" is how long the assumed pod will get expired.
// "stop" is the channel that would close the background goroutine.
func New(podInformer clientv1.PodInformer,
	nodeInformer clientv1.NodeInformer,
) Cache {

	schedulerCache := newSchedulerCache()

	podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					fmt.Printf("====== pod filter, name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    schedulerCache.AddPod,
				UpdateFunc: schedulerCache.UpdatePod,
				DeleteFunc: schedulerCache.DeletePod,
			},
		},
	)

	return schedulerCache
}

type schedulerCache struct {
	sync.Mutex

	// a map from pod key to podState.
	pods                    map[string]*PodInfo
	nodes                   map[string]*NodeInfo
	resourceQuotaAllocators map[string]*ResourceQuotaAllocatorInfo
}

func newSchedulerCache() *schedulerCache {
	return &schedulerCache{
		nodes: make(map[string]*NodeInfo),
		pods:  make(map[string]*PodInfo),
		resourceQuotaAllocators: make(map[string]*ResourceQuotaAllocatorInfo),
	}
}

// Assumes that lock is already acquired.
func (cache *schedulerCache) addPod(pod *v1.Pod) error {
	key, err := getPodKey(pod)
	if err != nil {
		return err
	}

	if _, ok := cache.pods[key]; ok {
		return fmt.Errorf("pod %v exist", key)
	}

	info := &PodInfo{
		name: key,
		pod:  pod.DeepCopy(),
	}
	cache.pods[key] = info
	return nil
}

// Assumes that lock is already acquired.
func (cache *schedulerCache) updatePod(oldPod, newPod *v1.Pod) error {
	if err := cache.removePod(oldPod); err != nil {
		return err
	}
	cache.addPod(newPod)
	return nil
}

// Assumes that lock is already acquired.
func (cache *schedulerCache) removePod(pod *v1.Pod) error {
	key, err := getPodKey(pod)
	if err != nil {
		return err
	}

	if _, ok := cache.pods[key]; !ok {
		return fmt.Errorf("pod %v doesn't exist", key)
	}
	delete(cache.pods, key)
	return nil
}

func (cache *schedulerCache) AddPod(obj interface{}) {
	pod := obj.(*v1.Pod)

	key, err := getPodKey(pod)
	if err != nil {
		return
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	err = cache.addPod(pod)
	if err != nil {
		glog.Errorf("Failed to add pod %s into cache: %v", key, err)
		return
	}
}

func (cache *schedulerCache) UpdatePod(oldPod, newPod *v1.Pod) error {
	key, err := getPodKey(oldPod)
	if err != nil {
		return err
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	err = cache.updatePod(oldPod, newPod)
	if err != nil {
		return fmt.Errorf("update pod failed. Pod key: %v", key)
	}
	return nil
}

func (cache *schedulerCache) RemovePod(pod *v1.Pod) error {
	key, err := getPodKey(pod)
	if err != nil {
		return err
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	err = cache.removePod(pod)
	if err != nil {
		return fmt.Errorf("remove pod failed. Pod key: %v", key)
	}
	return nil
}

func (cache *schedulerCache) AddNode(node *v1.Node) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	n, ok := cache.nodes[node.Name]
	if !ok {
		n = &NodeInfo{
			name: node.Name,
		}
		cache.nodes[node.Name] = n
	}
	return n.SetNode(node)
}

func (cache *schedulerCache) UpdateNode(oldNode, newNode *v1.Node) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	n, ok := cache.nodes[newNode.Name]
	if !ok {
		n = &NodeInfo{
			name: newNode.Name,
		}
		cache.nodes[newNode.Name] = n
	}
	return n.SetNode(newNode)
}

func (cache *schedulerCache) RemoveNode(node *v1.Node) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	n := cache.nodes[node.Name]
	if err := n.RemoveNode(node); err != nil {
		return err
	}
	return nil
}

func (cache *schedulerCache) addResourceQuotaAllocator(rqa *apiv1.ResourceQuotaAllocator) error {
	name := rqa.Name
	if _, ok := cache.resourceQuotaAllocators[name]; ok {
		return fmt.Errorf("resourceQuotaAllocator %v exist", name)
	}

	info := &ResourceQuotaAllocatorInfo{
		name:      name,
		allocator: rqa.DeepCopy(),
	}
	cache.resourceQuotaAllocators[name] = info
	return nil
}

func (cache *schedulerCache) updateResourceQuotaAllocator(oldRqa, newRqa *apiv1.ResourceQuotaAllocator) error {
	if err := cache.removeResourceQuotaAllocator(oldRqa); err != nil {
		return err
	}
	cache.addResourceQuotaAllocator(newRqa)
	return nil
}

func (cache *schedulerCache) removeResourceQuotaAllocator(rqa *apiv1.ResourceQuotaAllocator) error {
	name := rqa.Name
	if _, ok := cache.resourceQuotaAllocators[name]; !ok {
		return fmt.Errorf("resourceQuotaAllocator %v doesn't exist", name)
	}
	delete(cache.resourceQuotaAllocators, name)
	return nil
}

func (cache *schedulerCache) AddResourceQuotaAllocator(rqa *apiv1.ResourceQuotaAllocator) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return cache.addResourceQuotaAllocator(rqa)
}

func (cache *schedulerCache) UpdateResourceQuotaAllocator(oldRqa, newRqa *apiv1.ResourceQuotaAllocator) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.updateResourceQuotaAllocator(oldRqa, newRqa)
	return nil
}

func (cache *schedulerCache) RemoveResourceQuotaAllocator(rqa *apiv1.ResourceQuotaAllocator) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return cache.removeResourceQuotaAllocator(rqa)
}

func (cache *schedulerCache) Dump() *CacheSnapshot {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	snapshot := &CacheSnapshot{
		Nodes:      make(map[string]*NodeInfo),
		Pods:       make(map[string]*PodInfo),
		Allocators: make(map[string]*ResourceQuotaAllocatorInfo),
	}

	for key, value := range cache.nodes {
		snapshot.Nodes[key] = value.Clone()
	}
	for key, value := range cache.pods {
		snapshot.Pods[key] = value.Clone()
	}
	for key, value := range cache.resourceQuotaAllocators {
		snapshot.Allocators[key] = value.Clone()
	}
	return snapshot
}
