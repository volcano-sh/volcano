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
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
)

func interPodAffinity(handle k8sframework.Handle, fts feature.Features) (*interpodaffinity.InterPodAffinity, error) {
	plArgs := &config.InterPodAffinityArgs{}
	p, err := interpodaffinity.New(plArgs, handle, fts)
	return p.(*interpodaffinity.InterPodAffinity), err
}

func interPodAffinityScore(
	interPodAffinity *interpodaffinity.InterPodAffinity,
	state *k8sframework.CycleState,
	pod *v1.Pod,
	nodes []*v1.Node,
	podAffinityWeight int,
) (map[string]float64, error) {
	preScoreStatus := interPodAffinity.PreScore(context.TODO(), state, pod, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodeScoreList := make(k8sframework.NodeScoreList, len(nodes))
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
	defer parallelizeCancel()

	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodes), func(index int) {
		nodeName := nodes[index].Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := interPodAffinity.Score(ctx, state, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate inter pod affinity priority failed %v", status.Message())
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

	interPodAffinity.NormalizeScore(context.TODO(), state, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodes))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("inter pod affinity returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(podAffinityWeight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("inter pod affinity Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}
