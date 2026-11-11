/*
Copyright 2025 The Volcano Authors.

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

package nodescore

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type BaseScorePlugin interface {
	fwk.ScorePlugin
}

// PreScorePluginEntry contains the registered name and PreScore implementation consumed by RunScorePlugins.
type PreScorePluginEntry struct {
	Name   string
	Plugin fwk.PreScorePlugin
}

// ScorePluginEntry contains the registered name, Score implementation, and weight consumed by RunScorePlugins.
type ScorePluginEntry struct {
	Name   string
	Plugin BaseScorePlugin
	Weight int
}

type activeScorePlugin struct {
	entry  ScorePluginEntry
	scores fwk.NodeScoreList
}

func NodeInfosForCandidateNodes(nodes []*api.NodeInfo, nodeMap map[string]fwk.NodeInfo) []fwk.NodeInfo {
	nodeInfos := make([]fwk.NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if nodeInfo, ok := nodeMap[node.Name]; ok {
			nodeInfos = append(nodeInfos, nodeInfo)
		}
	}
	return nodeInfos
}

// RunScorePlugins runs PreScore plugins in order, then runs all active Score plugins
// with a single parallel node traversal.
func RunScorePlugins(
	preScorePluginEntries []PreScorePluginEntry,
	scorePluginEntries []ScorePluginEntry,
	cycleState fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
) (map[string]float64, error) {
	// the default parallelization worker number is 16.
	// the whole scoring will fail if one of the processes failed.
	// so just create a parallelizeContext to control the whole ParallelizeUntil process.
	// if the parallelizeCancel is invoked, the whole "ParallelizeUntil" goes to the end.
	// this could avoid extra computation, especially in huge cluster.
	// and the ParallelizeUntil guarantees only "workerNum" goroutines will be working simultaneously.
	// so it's enough to allocate workerNum size for errCh.
	// note that, in such case, size of errCh should be no less than parallelization number
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.Background())
	defer parallelizeCancel()

	skippedScorePlugins := make(map[string]struct{}, len(preScorePluginEntries))
	for _, entry := range preScorePluginEntries {
		status := entry.Plugin.PreScore(parallelizeContext, cycleState, pod, nodeInfos)
		if status.IsSkip() {
			skippedScorePlugins[entry.Name] = struct{}{}
			continue
		}
		if !status.IsSuccess() {
			return nil, fmt.Errorf("running PreScore plugin %q failed: %w", entry.Name, status.AsError())
		}
	}
	activePlugins := make([]activeScorePlugin, 0, len(scorePluginEntries))
	for _, entry := range scorePluginEntries {
		if _, skipped := skippedScorePlugins[entry.Name]; skipped {
			continue
		}
		activePlugins = append(activePlugins, activeScorePlugin{
			entry:  entry,
			scores: make(fwk.NodeScoreList, len(nodeInfos)),
		})
	}

	if len(activePlugins) == 0 {
		return map[string]float64{}, nil
	}

	// Score nodes in parallel. Each worker owns one node index and runs every
	// active plugin for that node.
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		nodeInfo := nodeInfos[index]
		nodeName := nodeInfo.Node().Name
		for pluginIndex := range activePlugins {
			plugin := &activePlugins[pluginIndex]
			score, status := plugin.entry.Plugin.Score(parallelizeContext, cycleState, pod, nodeInfo)
			if !status.IsSuccess() {
				parallelizeCancel()
				errCh <- fmt.Errorf("running Score plugin %q for node %q failed: %s", plugin.entry.Name, nodeName, status.Message())
				return
			}
			plugin.scores[index] = fwk.NodeScore{Name: nodeName, Score: score}
		}
	})
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	// Each normalizer owns one score list, so plugins can normalize in parallel.
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(activePlugins), func(index int) {
		plugin := &activePlugins[index]
		extensions := plugin.entry.Plugin.ScoreExtensions()
		if extensions == nil {
			return
		}
		status := extensions.NormalizeScore(parallelizeContext, cycleState, pod, plugin.scores)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("running NormalizeScore plugin %q failed: %s", plugin.entry.Name, status.Message())
		}
	})
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	// Validate normalized scores and aggregate weighted totals by node index.
	nodeTotalScores := make([]float64, len(nodeInfos))
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		var total float64
		for pluginIndex := range activePlugins {
			plugin := &activePlugins[pluginIndex]
			nodeScore := plugin.scores[index]
			if nodeScore.Score > fwk.MaxNodeScore || nodeScore.Score < fwk.MinNodeScore {
				parallelizeCancel()
				errCh <- fmt.Errorf("plugin %q returns an invalid score %v for node %q", plugin.entry.Name, nodeScore.Score, nodeScore.Name)
				return
			}
			weightedScore := nodeScore.Score * int64(plugin.entry.Weight)
			plugin.scores[index].Score = weightedScore
			total += float64(weightedScore)
		}
		nodeTotalScores[index] = total
	})
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	if logger := klog.V(4); logger.Enabled() {
		for index := range activePlugins {
			plugin := &activePlugins[index]
			logger.Infof("%s Score for task %s/%s is: %v", plugin.entry.Name, pod.Namespace, pod.Name, plugin.scores)
		}
	}

	nodeScores := make(map[string]float64, len(nodeInfos))
	for index, nodeInfo := range nodeInfos {
		nodeScores[nodeInfo.Node().Name] = nodeTotalScores[index]
	}
	return nodeScores, nil
}
