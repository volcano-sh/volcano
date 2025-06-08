/*
Copyright 2019 The Volcano Authors.

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

package reservation

import (
	"fmt"

	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "reservation"
)

type reservationPlugin struct {
	// Arguments given for the plugin
}

// New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	return &reservationPlugin{}
}

func (rp *reservationPlugin) Name() string {
	return PluginName
}

func (rp *reservationPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter reservation plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving reservation plugin...")
	}()
	validJobFn := func(obj interface{}) *api.ValidateResult {
		job, ok := obj.(*api.JobInfo)
		if !ok {
			return &api.ValidateResult{
				Pass:    false,
				Message: fmt.Sprintf("Failed to convert <%v> to *JobInfo", obj),
			}
		}
		ssn.MatchReservationForPod(job)

		if valid := ssn.CheckReservationAvailable(job); !valid {
			return &api.ValidateResult{
				Pass:    false,
				Reason:  v1beta1.InvalidReservationReason,
				Message: fmt.Sprintf("Reservation specified by job <%s/%s> is not Available", job.Namespace, job.Name),
			}
		}

		if ownerMatched := ssn.CheckReservationOwners(job); !ownerMatched {
			return &api.ValidateResult{
				Pass:   false,
				Reason: v1beta1.ReservationOwnerNotMatchReason,
				Message: fmt.Sprintf(
					"Reservation specified by job <%s/%s> is not owned by the job (ownership mismatch by ownerObject  or label selectors)",
					job.Namespace, job.Name,
				),
			}
		}

		if specMatched := ssn.CheckReservationMatch(job); !specMatched {
			return &api.ValidateResult{
				Pass:   false,
				Reason: v1beta1.ReservationSpecNotMatchReason,
				Message: fmt.Sprintf(
					"Reservation specified by job <%s/%s> does not match job task spec: task count or PodSpec does not match",
					job.Namespace, job.Name,
				),
			}
		}
		return nil
	}

	ssn.AddJobValidFn(rp.Name(), validJobFn)

	bestNodeFn := func(task *api.TaskInfo, scores map[float64][]*api.NodeInfo) *api.NodeInfo {
		klog.V(5).Infof("[debug]: enter reservation best node function for task %s/%s", task.Namespace, task.Name)
		if !task.IsReservationTask() {
			return nil
		}

		reservationNodeNames := task.ReservationNodeNames
		if len(reservationNodeNames) == 0 {
			return nil
		}

		nodeSet := make(map[string]struct{})
		for _, nodeList := range scores {
			for _, node := range nodeList {
				nodeSet[node.Name] = struct{}{}
			}
		}

		// match reservation node names specified in given order with available nodes
		for _, reserved := range reservationNodeNames {
			if _, ok := nodeSet[reserved]; ok {
				klog.V(5).Infof("[debug]: Found reservation node %s for task %s/%s", reserved, task.Namespace, task.Name)
				return ssn.Nodes[reserved]
			}
		}

		klog.V(5).Infof("[debug]: None of the specified reserved nodes are available for task %s/%s, falling back to scheduler default decision", task.Namespace, task.Name)
		return nil
	}
	ssn.AddBestNodeFn(rp.Name(), bestNodeFn)
}

func (rp *reservationPlugin) OnSessionClose(ssn *framework.Session) {}
