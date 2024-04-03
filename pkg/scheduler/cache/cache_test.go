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
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	kcache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildNode(name string, alloc v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID(name),
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func buildPod(ns, n, nn string,
	p v1.PodPhase, req v1.ResourceList,
	owner []metav1.OwnerReference, labels map[string]string) *v1.Pod {

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", ns, n)),
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
			Labels:          labels,
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			NodeName: nn,
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
		},
	}
}

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func TestGetOrCreateJob(t *testing.T) {
	owner1 := buildOwnerReference("j1")
	owner2 := buildOwnerReference("j2")

	pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner1}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.

	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pod2.Spec.SchedulerName = "volcano"
	pi2 := api.NewTaskInfo(pod2)

	pod3 := buildPod("c3", "p3", "n1", v1.PodRunning, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pi3 := api.NewTaskInfo(pod3)

	cache := &SchedulerCache{
		Nodes:          make(map[string]*api.NodeInfo),
		Jobs:           make(map[api.JobID]*api.JobInfo),
		schedulerNames: []string{"volcano"},
	}

	tests := []struct {
		task   *api.TaskInfo
		gotJob bool // whether getOrCreateJob will return job for corresponding task
	}{
		{
			task:   pi1,
			gotJob: true,
		},
		{
			task:   pi2,
			gotJob: false,
		},
		{
			task:   pi3,
			gotJob: false,
		},
	}
	for i, test := range tests {
		result := cache.getOrCreateJob(test.task) != nil
		if result != test.gotJob {
			t.Errorf("case %d: \n expected %t, \n got %t \n",
				i, test.gotJob, result)
		}
	}
}

func TestSchedulerCache_Bind_NodeWithSufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:  make(map[api.JobID]*api.JobInfo),
		Nodes: make(map[string]*api.NodeInfo),
		Binder: &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		},
		BindFlowChannel: make(chan *api.TaskInfo, 5000),
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	cache.AddOrUpdateNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"
	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}
	task.NodeName = "n1"
	err := cache.AddBindTask(task)
	if err != nil {
		t.Errorf("failed to bind pod to node: %v", err)
	}
}

func TestSchedulerCache_Bind_NodeWithInsufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:  make(map[api.JobID]*api.JobInfo),
		Nodes: make(map[string]*api.NodeInfo),
		Binder: &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		},
		BindFlowChannel: make(chan *api.TaskInfo, 5000),
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("5000m", "50G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	cache.AddOrUpdateNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"

	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}

	task.NodeName = "n1"
	taskBeforeBind := task.Clone()
	nodeBeforeBind := cache.Nodes["n1"].Clone()

	err := cache.AddBindTask(task)
	if err == nil {
		t.Errorf("expected bind to fail for node with insufficient resources")
	}

	_, taskAfterBind, err := cache.findJobAndTask(task)
	if err != nil {
		t.Errorf("expected to find task after failed bind")
	}
	if !reflect.DeepEqual(taskBeforeBind, taskAfterBind) {
		t.Errorf("expected task to remain the same after failed bind: \n %#v\n %#v", taskBeforeBind, taskAfterBind)
	}

	nodeAfterBind := cache.Nodes["n1"].Clone()
	if !reflect.DeepEqual(nodeBeforeBind, nodeAfterBind) {
		t.Errorf("expected node to remain the same after failed bind")
	}
}

func TestNodeOperation(t *testing.T) {
	// case 1
	node1 := buildNode("n1", api.BuildResourceList("2000m", "10G"))
	node2 := buildNode("n2", api.BuildResourceList("4000m", "16G"))
	node3 := buildNode("n3", api.BuildResourceList("3000m", "12G"))
	nodeInfo1 := api.NewNodeInfo(node1)
	nodeInfo2 := api.NewNodeInfo(node2)
	nodeInfo3 := api.NewNodeInfo(node3)
	tests := []struct {
		deletedNode *v1.Node
		nodes       []*v1.Node
		expected    *SchedulerCache
		delExpect   *SchedulerCache
	}{
		{
			deletedNode: node2,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n3"},
			},
		},
		{
			deletedNode: node1,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n2", "n3"},
			},
		},
		{
			deletedNode: node3,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
					"n3": nodeInfo3,
				},
				NodeList: []string{"n1", "n2", "n3"},
			},
			delExpect: &SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					"n1": nodeInfo1,
					"n2": nodeInfo2,
				},
				NodeList: []string{"n1", "n2"},
			},
		},
	}

	for i, test := range tests {
		cache := &SchedulerCache{
			Nodes:    make(map[string]*api.NodeInfo),
			NodeList: []string{},
		}

		for _, n := range test.nodes {
			cache.AddOrUpdateNode(n)
		}

		if !reflect.DeepEqual(cache, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, cache)
		}

		// delete node
		cache.RemoveNode(test.deletedNode.Name)
		if !reflect.DeepEqual(cache, test.delExpect) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.delExpect, cache)
		}
	}
}

func TestBindTasks(t *testing.T) {
	owner := buildOwnerReference("j1")
	scheduler := "fake-scheduler"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := NewDefaultMockSchedulerCache(scheduler)
	sc.Run(ctx.Done())
	kubeCli := sc.kubeClient

	pod := buildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{owner}, make(map[string]string))
	node := buildNode("n1", api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	pod.Annotations = map[string]string{"scheduling.k8s.io/group-name": "j1"}
	pod.Spec.SchedulerName = scheduler

	// make sure pod exist when calling fake client binding
	kubeCli.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	// set node in cache directly
	kubeCli.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})

	// wait for pod synced
	time.Sleep(100 * time.Millisecond)
	task := api.NewTaskInfo(pod)
	task.NodeName = "n1"
	err := sc.AddBindTask(task)
	if err != nil {
		t.Errorf("failed to bind pod to node: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	r := sc.Recorder.(*record.FakeRecorder)
	if len(r.Events) != 1 {
		t.Fatalf("succesfully binding task should have 1 event")
	}
}

var resourceKey = schema.GroupVersionResource{
	Group:    "example.io",
	Version:  "v1",
	Resource: "foo",
}

type testResource struct {
	name     string
	sc       *SchedulerCache
	ch       chan struct{}
	handlers CustomResourceEventHandlerFuncs
}

func (t *testResource) Name() string {
	return t.name
}

func (t *testResource) EventHandlerFuncs(gvr schema.GroupVersionResource) kcache.ResourceEventHandler {
	return t.handlers[gvr]
}

func newHandler(ch chan struct{}, sc *SchedulerCache) (CustomResourceEventHandler, error) {
	t := &testResource{
		name:     "test-handler",
		sc:       sc,
		ch:       ch,
		handlers: make(CustomResourceEventHandlerFuncs),
	}

	t.handlers[resourceKey] = kcache.ResourceEventHandlerFuncs{
		AddFunc: t.onResourceAdd,
	}
	return t, nil
}

func (t *testResource) onResourceAdd(obj interface{}) {
	if _, ok := t.sc.CustomResources[resourceKey]; !ok {
		t.sc.CustomResources[resourceKey] = make(map[string]schedulingapi.CustomResource)
	}

	t.sc.Mutex.Lock()
	defer t.sc.Mutex.Unlock()

	t.sc.CustomResources[resourceKey]["name-foo"] = nil
	t.ch <- struct{}{}
	klog.InfoS("Added resource")
}

func newUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

func Test_add_custom_event_handler(t *testing.T) {
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "example.io", Version: "v1", Resource: "foo"}: "fooList",
	}
	ch := make(chan struct{})
	tests := []struct {
		name        string
		sc          *SchedulerCache
		handlerName string
		creator     CustomResourceEventHandlerCreator
		trigger     func(gvr schema.GroupVersionResource, client dynamic.Interface) error
		want        map[schema.GroupVersionResource]map[string]schedulingapi.CustomResource
	}{
		{
			name:        "custom resource add",
			handlerName: "test",
			sc: &SchedulerCache{
				dynClient:         fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), gvrToListKind),
				CustomResources:   make(map[schema.GroupVersionResource]map[string]schedulingapi.CustomResource),
				CustomResourceGVR: []string{"foo.v1.example.io"}},
			creator: func(sc *SchedulerCache) (CustomResourceEventHandler, error) {
				return newHandler(ch, sc)
			},
			trigger: func(gvr schema.GroupVersionResource, client dynamic.Interface) error {
				obj := newUnstructured("example.io/v1", "foo", "ns-foo", "name-foo")
				_, err := client.Resource(resourceKey).Namespace("ns-foo").Create(context.TODO(), obj, metav1.CreateOptions{})

				return err
			},
			want: map[schema.GroupVersionResource]map[string]schedulingapi.CustomResource{
				resourceKey: {
					"name-foo": nil,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterCustomResourceEventHandler(tt.handlerName, tt.creator)
			tt.sc.addEventHandler()
			tt.sc.DynInformerFactory.Start(wait.NeverStop)
			if synced := tt.sc.DynInformerFactory.WaitForCacheSync(wait.NeverStop); !synced[resourceKey] {
				t.Errorf("faield to wait cache sync")
			}
			if tt.trigger != nil {
				assert.NoError(t, tt.trigger(resourceKey, tt.sc.dynClient))
			}

			<-ch
			got := tt.sc.CustomResources
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetEventHandlerByGVR() = %v, want %v", got, tt.want)
			}
		})
	}
}
