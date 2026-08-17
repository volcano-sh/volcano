/*
Copyright 2026 The Volcano Authors.

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

package hypernode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("HyperNode controller ownership transition", Ordered, func() {
	namespace := os.Getenv("VOLCANO_E2E_NAMESPACE")
	if namespace == "" {
		namespace = "volcano-system"
	}
	releaseName := os.Getenv("VOLCANO_E2E_RELEASE_NAME")
	if releaseName == "" {
		releaseName = "integration"
	}
	profile := os.Getenv("HYPERNODE_E2E_PROFILE")

	BeforeAll(func() {
		if profile != "ownership-transition" {
			Skip("ownership transition runs only in the ownership-transition profile")
		}
		Expect(os.Getenv("VOLCANO_E2E_CHART_PATH")).NotTo(BeEmpty())
		Expect(cleanupDiscoveredTopology()).To(Succeed())
		Eventually(discoveredHyperNodeCount, 2*time.Minute, time.Second).Should(Equal(0))
		Eventually(func() error {
			return expectHyperNodeControllerOwner(namespace, releaseName, "controller-manager")
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	AfterAll(func() {
		if profile != "ownership-transition" {
			return
		}
		// Preserve the failed ownership mode until the e2e harness exports Pod
		// logs. The disposable Kind cluster is removed by the harness afterward.
		if CurrentSpecReport().Failed() {
			return
		}
		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "disabled")).To(Succeed())
		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "controller-manager")).To(Succeed())
		Expect(cleanupDiscoveredTopology()).To(Succeed())
		Eventually(discoveredHyperNodeCount, 2*time.Minute, time.Second).Should(Equal(0))
	})

	It("preserves HyperNodes while moving ownership in both directions", func() {
		Expect(setNodeTopologyLabel("kwok-node-0", "rack-ownership-transition")).To(Succeed())

		var hyperNodeName, hyperNodeUID string
		Eventually(func() error {
			name, uid, err := ownershipTransitionHyperNodeIdentity("rack-ownership-transition", 1)
			if err == nil {
				hyperNodeName, hyperNodeUID = name, uid
			}
			return err
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("Stopping the aggregate owner before starting the standalone owner")
		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "disabled")).To(Succeed())
		Eventually(func() error {
			return expectHyperNodeControllerOwner(namespace, releaseName, "disabled")
		}, 2*time.Minute, time.Second).Should(Succeed())
		Expect(expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 1)).To(Succeed())

		Expect(setNodeTopologyLabel("kwok-node-1", "rack-ownership-transition")).To(Succeed())
		Consistently(func() error {
			return expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 1)
		}, 10*time.Second, time.Second).Should(Succeed(), "disabled mode must not reconcile topology changes")

		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "standalone")).To(Succeed())
		Eventually(func() error {
			return expectHyperNodeControllerOwner(namespace, releaseName, "standalone")
		}, 2*time.Minute, time.Second).Should(Succeed())
		Eventually(func() error {
			return expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 2)
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("Stopping the standalone owner before restoring the aggregate owner")
		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "disabled")).To(Succeed())
		Eventually(func() error {
			return expectHyperNodeControllerOwner(namespace, releaseName, "disabled")
		}, 2*time.Minute, time.Second).Should(Succeed())
		Expect(expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 2)).To(Succeed())

		Expect(setNodeTopologyLabel("kwok-node-1", "")).To(Succeed())
		Consistently(func() error {
			return expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 2)
		}, 10*time.Second, time.Second).Should(Succeed(), "disabled mode must not reconcile topology changes")

		Expect(upgradeHyperNodeControllerMode(namespace, releaseName, "controller-manager")).To(Succeed())
		Eventually(func() error {
			return expectHyperNodeControllerOwner(namespace, releaseName, "controller-manager")
		}, 2*time.Minute, time.Second).Should(Succeed())
		Eventually(func() error {
			return expectMigrationHyperNode(hyperNodeName, hyperNodeUID, 1)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})
})

func upgradeHyperNodeControllerMode(namespace, releaseName, mode string) error {
	chartPath := os.Getenv("VOLCANO_E2E_CHART_PATH")
	args := []string{
		"upgrade", releaseName, chartPath,
		"--namespace", namespace,
		"--reuse-values",
		"--set", "custom.hypernode_controller_mode=" + mode,
		"--wait",
		"--timeout", "5m",
	}
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if mode == "standalone" {
		args = append(args, "--set", "custom.hypernode_controller_replicas=2")
	}
	output, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm upgrade to HyperNode mode %q failed: %w\n%s", mode, err, output)
	}
	return nil
}

func expectHyperNodeControllerOwner(namespace, releaseName, mode string) error {
	controllerDeployment, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
		context.Background(), releaseName+"-controllers", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get aggregate controller Deployment: %w", err)
	}
	if err := expectDeploymentRolloutComplete(controllerDeployment); err != nil {
		return fmt.Errorf("aggregate controller rollout is incomplete: %w", err)
	}
	aggregateDisabled := hyperNodeControllerExcluded(controllerArgs(controllerDeployment))
	aggregatePods, err := e2eutil.KubeClient.CoreV1().Pods(namespace).List(
		context.Background(), metav1.ListOptions{LabelSelector: "app=volcano-controller"})
	if err != nil {
		return fmt.Errorf("list aggregate controller Pods: %w", err)
	}
	if len(aggregatePods.Items) != int(*controllerDeployment.Spec.Replicas) {
		return fmt.Errorf("found %d aggregate controller Pods, want %d", len(aggregatePods.Items), *controllerDeployment.Spec.Replicas)
	}
	for i := range aggregatePods.Items {
		pod := &aggregatePods.Items[i]
		if len(pod.Spec.Containers) == 0 || hyperNodeControllerExcluded(pod.Spec.Containers[0].Args) != aggregateDisabled {
			return fmt.Errorf("aggregate controller Pod %q does not match the current ownership mode", pod.Name)
		}
	}

	standalone, standaloneErr := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
		context.Background(), releaseName+"-hypernode-controller", metav1.GetOptions{})
	standalonePods, err := e2eutil.KubeClient.CoreV1().Pods(namespace).List(
		context.Background(), metav1.ListOptions{LabelSelector: "app=volcano-hypernode-controller"})
	if err != nil {
		return fmt.Errorf("list standalone controller Pods: %w", err)
	}
	switch mode {
	case "controller-manager":
		if aggregateDisabled {
			return fmt.Errorf("aggregate controller still excludes HyperNode")
		}
		if !apierrors.IsNotFound(standaloneErr) {
			return fmt.Errorf("standalone Deployment still exists: %v", standaloneErr)
		}
		if len(standalonePods.Items) != 0 {
			return fmt.Errorf("found %d terminating standalone controller Pods", len(standalonePods.Items))
		}
	case "disabled":
		if !aggregateDisabled {
			return fmt.Errorf("aggregate controller still owns HyperNode")
		}
		if !apierrors.IsNotFound(standaloneErr) {
			return fmt.Errorf("standalone Deployment still exists: %v", standaloneErr)
		}
		if len(standalonePods.Items) != 0 {
			return fmt.Errorf("found %d terminating standalone controller Pods", len(standalonePods.Items))
		}
	case "standalone":
		if !aggregateDisabled {
			return fmt.Errorf("aggregate controller still owns HyperNode")
		}
		if standaloneErr != nil {
			return fmt.Errorf("get standalone controller Deployment: %w", standaloneErr)
		}
		if err := expectDeploymentRolloutComplete(standalone); err != nil {
			return fmt.Errorf("standalone controller rollout is incomplete: %w", err)
		}
		if len(standalonePods.Items) != int(*standalone.Spec.Replicas) {
			return fmt.Errorf("found %d standalone controller Pods, want %d", len(standalonePods.Items), *standalone.Spec.Replicas)
		}
	default:
		return fmt.Errorf("unsupported HyperNode controller mode %q", mode)
	}
	return nil
}

func hyperNodeControllerExcluded(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "-hyperNode-controller") {
			return true
		}
	}
	return false
}

func expectDeploymentRolloutComplete(deployment *appsv1.Deployment) error {
	if deployment.Spec.Replicas == nil {
		return fmt.Errorf("replica count is unset")
	}
	desired := *deployment.Spec.Replicas
	status := deployment.Status
	if status.ObservedGeneration < deployment.Generation ||
		status.UpdatedReplicas != desired ||
		status.ReadyReplicas != desired ||
		status.AvailableReplicas != desired ||
		status.Replicas != desired {
		return fmt.Errorf("generation=%d observed=%d desired=%d replicas=%d updated=%d ready=%d available=%d",
			deployment.Generation, status.ObservedGeneration, desired, status.Replicas,
			status.UpdatedReplicas, status.ReadyReplicas, status.AvailableReplicas)
	}
	return nil
}

func ownershipTransitionHyperNodeIdentity(rack string, nodeCount int64) (string, string, error) {
	hyperNodes, err := discoveredHyperNodes()
	if err != nil {
		return "", "", err
	}
	if len(hyperNodes) != 1 {
		return "", "", fmt.Errorf("found %d discovered HyperNodes, want 1", len(hyperNodes))
	}
	hyperNode := &hyperNodes[0]
	if hyperNode.Labels[testTopologyLabel] != rack {
		return "", "", fmt.Errorf("found rack %q, want %q", hyperNode.Labels[testTopologyLabel], rack)
	}
	if hyperNode.Status.NodeCount != nodeCount {
		return "", "", fmt.Errorf("HyperNode %q nodeCount is %d, want %d", hyperNode.Name, hyperNode.Status.NodeCount, nodeCount)
	}
	return hyperNode.Name, string(hyperNode.UID), nil
}

func expectMigrationHyperNode(name, uid string, nodeCount int64) error {
	hyperNode, err := e2eutil.VcClient.TopologyV1alpha1().HyperNodes().Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(hyperNode.UID) != uid {
		return fmt.Errorf("HyperNode %q UID changed from %q to %q", name, uid, hyperNode.UID)
	}
	if hyperNode.Status.NodeCount != nodeCount {
		return fmt.Errorf("HyperNode %q nodeCount is %d, want %d", name, hyperNode.Status.NodeCount, nodeCount)
	}
	return nil
}
