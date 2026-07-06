/*
Copyright 2025 The Volcano Authors.

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
	"fmt"
	"testing"

	api "volcano.sh/volcano/pkg/scheduler/api"
)

func makeCache(size int) *SchedulerCache {
	sc := &SchedulerCache{
		Nodes:         make(map[string]*api.NodeInfo, size),
		NodeList:      make([]string, 0, size),
		nodeListIndex: make(map[string]int, size),
		imageStates:   make(map[string]*imageState),
	}
	return sc
}

func nodeName(i int) string {
	return fmt.Sprintf("node-%04d", i)
}

// BenchmarkNodeListIndexAddOrUpdate measures only the NodeList/nodeListIndex membership
// check and append path (no locking, no image states, no Nodes map writes).
func BenchmarkNodeListIndexAddOrUpdate(b *testing.B) {
	for _, n := range []int{100, 500, 2000} {
		b.Run(fmt.Sprintf("nodes=%d", n), func(b *testing.B) {
			names := make([]string, n)
			for i := range names {
				names[i] = nodeName(i)
			}
			b.ResetTimer()
			for iter := 0; iter < b.N; iter++ {
				sc := makeCache(n)
				for _, name := range names {
					sc.addOrUpdateNodeFast(name)
				}
			}
		})
	}
}

// BenchmarkNodeListIndexRemove measures only the NodeList/nodeListIndex swap-delete path
// (no locking, no NUMA cleanup). Setup is excluded from the timed region.
func BenchmarkNodeListIndexRemove(b *testing.B) {
	for _, n := range []int{100, 500, 2000} {
		b.Run(fmt.Sprintf("nodes=%d", n), func(b *testing.B) {
			b.ResetTimer()
			for iter := 0; iter < b.N; iter++ {
				b.StopTimer()
				sc := makeCache(n)
				for i := 0; i < n; i++ {
					name := nodeName(i)
					sc.nodeListIndex[name] = len(sc.NodeList)
					sc.NodeList = append(sc.NodeList, name)
				}
				b.StartTimer()

				for i := 0; i < n; i++ {
					sc.removeNodeFast(nodeName(i))
				}
			}
		})
	}
}

// addOrUpdateNodeFast exercises only the NodeList/nodeListIndex logic (no locking, no image states).
func (sc *SchedulerCache) addOrUpdateNodeFast(name string) {
	if _, exists := sc.nodeListIndex[name]; !exists {
		sc.nodeListIndex[name] = len(sc.NodeList)
		sc.NodeList = append(sc.NodeList, name)
	}
}

// removeNodeFast exercises only the NodeList/nodeListIndex logic (no locking, no NUMA cleanup).
func (sc *SchedulerCache) removeNodeFast(name string) {
	if idx, exists := sc.nodeListIndex[name]; exists {
		last := len(sc.NodeList) - 1
		if idx != last {
			sc.NodeList[idx] = sc.NodeList[last]
			sc.nodeListIndex[sc.NodeList[idx]] = idx
		}
		sc.NodeList[last] = ""
		sc.NodeList = sc.NodeList[:last]
		delete(sc.nodeListIndex, name)
	}
}
