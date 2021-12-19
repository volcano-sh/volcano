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
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestAddNode(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	eh.AddNode(n1)

	if len(cache.NodeList) != 1 {
		t.Errorf("Unexpected nodeList number %d, expected %d.", len(cache.NodeList), 1)
		return
	}

	if !nodeExist(cache, n1) {
		t.Errorf("Node not in the cache.")
		return
	}
}

func TestUpdateNode(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.Nodes["n1"] = schedulingapi.NewNodeInfo(n1)

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	n2 := buildNode("n1", buildResourceList("4000m", "10G"))

	eh.AddNode(n1)
	eh.UpdateNode(n1, n2)

	if len(cache.NodeList) != 1 {
		t.Errorf("Unexpected nodeList number %d, expected %d.", len(cache.NodeList), 1)
		return
	}

	if nodeExist(cache, n1) {
		t.Errorf("Old node in the cache.")
		return
	}

	if !nodeExist(cache, n2) {
		t.Errorf("New node not in the cache.")
		return
	}
}

func TestDeleteNode(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.Nodes["n1"] = schedulingapi.NewNodeInfo(n1)

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.DeleteNode(n1)
	if len(cache.Nodes) != 0 {
		t.Errorf("Unexpected node length %d, expected %d.", len(cache.Nodes), 0)
		return
	}

	if nodeExist(cache, n1) {
		t.Errorf("Node still in the cache.")
		return
	}
}

func TestAddPod(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs: make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
	}

	p1 := buildPod(
		"test-ns",
		"p1",
		"",
		v1.PodPending,
		buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner},
		make(map[string]string),
	)
	p1.Annotations = make(map[string]string)
	p1.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] = "pg1"

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPod(p1)
	if !podExist(cache, p1) {
		t.Errorf("Pod not in the cache.")
		return
	}
}

func TestUpdatePod(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs: make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
	}

	p1 := buildPod(
		"test-ns",
		"p1",
		"",
		v1.PodPending,
		buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner},
		make(map[string]string),
	)
	p1.Annotations = make(map[string]string)
	p1.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] = "pg1"

	p2 := buildPod(
		"test-ns",
		"p1",
		"",
		v1.PodPending,
		buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner},
		make(map[string]string),
	)
	p2.Annotations = p1.Annotations

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.UpdatePod(p1, p2)
	if !podExist(cache, p2) {
		t.Errorf("Pod not in the cache.")
		return
	}

	jobInfo, _ := cache.Jobs[schedulingapi.JobID(fmt.Sprintf("%s/%s", p2.Namespace, p2.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey]))]
	task, ok := jobInfo.Tasks[schedulingapi.TaskID("test-ns-p1")]
	if !ok {
		t.Errorf("Task not in the taskMap of job.")
		return
	}

	if !reflect.DeepEqual(task.Pod, p2) {
		t.Errorf("Pod update failed.")
		return
	}
}

func TestDeletePod(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:        make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		deletedJobs: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	p1 := buildPod(
		"test-ns",
		"p1",
		"",
		v1.PodPending,
		buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner},
		make(map[string]string),
	)
	p1.Annotations = make(map[string]string)
	p1.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] = "pg1"

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPod(p1)
	eh.DeletePod(p1)
	cache.processCleanupJob()

	if podExist(cache, p1) {
		t.Errorf("Pod not deleted.")
		return
	}
}

func TestAddPriorityClass(t *testing.T) {
	cache := &SchedulerCache{
		PriorityClasses: make(map[string]*schedulingv1.PriorityClass),
	}

	pc := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc1",
		},
		Value: 10,
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPriorityClass(pc)
	if _, ok := cache.PriorityClasses[pc.Name]; !ok {
		t.Errorf("Priority not in the cache.")
		return
	}
}

func TestUpdatePriorityClass(t *testing.T) {
	cache := &SchedulerCache{
		PriorityClasses: make(map[string]*schedulingv1.PriorityClass),
	}

	pc1 := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc1",
		},
		Value: 10,
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	pc2 := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc1",
		},
		Value: 100,
	}

	eh.UpdatePriorityClass(pc1, pc2)
	pc, ok := cache.PriorityClasses[pc2.Name]
	if !ok {
		t.Errorf("PriorityClass not exist in the cache.")
		return
	}

	if pc.Value != 100 {
		t.Errorf("PriorityClass was not the latest.")
		return
	}
}

func TestDeletePriorityClass(t *testing.T) {
	cache := &SchedulerCache{
		PriorityClasses: make(map[string]*schedulingv1.PriorityClass),
	}

	pc := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc1",
		},
		Value: 10,
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPriorityClass(pc)
	eh.DeletePriorityClass(pc)
	if _, ok := cache.PriorityClasses[pc.Name]; ok {
		t.Errorf("PriorityClass still in the cache.")
		return
	}
}

func TestAddResourceQuota(t *testing.T) {
	cache := &SchedulerCache{
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
	}

	rq := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "quota",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("3"),
				v1.ResourceMemory: resource.MustParse("100Gi"),
				v1.ResourcePods:   resource.MustParse("5"),
			},
			Scopes: []v1.ResourceQuotaScope{v1.ResourceQuotaScopeBestEffort},
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddResourceQuota(rq)
	if _, ok := cache.NamespaceCollection[rq.Namespace]; !ok {
		t.Errorf("Quota not exist in the cache.")
		return
	}
}

func TestUpdateResourceQuota(t *testing.T) {
	cache := &SchedulerCache{
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
	}

	rq := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "quota",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("3"),
				v1.ResourceMemory: resource.MustParse("100Gi"),
				v1.ResourcePods:   resource.MustParse("5"),
			},
			Scopes: []v1.ResourceQuotaScope{v1.ResourceQuotaScopeBestEffort},
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.UpdateResourceQuota(nil, rq)
	if _, ok := cache.NamespaceCollection[rq.Namespace]; !ok {
		t.Errorf("Quota not exist in the cache.")
		return
	}
}

func TestDeleteResourceQuota(t *testing.T) {
	cache := &SchedulerCache{
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
	}

	rq := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "quota",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("3"),
				v1.ResourceMemory: resource.MustParse("100Gi"),
				v1.ResourcePods:   resource.MustParse("5"),
			},
			Scopes: []v1.ResourceQuotaScope{v1.ResourceQuotaScopeBestEffort},
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddResourceQuota(rq)
	eh.DeleteResourceQuota(rq)
}

func TestAddPodGroup(t *testing.T) {
	cache := &SchedulerCache{
		Jobs: make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
	}

	pg := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "j1",
			Namespace: "testing",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPodGroup(pg)

	if _, ok := cache.Jobs["testing/j1"]; !ok {
		t.Errorf("PodGroup not exist in the cache.")
		return
	}
}

func TestUpdatePodGroup(t *testing.T) {
	cache := &SchedulerCache{
		Jobs: make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
	}

	pg1 := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "j1",
			Namespace:       "testing",
			ResourceVersion: "0",
		},
	}

	pg2 := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "j1",
			Namespace:       "testing",
			ResourceVersion: "1",
		},
		Spec: schedulingv1beta1.PodGroupSpec{
			MinMember: 2,
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.UpdatePodGroup(pg1, pg2)
	job, ok := cache.Jobs["testing/j1"]
	if !ok {
		t.Errorf("PodGroup not exist in the cache.")
		return
	}

	if job.PodGroup == nil {
		t.Errorf("PodGroup of job is nil.")
		return
	}

	if job.PodGroup.Spec.MinMember != 2 {
		t.Errorf("Unexpected podGroup minMember %d, expected %d.", job.PodGroup.Spec.MinMember, 2)
		return
	}
}

func TestDeletePodGroup(t *testing.T) {
	cache := &SchedulerCache{
		Jobs:        make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		deletedJobs: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	pg := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "j1",
			Namespace:       "testing",
			ResourceVersion: "0",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddPodGroup(pg)
	eh.DeletePodGroup(pg)
	cache.processCleanupJob()
	if len(cache.Jobs) != 0 {
		t.Errorf("PodGroup still in the cache.")
		return
	}
}

func TestAddQueue(t *testing.T) {
	cache := &SchedulerCache{
		Queues: make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
	}

	queue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "q1",
			Namespace: "testing",
		},
	}
	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddQueue(queue)
	if _, ok := cache.Queues[schedulingapi.QueueID(queue.Name)]; !ok {
		t.Errorf("Queue not exist in the cache.")
		return
	}
}

func TestUpdateQueue(t *testing.T) {
	cache := &SchedulerCache{
		Queues: make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
	}

	q1 := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "q1",
			Namespace:       "testing",
			ResourceVersion: "1",
		},
	}

	q2 := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "q1",
			Namespace:       "testing",
			ResourceVersion: "2",
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight: 2,
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.UpdateQueue(q1, q2)

	q, ok := cache.Queues[schedulingapi.QueueID(q2.Name)]
	if !ok {
		t.Errorf("Queue not exist in the cache.")
		return
	}

	if q.Weight != 2 {
		t.Errorf("Unexpecte queue weight %d, expected %d.", q.Weight, 2)
		return
	}
}

func TestDeleteQueue(t *testing.T) {
	cache := &SchedulerCache{
		Queues: make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
	}

	queue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "q1",
			Namespace:       "testing",
			ResourceVersion: "1",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddQueue(queue)
	eh.DeleteQueue(queue)
	if len(cache.Queues) != 0 {
		t.Errorf("Queue still in the cache.")
		return
	}
}

func TestAddNUMATopology(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.Nodes["n1"] = schedulingapi.NewNodeInfo(n1)

	nt := &nodeinfov1alpha1.Numatopology{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "n1",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddNUMATopology(nt)
	if cache.Nodes["n1"].NumaInfo == nil {
		t.Errorf("Unexpected nil numaInfo for the node.")
	}
}

func TestUpdateNUMATopology(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.Nodes["n1"] = schedulingapi.NewNodeInfo(n1)

	nt := &nodeinfov1alpha1.Numatopology{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "n1",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.UpdateNUMATopology(nil, nt)
	if cache.Nodes["n1"].NumaInfo == nil {
		t.Errorf("Unexpected nil numaInfo for the node.")
	}
}

func TestDeleteNUMATopology(t *testing.T) {
	cache := &SchedulerCache{
		Nodes: make(map[string]*schedulingapi.NodeInfo),
	}

	n1 := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.Nodes["n1"] = schedulingapi.NewNodeInfo(n1)

	nt := &nodeinfov1alpha1.Numatopology{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      "nt",
		},
	}

	eh, err := GetEventHandler("default", "", cache, nil)
	if err != nil {
		t.Errorf("Unexpected get event handler error %v.", err)
		return
	}

	eh.AddNUMATopology(nt)
	eh.DeleteNUMATopology(nt)
	if cache.Nodes["n1"].NumaInfo != nil {
		t.Errorf("Not nil numaInfo for the node.")
		return
	}
}

func nodeExist(cache *SchedulerCache, node *v1.Node) bool {
	if cache == nil {
		return false
	}

	if node == nil {
		return false
	}

	ni, ok := cache.Nodes[node.Name]
	if !ok {
		return false
	}

	if ni == nil {
		return false
	}

	if !reflect.DeepEqual(ni.Node, node) {
		return false
	}

	return true
}

func podExist(cache *SchedulerCache, pod *v1.Pod) bool {
	if cache == nil {
		return false
	}

	if pod == nil {
		return false
	}

	_, ok := cache.Jobs[schedulingapi.JobID(
		fmt.Sprintf("%s/%s", pod.Namespace, pod.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey]))]
	if !ok {
		return false
	}

	return true
}
