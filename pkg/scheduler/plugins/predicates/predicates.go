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
	"fmt"
	"strings"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/pkg/scheduler/algorithm/predicates"
	"k8s.io/kubernetes/pkg/scheduler/cache"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/plugins/util"
)

const (
	// MemoryPressurePredicate is the key for enabling Memory Pressure Predicate in YAML
	MemoryPressurePredicate = "predicate.MemoryPressureEnable"
	// DiskPressurePredicate is the key for enabling Disk Pressure Predicate in YAML
	DiskPressurePredicate = "predicate.DiskPressureEnable"
	// PIDPressurePredicate is the key for enabling PID Pressure Predicate in YAML
	PIDPressurePredicate = "predicate.PIDPressureEnable"
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
	return "predicates"
}

func formatReason(reasons []algorithm.PredicateFailureReason) string {
	reasonStrings := []string{}
	for _, v := range reasons {
		reasonStrings = append(reasonStrings, fmt.Sprintf("%v", v.GetReason()))
	}

	return strings.Join(reasonStrings, ", ")
}

type predicateEnable struct {
	memoryPressureEnable bool
	diskPressureEnable   bool
	pidPressureEnable    bool
}

func enablePredicate(args framework.Arguments) predicateEnable {

	/*
		   User Should give predicatesEnable in this format(predicate.MemoryPressureEnable, predicate.DiskPressureEnable, predicate.PIDPressureEnable.
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
		     - name: proportion
		     - name: nodeorder
	*/

	predicate := predicateEnable{
		memoryPressureEnable: false,
		diskPressureEnable:   false,
		pidPressureEnable:    false,
	}

	// Checks whether predicate.MemoryPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.memoryPressureEnable, MemoryPressurePredicate)

	// Checks whether predicate.DiskPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.diskPressureEnable, DiskPressurePredicate)

	// Checks whether predicate.PIDPressureEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.pidPressureEnable, PIDPressurePredicate)

	return predicate
}

func (pp *predicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := &util.PodLister{
		Session: ssn,
	}

	ni := &util.CachedNodeInfo{
		Session: ssn,
	}

	predicate := enablePredicate(pp.pluginArguments)

	ssn.AddPredicateFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)

		if node.Allocatable.MaxTaskNum <= len(nodeInfo.Pods()) {
			return fmt.Errorf("node <%s> can not allow more task running on it", node.Name)
		}

		// CheckNodeCondition Predicate
		fit, reasons, err := predicates.CheckNodeConditionPredicate(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("CheckNodeCondition predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> are not available to schedule task <%s/%s>: %s",
				node.Name, task.Namespace, task.Name, formatReason(reasons))
		}

		// CheckNodeUnschedulable Predicate
		fit, _, err = predicates.CheckNodeUnschedulablePredicate(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("CheckNodeUnschedulable Predicate Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> node <%s> set to unschedulable",
				task.Namespace, task.Name, node.Name)
		}

		// NodeSelector Predicate
		fit, _, err = predicates.PodMatchNodeSelector(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("NodeSelect predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> didn't match task <%s/%s> node selector",
				node.Name, task.Namespace, task.Name)
		}

		// HostPorts Predicate
		fit, _, err = predicates.PodFitsHostPorts(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("HostPorts predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> didn't have available host ports for task <%s/%s>",
				node.Name, task.Namespace, task.Name)
		}

		// Toleration/Taint Predicate
		fit, _, err = predicates.PodToleratesNodeTaints(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("Toleration/Taint predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> does not tolerate node <%s> taints",
				task.Namespace, task.Name, node.Name)
		}

		if predicate.memoryPressureEnable {
			// CheckNodeMemoryPressurePredicate
			fit, _, err = predicates.CheckNodeMemoryPressurePredicate(task.Pod, nil, nodeInfo)
			if err != nil {
				return err
			}

			glog.V(4).Infof("CheckNodeMemoryPressure predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
				task.Namespace, task.Name, node.Name, fit, err)

			if !fit {
				return fmt.Errorf("node <%s> are not available to schedule task <%s/%s> due to Memory Pressure",
					node.Name, task.Namespace, task.Name)
			}
		}

		if predicate.diskPressureEnable {
			// CheckNodeDiskPressurePredicate
			fit, _, err = predicates.CheckNodeDiskPressurePredicate(task.Pod, nil, nodeInfo)
			if err != nil {
				return err
			}

			glog.V(4).Infof("CheckNodeDiskPressure predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
				task.Namespace, task.Name, node.Name, fit, err)

			if !fit {
				return fmt.Errorf("node <%s> are not available to schedule task <%s/%s> due to Disk Pressure",
					node.Name, task.Namespace, task.Name)
			}
		}

		if predicate.pidPressureEnable {
			// CheckNodePIDPressurePredicate
			fit, _, err = predicates.CheckNodePIDPressurePredicate(task.Pod, nil, nodeInfo)
			if err != nil {
				return err
			}

			glog.V(4).Infof("CheckNodePIDPressurePredicate predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
				task.Namespace, task.Name, node.Name, fit, err)

			if !fit {
				return fmt.Errorf("node <%s> are not available to schedule task <%s/%s> due to PID Pressure",
					node.Name, task.Namespace, task.Name)
			}
		}

		// Pod Affinity/Anti-Affinity Predicate
		podAffinityPredicate := predicates.NewPodAffinityPredicate(ni, pl)
		fit, _, err = podAffinityPredicate(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("Pod Affinity/Anti-Affinity predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> affinity/anti-affinity failed on node <%s>",
				node.Name, task.Namespace, task.Name)
		}

		return nil
	})
}

func (pp *predicatesPlugin) OnSessionClose(ssn *framework.Session) {}
