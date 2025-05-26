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

package api

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
)

// ClusterInfo is a snapshot of cluster by cache.
type ClusterInfo struct {
	Jobs                      map[JobID]*JobInfo
	Nodes                     map[string]*NodeInfo
	HyperNodes                HyperNodeInfoMap
	HyperNodesSetByTier       map[int]sets.Set[string]
	RealNodesSet              map[string]sets.Set[string]
	HyperNodesReadyToSchedule bool
	Queues                    map[QueueID]*QueueInfo
	NamespaceInfo             map[NamespaceName]*NamespaceInfo
	RevocableNodes            map[string]*NodeInfo
	NodeList                  []string
	CSINodesStatus            map[string]*CSINodeStatusInfo
}

func (ci ClusterInfo) String() string {
	str := "Cache:\n"

	if len(ci.Nodes) != 0 {
		str += "Nodes:\n"
		for _, n := range ci.Nodes {
			str += fmt.Sprintf("\t %s: idle(%v) used(%v) allocatable(%v) pods(%d)\n",
				n.Name, n.Idle, n.Used, n.Allocatable, len(n.Tasks))

			i := 0
			for _, p := range n.Tasks {
				str += fmt.Sprintf("\t\t %d: %v\n", i, p)
				i++
			}
		}
	}

	if len(ci.Jobs) != 0 {
		str += "Jobs:\n"
		for _, job := range ci.Jobs {
			str += fmt.Sprintf("\t Job(%s) name(%s) minAvailable(%v)\n",
				job.UID, job.Name, job.MinAvailable)

			i := 0
			for _, task := range job.Tasks {
				str += fmt.Sprintf("\t\t %d: %v\n", i, task)
				i++
			}
		}
	}

	if len(ci.NamespaceInfo) != 0 {
		str += "Namespaces:\n"
		for _, ns := range ci.NamespaceInfo {
			str += fmt.Sprintf("\t Namespace(%s)\n", ns.Name)
		}
	}

	if len(ci.NodeList) != 0 {
		str += fmt.Sprintf("NodeList: %v\n", ci.NodeList)
	}

	return str
}
