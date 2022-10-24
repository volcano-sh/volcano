/*
Copyright 2022 The Kubernetes Authors.

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

package nodeorder

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/selectorspread"
)

func selectorSpread(handle k8sframework.Handle) (*selectorspread.SelectorSpread, error) {
	p, err := selectorspread.New(nil, handle)
	return p.(*selectorspread.SelectorSpread), err
}

func selectorSpreadScore(
	selectorSpread *selectorspread.SelectorSpread,
	cycleState *k8sframework.CycleState,
	pod *v1.Pod,
	nodes []*v1.Node,
	selectorSpreadWeight int,
) (map[string]float64, error) {
	preScoreStatus := selectorSpread.PreScore(context.TODO(), cycleState, pod, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}
	nodeScoreList := make(k8sframework.NodeScoreList, len(nodes))
	// size of errCh should be no less than parallelization number, see interPodAffinityScore.
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodes), func(index int) {
		nodeName := nodes[index].Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := selectorSpread.Score(ctx, cycleState, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate selector spread priority failed %v", status.Message())
			return
		}
		nodeScoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	selectorSpread.NormalizeScore(context.TODO(), cycleState, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodes))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("selector spread returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(selectorSpreadWeight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("selector spread Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}
