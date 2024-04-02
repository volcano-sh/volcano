/*
Copyright 2024 The Volcano Authors.

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

package networktopology

import (
	"errors"
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	networkTopologyKeys = "net-topology.keys" // keys for node labels, split by comma, which are used to distinguish same topology
)

type staticTopAware struct {
	weight       int
	records      map[api.JobID]string // map job id to node name, used to make affinity for same job's other tasks
	topologyKeys []string             // topology key list, eg: "idc, rack, switch", key with more priority put at front of list
}

func (st *staticTopAware) OnSessionOpen(ssn *framework.Session) {
	if len(st.topologyKeys) == 0 {
		return
	}
	// TODO: for job has running task, parse it and record node info

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		if task == nil || node == nil || node.Node == nil {
			return 0, errors.New("invalid task or node info in net-topology plugin")
		}
		target, ok := st.records[task.Job]
		if !ok { // first task of a job, has no initial node info
			return 0, nil
		}
		tNode := ssn.Nodes[target]
		if tNode == nil || tNode.Node == nil {
			return 0, fmt.Errorf("target node %s not found", target)
		}
		score := 0
		weight := st.weight

		tlabels := tNode.Node.Labels
		labels := node.Node.Labels
		lenth := len(st.topologyKeys)
		for i, key := range st.topologyKeys {
			tval := tlabels[key]
			if len(tval) == 0 { // node's target label value is empty, skip
				continue
			}
			if tval == labels[key] {
				score += (lenth - i) //  key with more priority at front of which with less priority
				break
			}
		}

		klog.V(5).Infof("node %s get score %d multiple weight %d when schedule %s", node.Name, score, weight, task.Name)
		return float64(score * weight), nil
	}
	ssn.AddNodeOrderFn(PluginName, nodeOrderFn)

	// Register event handlers: when job's first task bind to node, record node name
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			if job == nil {
				return
			}
			if _, ok := st.records[job.UID]; !ok {
				st.records[job.UID] = event.Task.NodeName
				klog.V(5).Infof("add job %s affinity record to node %s in ssesion %v", job.UID, event.Task.NodeName, ssn.UID)
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			if job == nil {
				return
			}
			delete(st.records, job.UID)
			klog.V(5).Infof("clean job %s affinity record to node %s in ssesion %v", job.UID, event.Task.NodeName, ssn.UID)
		},
	})
}

func (st *staticTopAware) OnSessionClose(ssn *framework.Session) {
	st.records = nil
}
