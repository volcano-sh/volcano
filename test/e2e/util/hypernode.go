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

package util

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

// SetupHyperNode creates a hypernode with the given configuration
func SetupHyperNode(ctx *TestContext, spec *topologyv1alpha1.HyperNode) error {
	hyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name,
		},
		Spec: spec.Spec,
	}

	_, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create hypernode %s: %v", spec.Name, err)
	}

	return nil
}

// VerifyPodScheduling verifies if pods are scheduled according to topology requirements
func VerifyPodScheduling(ctx *TestContext, job *batchv1alpha1.Job, expectedNodes []string) error {
	// Get all pods for the job
	pods := GetTasksOfJob(ctx, job)
	if len(pods) == 0 {
		return fmt.Errorf("no pods found for job %s", job.Name)
	}

	// Create a map for quick lookup of expected nodes
	nodeSet := make(map[string]bool)
	for _, node := range expectedNodes {
		nodeSet[node] = true
	}

	// Verify each pod is scheduled to expected nodes
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			return fmt.Errorf("pod %s/%s is not scheduled", pod.Namespace, pod.Name)
		}
		if !nodeSet[pod.Spec.NodeName] {
			return fmt.Errorf("pod %s/%s is scheduled to unexpected node %s, expected nodes: %v",
				pod.Namespace, pod.Name, pod.Spec.NodeName, expectedNodes)
		}
	}

	return nil
}

// CleanupHyperNodes deletes all hypernode resources in the cluster
func CleanupHyperNodes(ctx *TestContext) error {
	err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to delete hypernodes: %v", err)
	}
	return nil
}
