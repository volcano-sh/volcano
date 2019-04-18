/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"context"
	"math/rand"
	"sort"
	"sync"

	"github.com/golang/glog"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// PredicateNodes returns nodes that fit task
func PredicateNodes(task *api.TaskInfo, nodes []*api.NodeInfo, fn api.PredicateFn) []*api.NodeInfo {
	var predicateNodes []*api.NodeInfo

	var workerLock sync.Mutex
	checkNode := func(index int) {
		node := nodes[index]
		glog.V(3).Infof("Considering Task <%v/%v> on node <%v>: <%v> vs. <%v>",
			task.Namespace, task.Name, node.Name, task.Resreq, node.Idle)

		// TODO (k82cn): Enable eCache for performance improvement.
		if err := fn(task, node); err != nil {
			glog.Errorf("Predicates failed for task <%s/%s> on node <%s>: %v",
				task.Namespace, task.Name, node.Name, err)
			return
		}

		workerLock.Lock()
		predicateNodes = append(predicateNodes, node)
		workerLock.Unlock()
	}

	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), checkNode)
	return predicateNodes
}

// PrioritizeNodes returns a map whose key is node's score and value are corresponding nodes
func PrioritizeNodes(task *api.TaskInfo, nodes []*api.NodeInfo, fn api.NodeOrderFn) map[float64][]*api.NodeInfo {
	nodeScores := map[float64][]*api.NodeInfo{}

	var workerLock sync.Mutex
	scoreNode := func(index int) {
		node := nodes[index]
		score, err := fn(task, node)
		if err != nil {
			glog.Errorf("Error in Calculating Priority for the node:%v", err)
			return
		}

		workerLock.Lock()
		nodeScores[score] = append(nodeScores[score], node)
		workerLock.Unlock()
	}
	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), scoreNode)
	return nodeScores
}

// SortNodes returns nodes by order of score
func SortNodes(nodeScores map[float64][]*api.NodeInfo) []*api.NodeInfo {
	var nodesInorder []*api.NodeInfo
	var keys []float64
	for key := range nodeScores {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(keys)))
	for _, key := range keys {
		nodes := nodeScores[key]
		nodesInorder = append(nodesInorder, nodes...)
	}
	return nodesInorder
}

// SelectBestNode returns best node whose score is highest, pick one randomly if there are many nodes with same score.
func SelectBestNode(nodeScores map[float64][]*api.NodeInfo) *api.NodeInfo {
	var bestNodes []*api.NodeInfo
	maxScore := -1.0
	for score, nodes := range nodeScores {
		if score > maxScore {
			maxScore = score
			bestNodes = nodes
		}
	}

	return bestNodes[rand.Intn(len(bestNodes))]
}

// GetNodeList returns values of the map 'nodes'
func GetNodeList(nodes map[string]*api.NodeInfo) []*api.NodeInfo {
	result := make([]*api.NodeInfo, 0, len(nodes))
	for _, v := range nodes {
		result = append(result, v)
	}
	return result
}
