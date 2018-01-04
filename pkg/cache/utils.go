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
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/listers/core/v1"
	clientcache "k8s.io/client-go/tools/cache"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

// TODO some operation for map[arbv1.ResourceName]resource.Quantity, it is better to enhance it in resource_info.go

func ResourcesAdd(res1 map[arbv1.ResourceName]resource.Quantity, res2 map[arbv1.ResourceName]resource.Quantity) map[arbv1.ResourceName]resource.Quantity {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	mem1 := res1["memory"].DeepCopy()
	mem2 := res2["memory"].DeepCopy()

	cpu1.Add(cpu2)
	mem1.Add(mem2)

	return map[arbv1.ResourceName]resource.Quantity{
		"cpu":    cpu1,
		"memory": mem1,
	}
}

func ResourcesSub(res1 map[arbv1.ResourceName]resource.Quantity, res2 map[arbv1.ResourceName]resource.Quantity) map[arbv1.ResourceName]resource.Quantity {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	mem1 := res1["memory"].DeepCopy()
	mem2 := res2["memory"].DeepCopy()

	if cpu1.Cmp(cpu2) <= 0 {
		cpu1 = resource.MustParse("0")
	} else {
		cpu1.Sub(cpu2)
	}
	if mem1.Cmp(mem2) <= 0 {
		mem1 = resource.MustParse("0")
	} else {
		mem1.Sub(mem2)
	}

	return map[arbv1.ResourceName]resource.Quantity{
		"cpu":    cpu1,
		"memory": mem1,
	}
}

func ResourcesIsZero(res map[arbv1.ResourceName]resource.Quantity) bool {
	cpu := res["cpu"].DeepCopy()
	mem := res["memory"].DeepCopy()

	if cpu.IsZero() && mem.IsZero() {
		return true
	}

	return false
}

// -1  - if res1 < res2
// 0   - if res1 = res2
// 1   - if not belong above cases
func CompareResources(res1 map[arbv1.ResourceName]resource.Quantity, res2 map[arbv1.ResourceName]resource.Quantity) int {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	memory1 := res1["memory"].DeepCopy()
	memory2 := res2["memory"].DeepCopy()

	if cpu1.Cmp(cpu2) <= 0 && memory1.Cmp(memory2) <= 0 {
		if cpu1.Cmp(cpu2) == 0 && memory1.Cmp(memory2) == 0 {
			return 0
		} else {
			return -1
		}
	} else {
		return 1
	}
}

func NodeLister(client clientset.Interface, stopChannel <-chan struct{}) ([]*v1.Node, error) {
	nl := GetNodeLister(client, stopChannel)
	nodes, err := nl.List(labels.Everything())
	if err != nil {
		return []*v1.Node{}, err
	}
	return nodes, err
}

func GetNodeLister(client clientset.Interface, stopChannel <-chan struct{}) corev1.NodeLister {
	listWatcher := clientcache.NewListWatchFromClient(client.Core().RESTClient(), "nodes", v1.NamespaceAll, fields.Everything())
	store := clientcache.NewIndexer(clientcache.MetaNamespaceKeyFunc, clientcache.Indexers{clientcache.NamespaceIndex: clientcache.MetaNamespaceIndexFunc})
	nodeLister := corev1.NewNodeLister(store)
	reflector := clientcache.NewReflector(listWatcher, &v1.Node{}, store, time.Hour)
	reflector.Run(stopChannel)

	return nodeLister
}

// podKey returns the string key of a pod.
func podKey(pod *v1.Pod) (string, error) {
	return clientcache.MetaNamespaceKeyFunc(pod)
}
