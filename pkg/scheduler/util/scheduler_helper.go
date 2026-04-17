/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added HyperNode support for network topology-aware scheduling
- Added adaptive node selection optimization based on cluster size for improved performance

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
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	baselinePercentageOfNodesToFind = 50

	DefaultComponentName = "vc-scheduler"

	// nodeScoreWorker is the number of parallel workers for node scoring
	nodeScoreWorker = 16
)

var lastProcessedNodeIndex atomic.Int64

// nodeScoreResult holds the per-node scoring output collected by parallel workers.
type nodeScoreResult struct {
	mapScores  map[string]float64
	orderScore float64
	nodeName   string
	err        error
}

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
	nodeScores := map[float64][]*api.NodeInfo{}
	numNodes := len(nodes)

	// Pre-allocate lock-free slices for collecting scores from parallel workers
	results := make([]nodeScoreResult, numNodes)

	// Use WaitGroup to execute BatchNodeOrderFn concurrently with map/reduce operations
	var wg sync.WaitGroup
	var batchNodeScore map[string]float64
	var batchErr error

	// Start BatchNodeOrderFn in a separate goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		batchNodeScore, batchErr = batchFn(task, nodes)
		if batchErr != nil {
			klog.Errorf("Error in Calculating batch Priority for the node, err %v", batchErr)
		}
	}()

	// Execute map phase in parallel using lock-free slice writes
	scoreNode := func(index int) {
		node := nodes[index]
		mapScores, orderScore, err := mapFn(task, node)
		if err != nil {
			klog.Errorf("Error in Calculating Priority for the node:%v", err)
			results[index].err = err
			return
		}
		// Lock-free write to pre-allocated slice at unique index
		results[index].mapScores = mapScores
		results[index].orderScore = orderScore
		results[index].nodeName = node.Name
	}
	workqueue.ParallelizeUntil(context.TODO(), nodeScoreWorker, numNodes, scoreNode)

	// Aggregate map scores into pluginNodeScoreMap after parallel execution
	pluginNodeScoreMap := map[string]fwk.NodeScoreList{}
	nodeOrderScoreMap := map[string]float64{}
	for i := 0; i < numNodes; i++ {
		if results[i].err != nil {
			continue
		}
		for plugin, score := range results[i].mapScores {
			nodeScoreList, ok := pluginNodeScoreMap[plugin]
			if !ok {
				nodeScoreList = make(fwk.NodeScoreList, 0, numNodes)
			}
			hp := fwk.NodeScore{
				Name:  results[i].nodeName,
				Score: int64(math.Floor(score)),
			}
			pluginNodeScoreMap[plugin] = append(nodeScoreList, hp)
		}
		nodeOrderScoreMap[results[i].nodeName] = results[i].orderScore
	}

	// Execute reduce phase
	reduceScores, err := reduceFn(task, pluginNodeScoreMap)
	if err != nil {
		klog.Errorf("Error in Calculating Priority for the node:%v", err)
		return nodeScores
	}

	// Wait for BatchNodeOrderFn to complete
	wg.Wait()

	// Explicitly discard potentially partial results if BatchNodeOrderFn failed
	if batchErr != nil {
		klog.Warningf("BatchNodeOrderFn failed, proceeding with reduce+order scores only: %v", batchErr)
		batchNodeScore = nil
	}

	// Aggregate final scores
	nodeScoreMap := map[string]float64{}
	for _, node := range nodes {
		// If no plugin is applied to this node, the default is 0.0
		score := 0.0
		if reduceScore, ok := reduceScores[node.Name]; ok {
			score += reduceScore
		}
		if orderScore, ok := nodeOrderScoreMap[node.Name]; ok {
			score += orderScore
		}
		if batchNodeScore != nil {
			if batchScore, ok := batchNodeScore[node.Name]; ok {
				score += batchScore
			}
		}
		nodeScores[score] = append(nodeScores[score], node)

		if klog.V(5).Enabled() {
			nodeScoreMap[node.Name] = score
		}
	}

	klog.V(5).Infof("Prioritize nodeScoreMap for task<%s/%s> is: %v", task.Namespace, task.Name, nodeScoreMap)
	return nodeScores
}

// PrioritizeHyperNodes returns a map whose key is hyperNode's score and value are corresponding hyperNodes
func PrioritizeHyperNodes(candidateHyperNodes map[string][]*api.NodeInfo, subJob *api.SubJobInfo, fn api.HyperNodeOrderMapFn) (map[float64][]string, error) {
	hyperNodesScoreMap := make(map[string]float64)
	mapScores, err := fn(subJob, candidateHyperNodes)
	if err != nil {
		return nil, err
	}

	// plugin scores of hyperNode.
	for pluginName, scores := range mapScores {
		for hyperNode, score := range scores {
			klog.V(5).InfoS("Add plugin score at hypeNode", "subJob", subJob.UID, "pluginName", pluginName, "hyperNodeName", hyperNode, "score", score)
			hyperNodesScoreMap[hyperNode] += score
		}
	}

	hyperNodeScores := make(map[float64][]string)
	hyperNodeScoreMap := make(map[string]float64)
	for hyperNodeName := range candidateHyperNodes {
		// If no plugin is applied to this node, the default is 0.0
		score := 0.0
		if value, ok := hyperNodesScoreMap[hyperNodeName]; ok {
			score += value
		}
		hyperNodeScores[score] = append(hyperNodeScores[score], hyperNodeName)

		if klog.V(5).Enabled() {
			hyperNodeScoreMap[hyperNodeName] = score
		}
	}

	klog.V(5).InfoS("Prioritize hyperNode score map for subJob", "subJob", subJob.UID, "scoreMap", hyperNodeScoreMap)
	return hyperNodeScores, nil
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

// SelectBestNodeAndScore returns the best node whose score is highest and the highest score, pick one randomly if there are many nodes with same score.
func SelectBestNodeAndScore(nodeScores map[float64][]*api.NodeInfo) (*api.NodeInfo, float64) {
	var bestNodes []*api.NodeInfo
	var maxScore = math.Inf(-1)
	for score, nodes := range nodeScores {
		if score > maxScore {
			maxScore = score
			bestNodes = nodes
		}
	}

	if len(bestNodes) == 0 {
		return nil, 0
	}

	return bestNodes[rand.Intn(len(bestNodes))], maxScore
}

// SelectBestHyperNodeAndScore return the best hyperNode name whose score is highest, pick one randomly if there are many hyperNodes with same score.
func SelectBestHyperNodeAndScore(hyperNodeScores map[float64][]string) (string, float64) {
	var bestHyperNodes []string
	var maxScore = math.Inf(-1)
	for score, hyperNodes := range hyperNodeScores {
		if score > maxScore {
			maxScore = score
			bestHyperNodes = hyperNodes
		}
	}

	if len(bestHyperNodes) == 0 {
		return "", 0
	}

	return bestHyperNodes[rand.Intn(len(bestHyperNodes))], maxScore
}

// SelectBestNodesAndScores returns the best N node whose score is highest N score, pick one randomly if there are many nodes with same score.
func SelectBestNodesAndScores(nodeScores map[float64][]*api.NodeInfo, count int) ([]*api.NodeInfo, []float64) {
	bestNodes := []*api.NodeInfo{}
	scores := []float64{}
	if count <= 0 || len(nodeScores) == 0 {
		return bestNodes, scores
	}
	allScores := make([]float64, 0, len(nodeScores))
	for score := range nodeScores {
		allScores = append(allScores, score)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(allScores)))

	selecteNodeCount := 0
	for _, score := range allScores {
		nodes := nodeScores[score]
		if len(nodes)+selecteNodeCount > count {
			rand.Shuffle(len(nodes), func(i, j int) {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			})
		}
		for _, node := range nodes {
			bestNodes = append(bestNodes, node)
			scores = append(scores, score)
			selecteNodeCount++
			if len(nodes) == count {
				return nodes, scores
			}
		}
	}
	return bestNodes, scores
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

// GetRealNodesByHyperNode returns values of the map 'hyperNodes'.
func GetRealNodesByHyperNode(hyperNodes map[string]sets.Set[string], allNodes map[string]*api.NodeInfo) (map[string][]*api.NodeInfo, map[string]sets.Set[string]) {
	resultList := make(map[string][]*api.NodeInfo)
	resultSet := make(map[string]sets.Set[string])
	for hyperNodeName, nodes := range hyperNodes {
		resultList[hyperNodeName] = make([]*api.NodeInfo, 0, len(nodes))
		resultSet[hyperNodeName] = make(sets.Set[string], len(nodes))
		for node := range nodes {
			if ni, ok := allNodes[node]; ok {
				resultList[hyperNodeName] = append(resultList[hyperNodeName], ni)
				resultSet[hyperNodeName].Insert(ni.Name)
			}
		}
	}
	return resultList, resultSet
}

// ValidateVictims returns an error if the resources of the victims can't satisfy the preemptor
func ValidateVictims(preemptor *api.TaskInfo, node *api.NodeInfo, victims []*api.TaskInfo) error {
	// Victims should not be judged to be empty here.
	// It is possible to complete the scheduling of the preemptor without evicting the task.
	// In the first round, a large task (CPU: 8) is expelled, and a small task is scheduled (CPU: 2)
	// When the following rounds of victims are empty, it is still allowed to schedule small tasks (CPU: 2)
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

// ConvertRes2ResList convert resource type from api.Resource in scheduler to v1.ResourceList in yaml
func ConvertRes2ResList(res *api.Resource) v1.ResourceList {
	var rl = v1.ResourceList{}
	rl[v1.ResourceCPU] = *resource.NewMilliQuantity(int64(res.MilliCPU), resource.DecimalSI)
	rl[v1.ResourceMemory] = *resource.NewQuantity(int64(res.Memory), resource.BinarySI)
	for resourceName, f := range res.ScalarResources {
		if resourceName == v1.ResourcePods {
			rl[resourceName] = *resource.NewQuantity(int64(f), resource.DecimalSI)
			continue
		}
		rl[resourceName] = *resource.NewMilliQuantity(int64(f), resource.DecimalSI)
	}
	return rl
}

// FindHyperNodeForNode Find the hyperNode to which the node belongs.
func FindHyperNodeForNode(nodeName string, hyperNodes map[string][]*api.NodeInfo, hyperNodesTiers []int, hyperNodesSetByTier map[int]sets.Set[string]) string {
	if len(hyperNodesTiers) == 0 {
		return ""
	}
	nodeTypeHyperNodes := hyperNodesSetByTier[hyperNodesTiers[0]]
	for hyperNode := range nodeTypeHyperNodes {
		nodes := hyperNodes[hyperNode]
		for _, node := range nodes {
			if node.Name == nodeName {
				return hyperNode
			}
		}
	}
	return ""
}

// FindJobTaskNumOfHyperNode find out the number of tasks that belong to the hyperNode.
func FindJobTaskNumOfHyperNode(hyperNodeName string, tasks api.TasksMap, hyperNodes map[string][]*api.NodeInfo) int {
	nodes := hyperNodes[hyperNodeName]
	taskCount := 0
	for _, task := range tasks {
		for _, node := range nodes {
			if node.Name == task.NodeName {
				taskCount++
				break
			}
		}
	}
	return taskCount
}
