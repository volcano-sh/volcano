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
	"fmt"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"math"
	"math/rand"
	"sort"
	"sync"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const baselinePercentageOfNodesToFind = 50

var lastProcessedNodeIndex int

// CalculateNumOfFeasibleNodesToFind returns the number of feasible nodes that once found,
// the scheduler stops its search for more feasible nodes.
func CalculateNumOfFeasibleNodesToFind(numAllNodes int32) (numNodes int32) {
	opts := options.ServerOpts
	if numAllNodes <= opts.MinNodesToFind || opts.PercentageOfNodesToFind >= 100 {
		return numAllNodes
	}

	adaptivePercentage := opts.PercentageOfNodesToFind
	if adaptivePercentage <= 0 {
		adaptivePercentage = baselinePercentageOfNodesToFind - numAllNodes/125
		if adaptivePercentage < opts.MinPercentageOfNodesToFind {
			adaptivePercentage = opts.MinPercentageOfNodesToFind
		}
	}

	numNodes = numAllNodes * adaptivePercentage / 100
	if numNodes < opts.MinNodesToFind {
		numNodes = opts.MinNodesToFind
	}
	return numNodes
}

// PrioritizeNodes returns a map whose key is node's score and value are corresponding nodes
func PrioritizeNodes(task *api.TaskInfo, nodes []*api.NodeInfo, batchFn api.BatchNodeOrderFn, mapFn api.NodeOrderMapFn, reduceFn api.NodeOrderReduceFn) map[float64][]*api.NodeInfo {
	pluginNodeScoreMap := map[string]k8sframework.NodeScoreList{}
	nodeOrderScoreMap := map[string]float64{}
	nodeScores := map[float64][]*api.NodeInfo{}
	var workerLock sync.Mutex
	scoreNode := func(index int) {
		node := nodes[index]
		mapScores, orderScore, err := mapFn(task, node)
		if err != nil {
			klog.Errorf("Error in Calculating Priority for the node:%v", err)
			return
		}

		workerLock.Lock()
		for plugin, score := range mapScores {
			nodeScoreMap, ok := pluginNodeScoreMap[plugin]
			if !ok {
				nodeScoreMap = k8sframework.NodeScoreList{}
			}
			hp := k8sframework.NodeScore{}
			hp.Name = node.Name
			hp.Score = int64(math.Floor(score))
			pluginNodeScoreMap[plugin] = append(nodeScoreMap, hp)
		}
		nodeOrderScoreMap[node.Name] = orderScore
		workerLock.Unlock()
	}
	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), scoreNode)
	reduceScores, err := reduceFn(task, pluginNodeScoreMap)
	if err != nil {
		klog.Errorf("Error in Calculating Priority for the node:%v", err)
		return nodeScores
	}

	batchNodeScore, err := batchFn(task, nodes)
	if err != nil {
		klog.Errorf("Error in Calculating batch Priority for the node, err %v", err)
		return nodeScores
	}

	for _, node := range nodes {
		if score, found := reduceScores[node.Name]; found {
			if orderScore, ok := nodeOrderScoreMap[node.Name]; ok {
				score += orderScore
			}
			if batchScore, ok := batchNodeScore[node.Name]; ok {
				score += batchScore
			}
			nodeScores[score] = append(nodeScores[score], node)
		} else {
			// If no plugin is applied to this node, the default is 0.0
			score = 0.0
			if orderScore, ok := nodeOrderScoreMap[node.Name]; ok {
				score += orderScore
			}
			if batchScore, ok := batchNodeScore[node.Name]; ok {
				score += batchScore
			}
			nodeScores[score] = append(nodeScores[score], node)
		}
	}
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

	if len(bestNodes) == 0 {
		return nil
	}

	return bestNodes[rand.Intn(len(bestNodes))]
}

// GetNodeList returns values of the map 'nodes'
func GetNodeList(nodes map[string]*api.NodeInfo, nodeList []string) []*api.NodeInfo {
	result := make([]*api.NodeInfo, 0, len(nodeList))
	for _, nodename := range nodeList {
		if ni, ok := nodes[nodename]; ok {
			result = append(result, ni)
		}
	}
	return result
}

// ValidateVictims returns an error if the resources of the victims can't satisfy the preemptor
func ValidateVictims(preemptor *api.TaskInfo, node *api.NodeInfo, victims []*api.TaskInfo) error {
	if len(victims) == 0 {
		return fmt.Errorf("no victims")
	}
	futureIdle := node.FutureIdle()
	for _, victim := range victims {
		futureIdle.Add(victim.Resreq)
	}
	// Every resource of the preemptor needs to be less or equal than corresponding
	// idle resource after preemption.
	if !preemptor.InitResreq.LessEqual(futureIdle, api.Zero) {
		return fmt.Errorf("not enough resources: requested <%v>, but future idle <%v>",
			preemptor.InitResreq, futureIdle)
	}
	return nil
}

// GetMinInt return minimum int from vals
func GetMinInt(vals ...int) int {
	if len(vals) == 0 {
		return 0
	}

	min := vals[0]
	for _, val := range vals {
		if val <= min {
			min = val
		}
	}
	return min
}
