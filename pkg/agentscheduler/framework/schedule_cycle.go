/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added k8s client integration and event recording capabilities
- Enhanced with HyperNode support for network topology aware scheduling
- Extended plugin system with additional extension points

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

package framework

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// Session information for the current session
type ScheduleCycle struct {
	UID types.UID

	kubeClient kubernetes.Interface
	vcClient   vcclient.Interface
	recorder   record.EventRecorder
	cache      cache.Cache
	// restConfig      *rest.Config
	informerFactory informers.SharedInformerFactory

	Nodes         map[string]*api.NodeInfo
	NamespaceInfo map[api.NamespaceName]*api.NamespaceInfo

	// NodeMap is like Nodes except that it uses k8s NodeInfo api and should only
	// be used in k8s compatible api scenarios such as in predicates and nodeorder plugins.
	NodeMap map[string]fwk.NodeInfo
	// PodLister *PodLister

	Tiers []conf.Tier
	// Configurations []conf.Configuration
	NodeList []*api.NodeInfo

	// plugins map[string]Plugin
	// eventHandlers   []*EventHandler
	predicateFns    map[string]api.PredicateFn
	prePredicateFns map[string]api.PrePredicateFn
	// bestNodeFns         map[string]api.BestNodeFn
	nodeOrderFns      map[string]api.NodeOrderFn
	batchNodeOrderFns map[string]api.BatchNodeOrderFn
	nodeMapFns        map[string]api.NodeMapFn
	nodeReduceFns     map[string]api.NodeReduceFn

	plugins    map[string]Plugin
	cycleState *k8sframework.CycleState
}

func startSchedulingCycle(cache cache.Cache, plugins map[string]Plugin) *ScheduleCycle {
	sc := &ScheduleCycle{
		UID:        uuid.NewUUID(),
		kubeClient: cache.Client(),
		vcClient:   cache.VCClient(),
		// restConfig:      cache.ClientConfig(),
		recorder:        cache.EventRecorder(),
		cache:           cache,
		informerFactory: cache.SharedInformerFactory(),

		Nodes: map[string]*api.NodeInfo{},

		// plugins:           map[string]Plugin{},
		predicateFns:      map[string]api.PredicateFn{},
		prePredicateFns:   map[string]api.PrePredicateFn{},
		nodeOrderFns:      map[string]api.NodeOrderFn{},
		batchNodeOrderFns: map[string]api.BatchNodeOrderFn{},
		nodeMapFns:        map[string]api.NodeMapFn{},
		nodeReduceFns:     map[string]api.NodeReduceFn{},
		plugins:           plugins,
	}

	snapshot := cache.Snapshot()

	sc.NodeList = util.GetNodeList(snapshot.Nodes, snapshot.NodeList)
	sc.Nodes = snapshot.Nodes
	sc.NamespaceInfo = snapshot.NamespaceInfo

	klog.V(3).Infof("Start Schedule Cycle %v",
		sc.UID)

	return sc
}

func endCycle(sc *ScheduleCycle) {
	// ju := NewJobUpdater(sc)
	// ju.UpdateAll()

	sc.Nodes = nil
	// sc.plugins = nil
	// sc.eventHandlers = nil
	sc.NodeList = nil

	klog.V(3).Infof("Close Session %v", sc.UID)
}

// PredicateForAllocateAction checks if the predicate error contains
// - Unschedulable
// - UnschedulableAndUnresolvable
// - ErrorSkipOrWait
func (sc *ScheduleCycle) PredicateForAllocateAction(task *api.TaskInfo, node *api.NodeInfo) error {
	err := sc.PredicateFn(task, node)
	if err == nil {
		return nil
	}

	fitError, ok := err.(*api.FitError)
	if !ok {
		return api.NewFitError(task, node, err.Error())
	}

	statusSets := fitError.Status
	if statusSets.ContainsUnschedulable() || statusSets.ContainsUnschedulableAndUnresolvable() ||
		statusSets.ContainsErrorSkipOrWait() {
		return fitError
	}
	return nil
}

// Allocate the task to the node in the session
func (sc *ScheduleCycle) Allocate(task *api.TaskInfo, nodeInfo *api.NodeInfo) (err error) {
	hostname := nodeInfo.Name
	task.Pod.Spec.NodeName = hostname

	// Only update status in session
	// job, found := sc.Jobs[task.Job]
	// if found {
	// 	if err := job.UpdateTaskStatus(task, api.Allocated); err != nil {
	// 		klog.Errorf("Failed to update task <%v/%v> status to %v when binding in Session <%v>: %v",
	// 			task.Namespace, task.Name, api.Allocated, sc.UID, err)
	// 		return err
	// 	}
	// } else {
	// 	klog.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
	// 		task.Job, sc.UID)
	// 	return fmt.Errorf("failed to find job %s", task.Job)
	// }

	task.NodeName = hostname

	if node, found := sc.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			klog.Errorf("Failed to add task <%v/%v> to node <%v>  when binding in Session <%v>: %v",
				task.Namespace, task.Name, hostname, sc.UID, err)
			return err
		}
		klog.V(3).Infof("After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		klog.Errorf("Failed to find Node <%s> in Session <%s> index when binding.",
			hostname, sc.UID)
		return fmt.Errorf("failed to find node %s", hostname)
	}

	// Callbacks
	// for _, eh := range sc.eventHandlers {
	// 	if eh.AllocateFunc != nil {
	// 		eh.AllocateFunc(&Event{
	// 			Task: task,
	// 		})
	// 	}
	// }

	// if sc.JobReady(job) {
	// 	for _, task := range job.TaskStatusIndex[api.Allocated] {
	// 		if err := sc.dispatch(task); err != nil {
	// 			klog.Errorf("Failed to dispatch task <%v/%v>: %v",
	// 				task.Namespace, task.Name, err)
	// 			return err
	// 		}
	// 	}
	// }

	return nil
}

func (sc *ScheduleCycle) CreateBindContext(task *api.TaskInfo) *cache.BindContext {
	bindContext := &cache.BindContext{
		TaskInfo:   task,
		Extensions: make(map[string]cache.BindContextExtension),
	}

	for _, plugin := range sc.plugins {
		// If the plugin implements the BindContextHandler interface, call the SetupBindContextExtension method.
		if handler, ok := plugin.(BindContextHandler); ok {
			handler.SetupBindContextExtension(sc.cycleState, bindContext)
		}
	}

	return bindContext
}

// AddEventHandler add event handlers
// func (sc *ScheduleCycle) AddEventHandler(eh *EventHandler) {
// 	sc.eventHandlers = append(sc.eventHandlers, eh)
// }

// KubeClient returns the kubernetes client
func (sc *ScheduleCycle) KubeClient() kubernetes.Interface {
	return sc.kubeClient
}

// VCClient returns the volcano client
func (sc *ScheduleCycle) VCClient() vcclient.Interface {
	return sc.vcClient
}

// // ClientConfig returns the rest client
// func (sc *ScheduleCycle) ClientConfig() *rest.Config {
// 	return sc.restConfig
// }

// InformerFactory returns the scheduler ShareInformerFactory
func (sc *ScheduleCycle) InformerFactory() informers.SharedInformerFactory {
	return sc.informerFactory
}

// SharedDRAManager returns the shared DRAManager from cache
func (sc *ScheduleCycle) SharedDRAManager() k8sframework.SharedDRAManager {
	return sc.cache.SharedDRAManager()
}

// String return nodes and jobs information in the session
func (sc *ScheduleCycle) String() string {
	msg := fmt.Sprintf("Session %v: \n", sc.UID)

	for _, node := range sc.Nodes {
		msg = fmt.Sprintf("%s%v\n", msg, node)
	}

	return msg
}

func (sc *ScheduleCycle) GetCycleState(taskID api.TaskID) *k8sframework.CycleState {
	// only on pod is scheduled in on cycle, so ignote the parameter
	if sc.cycleState == nil {
		sc.cycleState = k8sframework.NewCycleState()
	}
	return sc.cycleState
}
