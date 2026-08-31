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
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/parallelize"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type BaseScorePlugin interface {
	fwk.ScorePlugin
}

// PreScorePluginSpec identifies a registered PreScore plugin by its logical name.
type PreScorePluginSpec struct {
	Name   string
	Plugin fwk.PreScorePlugin
}

// ScorePluginSpec identifies a registered Score plugin and its configured weight.
type ScorePluginSpec struct {
	Name   string
	Plugin BaseScorePlugin
	Weight int
}

const scoreWorkerNum = parallelize.DefaultParallelism

type activeScorePlugin struct {
	spec   ScorePluginSpec
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
	ctx context.Context,
	preScorePlugins []PreScorePluginSpec,
	scorePlugins []ScorePluginSpec,
	cycleState fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
) (map[string]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	skippedScorePlugins := make(map[string]struct{}, len(preScorePlugins))
	for _, spec := range preScorePlugins {
		status := spec.Plugin.PreScore(ctx, cycleState, pod, nodeInfos)
		if status.IsSkip() {
			skippedScorePlugins[spec.Name] = struct{}{}
			continue
		}
		if !status.IsSuccess() {
			return nil, fmt.Errorf("running PreScore plugin %q failed: %w", spec.Name, status.AsError())
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	activePlugins := make([]activeScorePlugin, 0, len(scorePlugins))
	for _, spec := range scorePlugins {
		if _, skipped := skippedScorePlugins[spec.Name]; skipped {
			continue
		}
		activePlugins = append(activePlugins, activeScorePlugin{
			spec:   spec,
			scores: make(fwk.NodeScoreList, len(nodeInfos)),
		})
	}

	if len(activePlugins) == 0 {
		return map[string]float64{}, nil
	}

	parallelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := parallelize.NewResultChannel[error]()

	// Score nodes in parallel. Each worker owns one node index and runs every
	// active plugin for that node.
	workqueue.ParallelizeUntil(parallelCtx, scoreWorkerNum, len(nodeInfos), func(index int) {
		nodeInfo := nodeInfos[index]
		nodeName := nodeInfo.Node().Name
		for pluginIndex := range activePlugins {
			plugin := &activePlugins[pluginIndex]
			score, status := plugin.spec.Plugin.Score(parallelCtx, cycleState, pod, nodeInfo)
			if !status.IsSuccess() {
				errCh.SendWithCancel(
					fmt.Errorf("running Score plugin %q for node %q failed: %s", plugin.spec.Name, nodeName, status.Message()),
					cancel,
				)
				return
			}
			plugin.scores[index] = fwk.NodeScore{Name: nodeName, Score: score}
		}
	})
	if err := errCh.Receive(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Each normalizer owns one score list, so plugins can normalize in parallel.
	workqueue.ParallelizeUntil(parallelCtx, scoreWorkerNum, len(activePlugins), func(index int) {
		plugin := &activePlugins[index]
		extensions := plugin.spec.Plugin.ScoreExtensions()
		if extensions == nil {
			return
		}
		status := extensions.NormalizeScore(parallelCtx, cycleState, pod, plugin.scores)
		if !status.IsSuccess() {
			errCh.SendWithCancel(
				fmt.Errorf("running NormalizeScore plugin %q failed: %s", plugin.spec.Name, status.Message()),
				cancel,
			)
		}
	})
	if err := errCh.Receive(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate normalized scores and aggregate weighted totals by node index.
	nodeTotalScores := make([]float64, len(nodeInfos))
	workqueue.ParallelizeUntil(parallelCtx, scoreWorkerNum, len(nodeInfos), func(index int) {
		var total float64
		for pluginIndex := range activePlugins {
			plugin := &activePlugins[pluginIndex]
			nodeScore := plugin.scores[index]
			if nodeScore.Score > fwk.MaxNodeScore || nodeScore.Score < fwk.MinNodeScore {
				errCh.SendWithCancel(
					fmt.Errorf("plugin %q returns an invalid score %v for node %q", plugin.spec.Name, nodeScore.Score, nodeScore.Name),
					cancel,
				)
				return
			}
			total += float64(nodeScore.Score * int64(plugin.spec.Weight))
		}
		nodeTotalScores[index] = total
	})
	if err := errCh.Receive(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	nodeScores := make(map[string]float64, len(nodeInfos))
	for index, nodeInfo := range nodeInfos {
		nodeScores[nodeInfo.Node().Name] = nodeTotalScores[index]
	}
	return nodeScores, nil
}
