/*
Copyright 2018 The Kubernetes Authors.

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
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "predicates"

	// MemoryPressurePredicate is the key for enabling Memory Pressure Predicate in YAML
	MemoryPressurePredicate = "predicate.MemoryPressureEnable"
	// DiskPressurePredicate is the key for enabling Disk Pressure Predicate in YAML
	DiskPressurePredicate = "predicate.DiskPressureEnable"
	// PIDPressurePredicate is the key for enabling PID Pressure Predicate in YAML
	PIDPressurePredicate = "predicate.PIDPressureEnable"
	// GPUSharingPredicate is the key for enabling GPU Sharing Predicate in YAML
	GPUSharingPredicate = "predicate.GPUSharingEnable"
)

type predicatesPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return predicate plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &predicatesPlugin{pluginArguments: arguments}
}

func (pp *predicatesPlugin) Name() string {
	return PluginName
}

type predicateEnable struct {
	memoryPressureEnable bool
	diskPressureEnable   bool
	pidPressureEnable    bool
	gpuSharingEnable     bool
}

func enablePredicate(args framework.Arguments) predicateEnable {

	/*
		   User Should give predicatesEnable in this format(predicate.MemoryPressureEnable, predicate.DiskPressureEnable, predicate.PIDPressureEnable, predicate.GPUSharingEnable.
		   Currently supported only for MemoryPressure, DiskPressure, PIDPressure predicate checks.

		   actions: "reclaim, allocate, backfill, preempt"
		   tiers:
		   - plugins:
		     - name: priority
		     - name: gang
		     - name: conformance
		   - plugins:
		     - name: drf
		     - name: predicates
		       arguments:
		 		 predicate.MemoryPressureEnable: true
		 		 predicate.DiskPressureEnable: true
				 predicate.PIDPressureEnable: true
				 predicate.GPUSharingEnable: true
		     - name: proportion
		     - name: nodeorder
	*/

	predicate := predicateEnable{
		memoryPressureEnable: false,
		diskPressureEnable:   false,
		pidPressureEnable:    false,
		gpuSharingEnable:     false,
	}

	// Checks whether predicate.MemoryPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.memoryPressureEnable, MemoryPressurePredicate)

	// Checks whether predicate.DiskPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.diskPressureEnable, DiskPressurePredicate)

	// Checks whether predicate.PIDPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.pidPressureEnable, PIDPressurePredicate)

	// Checks whether predicate.GPUSharingEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.gpuSharingEnable, GPUSharingPredicate)

	return predicate
}

func (pp *predicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := util.NewPodLister(ssn)
	pods, _ := pl.List(labels.NewSelector())
	nodeMap, nodeSlice := util.GenerateNodeMapAndSlice(ssn.Nodes)

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			pod := pl.UpdateTask(event.Task, event.Task.NodeName)

			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Warningf("predicates, update pod %s/%s allocate to NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
			} else {
				node.AddPod(pod)
				klog.V(4).Infof("predicates, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, nodeName)
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			pod := pl.UpdateTask(event.Task, "")

			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Warningf("predicates, update pod %s/%s allocate from NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
			} else {
				node.RemovePod(pod)
				klog.V(4).Infof("predicates, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
			}
		},
	})

	// Initialize k8s plugins
	// TODO: Add more predicates, k8s.io/kubernetes/pkg/scheduler/framework/plugins/legacy_registry.go
	handle := framework.NewFrameworkHandle(pods, nodeSlice)
	// 1. NodeUnschedulable
	plugin, _ := nodeunschedulable.New(nil, handle)
	nodeUnscheduleFilter := plugin.(*nodeunschedulable.NodeUnschedulable)
	// 2. NodeAffinity
	plugin, _ = nodeaffinity.New(nil, handle)
	nodeAffinityFilter := plugin.(*nodeaffinity.NodeAffinity)
	// 3. NodePorts
	plugin, _ = nodeports.New(nil, handle)
	nodePortFilter := plugin.(*nodeports.NodePorts)
	// 4. TaintToleration
	plugin, _ = tainttoleration.New(nil, handle)
	tolerationFilter := plugin.(*tainttoleration.TaintToleration)
	// 5. InterPodAffinity
	plugin, _ = interpodaffinity.New(nil, handle)
	podAffinityFilter := plugin.(*interpodaffinity.InterPodAffinity)

	ssn.AddPredicateFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		nodeInfo, found := nodeMap[node.Name]
		if !found {
			fmt.Errorf("failed to predicates, node info for %s not found", node.Name)
		}

		if node.Allocatable.MaxTaskNum <= len(nodeInfo.Pods()) {
			klog.V(4).Infof("NodePodNumber predicates Task <%s/%s> on Node <%s> failed",
				task.Namespace, task.Name, node.Name)
			return api.NewFitError(task, node, api.NodePodNumberExceeded)
		}

		state := k8sframework.NewCycleState()
		// CheckNodeUnschedulable
		status := nodeUnscheduleFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s predicates failed %s", nodeunschedulable.Name, status.Message())
		}

		// Check NodeAffinity
		status = nodeAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s predicates failed %s", nodeaffinity.Name, status.Message())
		}

		// Check NodePorts
		nodePortFilter.PreFilter(context.TODO(), state, task.Pod)
		status = nodePortFilter.Filter(context.TODO(), state, nil, nodeInfo)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s predicates failed %s", nodeaffinity.Name, status.Message())
		}

		// PodToleratesNodeTaints: TaintToleration
		status = tolerationFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s predicates failed %s", tainttoleration.Name, status.Message())
		}

		// InterPodAffinity Predicate
		status = podAffinityFilter.PreFilter(context.TODO(), state, task.Pod)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s pre-predicates failed %s", interpodaffinity.Name, status.Message())
		}

		status = podAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
		if !status.IsSuccess() {
			return fmt.Errorf("plugin %s predicates failed %s", interpodaffinity.Name, status.Message())
		}

		// if predicate.gpuSharingEnable
		if true {
			// CheckGPUSharingPredicate
			fit, err := CheckNodeGPUSharingPredicate(task.Pod, node)
			if err != nil {
				return err
			}

			klog.V(4).Infof("CheckNodeGPUSharingPredicate predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
				task.Namespace, task.Name, node.Name, fit, err)
		}

		return nil
	})
}

func (pp *predicatesPlugin) OnSessionClose(ssn *framework.Session) {}

// CheckNodeGPUSharingPredicate checks if a gpu sharing pod can be scheduled on a node.
func CheckNodeGPUSharingPredicate(pod *v1.Pod, nodeInfo *api.NodeInfo) (bool, error) {
	_, ok := nodeInfo.Node.Status.Capacity["volcano.sh/node-gpu-core"]
	if !ok {
		return false, fmt.Errorf("node is not gpu sharing")
	} else {
		isEnoughGPUMemoryOnNode := nodeInfo.CheckPredicatePodOnGPUNode(pod)
		if !isEnoughGPUMemoryOnNode {
			return false, fmt.Errorf("no enough gpu memory on single device")
		}
	}
	return true, nil
}
