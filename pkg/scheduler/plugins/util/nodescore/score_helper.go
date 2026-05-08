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
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

type BaseScorePlugin interface {
	k8sframework.PreScorePlugin
	k8sframework.ScorePlugin
}

// EmptyNormalizer is a no-op normalizer that does nothing. For plugins that do not implement the ScoreExtensions interface,
// such as VolumeBinding, ExampleNormalizer can be used, indicating that there is no need to normalize the score
type EmptyNormalizer struct{}

func (e *EmptyNormalizer) NormalizeScore(ctx context.Context, state fwk.CycleState, p *v1.Pod, scores k8sframework.NodeScoreList) *fwk.Status {
	return nil
}

func CalculatePluginScore(
	pluginName string,
	plugin BaseScorePlugin,
	normalizer k8sframework.ScoreExtensions,
	cycleState fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
	weight int,
) (map[string]float64, error) {
	preScoreStatus := plugin.PreScore(context.TODO(), cycleState, pod, nodeInfos)
	if preScoreStatus.IsSkip() {
		// Skip Score
		return map[string]float64{}, nil
	}
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodeScoreList := make(k8sframework.NodeScoreList, len(nodeInfos))
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
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		nodeInfo := nodeInfos[index]
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := plugin.Score(ctx, cycleState, pod, nodeInfo)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate %s priority failed %v", pluginName, status.Message())
			return
		}
		nodeScoreList[index] = k8sframework.NodeScore{
			Name:  nodeInfo.Node().Name,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	normalizer.NormalizeScore(context.TODO(), cycleState, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodeInfos))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("%s returns an invalid score %v for node %s", pluginName, nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(weight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("%s Score for task %s/%s is: %v", pluginName, pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}
