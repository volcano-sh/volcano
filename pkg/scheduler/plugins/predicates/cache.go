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

package predicates

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

type predicateCache struct {
	sync.RWMutex
	cache map[string]map[string]bool //key_1: nodename key_2:pod uid
}

// predicateCacheNew return cache map
func predicateCacheNew() *predicateCache {
	return &predicateCache{
		cache: make(map[string]map[string]bool),
	}
}

// getPodTemplateUID return pod template key
func getPodTemplateUID(pod *v1.Pod) string {
	uid, found := pod.Annotations[batch.PodTemplateKey]
	if !found {
		return ""
	}

	return uid
}

// PredicateWithCache: check the predicate result existed in cache
func (pc *predicateCache) PredicateWithCache(nodeName string, pod *v1.Pod) (bool, error) {
	podTemplateUID := getPodTemplateUID(pod)
	if podTemplateUID == "" {
		return false, fmt.Errorf("no anonation of volcano.sh/template-uid in pod %s", pod.Name)
	}

	pc.RLock()
	defer pc.RUnlock()
	if nodeCache, exist := pc.cache[nodeName]; exist {
		if result, exist := nodeCache[podTemplateUID]; exist {
			klog.V(4).Infof("Predicate node %s and pod %s result %v", nodeName, pod.Name, result)
			return result, nil
		}
	}

	return false, fmt.Errorf("no information of node %s and pod %s in predicate cache", nodeName, pod.Name)
}

// UpdateCache update cache data
func (pc *predicateCache) UpdateCache(nodeName string, pod *v1.Pod, fit bool) {
	podTemplateUID := getPodTemplateUID(pod)
	if podTemplateUID == "" {
		klog.V(3).Infof("Don't find pod %s template uid", pod.Name)
		return
	}

	pc.Lock()
	defer pc.Unlock()

	if _, exist := pc.cache[nodeName]; !exist {
		podCache := make(map[string]bool)
		podCache[podTemplateUID] = fit
		pc.cache[nodeName] = podCache
	} else {
		pc.cache[nodeName][podTemplateUID] = fit
	}
}
