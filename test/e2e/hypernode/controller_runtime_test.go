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
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	testTopologyLabel = "volcano.sh/e2e-hypernode-rack"
	sourceLabel       = "volcano.sh/network-topology-source"
	labelSource       = "label"
)

var _ = Describe("HyperNode controller runtime", Ordered, func() {
	mode := os.Getenv("HYPERNODE_CONTROLLER_MODE")
	if mode == "" {
		mode = "controller-manager"
	}
	namespace := os.Getenv("VOLCANO_E2E_NAMESPACE")
	if namespace == "" {
		namespace = "volcano-system"
	}
	releaseName := os.Getenv("VOLCANO_E2E_RELEASE_NAME")
	if releaseName == "" {
		releaseName = "integration"
	}

	BeforeAll(func() {
		Expect(cleanupDiscoveredTopology()).To(Succeed())
		Eventually(discoveredHyperNodeCount, 2*time.Minute, time.Second).Should(Equal(0))
	})

	AfterAll(func() {
		Expect(cleanupDiscoveredTopology()).To(Succeed())
		Eventually(discoveredHyperNodeCount, 2*time.Minute, time.Second).Should(Equal(0))
	})

	It("deploys exactly one HyperNode controller owner", func() {
		controllerDeployment, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
			context.Background(), releaseName+"-controllers", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		standaloneName := releaseName + "-hypernode-controller"
		standaloneDeployment, standaloneErr := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
			context.Background(), standaloneName, metav1.GetOptions{})
		if mode == "standalone" {
			Expect(standaloneErr).NotTo(HaveOccurred())
			Expect(controllerArgs(controllerDeployment)).To(ContainElement(ContainSubstring("-hyperNode-controller")))
			verifyStandaloneDeployment(standaloneDeployment, releaseName)
			Eventually(func() (int32, error) {
				deployment, getErr := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
					context.Background(), standaloneName, metav1.GetOptions{})
				if getErr != nil {
					return 0, getErr
				}
				return deployment.Status.ReadyReplicas, nil
			}, 2*time.Minute, time.Second).Should(Equal(int32(2)))
			return
		}

		Expect(apierrors.IsNotFound(standaloneErr)).To(BeTrue(), "standalone Deployment must not exist in controller-manager mode")
		Expect(controllerArgs(controllerDeployment)).NotTo(ContainElement(ContainSubstring("-hyperNode-controller")))
	})

	It("discovers, updates, and removes label topology while reconciling status", func() {
		Expect(setNodeTopologyLabel("kwok-node-0", "rack-a")).To(Succeed())
		Expect(setNodeTopologyLabel("kwok-node-1", "rack-a")).To(Succeed())
		Eventually(func() error {
			return expectDiscoveredTopology(map[string][]string{
				"rack-a": {"kwok-node-0", "kwok-node-1"},
			})
		}, 2*time.Minute, time.Second).Should(Succeed())

		Expect(setNodeTopologyLabel("kwok-node-1", "rack-b")).To(Succeed())
		Eventually(func() error {
			return expectDiscoveredTopology(map[string][]string{
				"rack-a": {"kwok-node-0"},
				"rack-b": {"kwok-node-1"},
			})
		}, 2*time.Minute, time.Second).Should(Succeed())

		Expect(setNodeTopologyLabel("kwok-node-0", "")).To(Succeed())
		Expect(setNodeTopologyLabel("kwok-node-1", "")).To(Succeed())
		Eventually(discoveredHyperNodeCount, 2*time.Minute, time.Second).Should(Equal(0))
	})

	It("uses a topology-scoped standalone service account", func() {
		if mode != "standalone" {
			Skip("dedicated RBAC applies only to standalone mode")
		}

		serviceAccountUser := fmt.Sprintf("system:serviceaccount:%s:%s-hypernode-controller", namespace, releaseName)
		for _, access := range []struct {
			name      string
			verb      string
			group     string
			resource  string
			namespace string
			allowed   bool
		}{
			{name: "list Nodes", verb: "list", resource: "nodes", allowed: true},
			{name: "write HyperNodes", verb: "create", group: "topology.volcano.sh", resource: "hypernodes", allowed: true},
			{name: "update HyperNode status", verb: "update", group: "topology.volcano.sh", resource: "hypernodes/status", allowed: true},
			{name: "watch controller ConfigMap", verb: "watch", resource: "configmaps", namespace: namespace, allowed: true},
			{name: "read UFM Secret", verb: "get", resource: "secrets", namespace: namespace, allowed: true},
			{name: "deny cross-namespace Secret access", verb: "get", resource: "secrets", namespace: "default", allowed: false},
			{name: "create leader Lease", verb: "create", group: "coordination.k8s.io", resource: "leases", namespace: namespace, allowed: true},
			{name: "deny unrelated Pods", verb: "list", resource: "pods", namespace: namespace, allowed: false},
		} {
			By("Checking standalone RBAC can " + access.name)
			allowed, reason, err := subjectCan(serviceAccountUser, access.verb, access.group, access.resource, access.namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(allowed).To(Equal(access.allowed), "authorization reason: %s", reason)
		}
	})

	It("fails over and continues reconciliation", func() {
		if mode != "standalone" {
			Skip("leader failover applies only to standalone mode")
		}

		leaseName := releaseName + "-hypernode-controller-manager"
		var previousHolder string
		Eventually(func() string {
			lease, err := e2eutil.KubeClient.CoordinationV1().Leases(namespace).Get(
				context.Background(), leaseName, metav1.GetOptions{})
			if err != nil || lease.Spec.HolderIdentity == nil {
				return ""
			}
			previousHolder = *lease.Spec.HolderIdentity
			return previousHolder
		}, time.Minute, time.Second).ShouldNot(BeEmpty())

		leaderPod := strings.SplitN(previousHolder, "_", 2)[0]
		Expect(e2eutil.KubeClient.CoreV1().Pods(namespace).Delete(
			context.Background(), leaderPod, metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() string {
			lease, err := e2eutil.KubeClient.CoordinationV1().Leases(namespace).Get(
				context.Background(), leaseName, metav1.GetOptions{})
			if err != nil || lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == previousHolder {
				return ""
			}
			return *lease.Spec.HolderIdentity
		}, 2*time.Minute, time.Second).ShouldNot(BeEmpty())
		Eventually(func() (int32, error) {
			deployment, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
				context.Background(), releaseName+"-hypernode-controller", metav1.GetOptions{})
			if err != nil {
				return 0, err
			}
			return deployment.Status.ReadyReplicas, nil
		}, 2*time.Minute, time.Second).Should(Equal(int32(2)))

		Expect(setNodeTopologyLabel("kwok-node-2", "rack-after-failover")).To(Succeed())
		Eventually(func() error {
			return expectDiscoveredTopology(map[string][]string{
				"rack-after-failover": {"kwok-node-2"},
			})
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

})

func controllerArgs(deployment *appsv1.Deployment) []string {
	if deployment == nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	return deployment.Spec.Template.Spec.Containers[0].Args
}

func verifyStandaloneDeployment(deployment *appsv1.Deployment, releaseName string) {
	Expect(deployment).NotTo(BeNil())
	Expect(deployment.Spec.Replicas).NotTo(BeNil())
	Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
	Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(releaseName + "-hypernode-controller"))
	Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	container := deployment.Spec.Template.Spec.Containers[0]
	Expect(container.Image).To(ContainSubstring("vc-hypernode-controller-manager:"))
	Expect(container.ReadinessProbe).NotTo(BeNil())
	Expect(container.ReadinessProbe.HTTPGet).NotTo(BeNil())
	Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
	Expect(container.LivenessProbe).NotTo(BeNil())
	Expect(container.LivenessProbe.HTTPGet).NotTo(BeNil())
	Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
}

func subjectCan(user, verb, group, resource, namespace string) (bool, string, error) {
	review, err := e2eutil.KubeClient.AuthorizationV1().SubjectAccessReviews().Create(
		context.Background(),
		&authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User: user,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb: verb, Group: group, Resource: resource, Namespace: namespace,
				},
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return false, "", err
	}
	return review.Status.Allowed, review.Status.Reason, nil
}

func setNodeTopologyLabel(name, value string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		node = node.DeepCopy()
		if value == "" {
			delete(node.Labels, testTopologyLabel)
		} else {
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			node.Labels[testTopologyLabel] = value
		}
		_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
		return err
	})
}

func discoveredHyperNodes() ([]topologyv1alpha1.HyperNode, error) {
	list, err := e2eutil.VcClient.TopologyV1alpha1().HyperNodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: sourceLabel + "=" + labelSource,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func discoveredHyperNodeCount() int {
	hyperNodes, err := discoveredHyperNodes()
	if err != nil {
		return -1
	}
	return len(hyperNodes)
}

func expectDiscoveredTopology(expected map[string][]string) error {
	hyperNodes, err := discoveredHyperNodes()
	if err != nil {
		return err
	}
	if len(hyperNodes) != len(expected) {
		return fmt.Errorf("found %d discovered HyperNodes, want %d", len(hyperNodes), len(expected))
	}
	for i := range hyperNodes {
		hyperNode := &hyperNodes[i]
		rack := hyperNode.Labels[testTopologyLabel]
		expectedMembers, exists := expected[rack]
		if !exists {
			return fmt.Errorf("unexpected discovered rack %q", rack)
		}
		if hyperNode.Spec.Tier != 1 || hyperNode.Spec.TierName != testTopologyLabel {
			return fmt.Errorf("rack %q has tier %d and tierName %q", rack, hyperNode.Spec.Tier, hyperNode.Spec.TierName)
		}
		actualMembers := make([]string, 0, len(hyperNode.Spec.Members))
		for _, member := range hyperNode.Spec.Members {
			if member.Type != topologyv1alpha1.MemberTypeNode || member.Selector.ExactMatch == nil {
				return fmt.Errorf("rack %q contains a non-exact Node member", rack)
			}
			actualMembers = append(actualMembers, member.Selector.ExactMatch.Name)
		}
		sort.Strings(actualMembers)
		sort.Strings(expectedMembers)
		if strings.Join(actualMembers, ",") != strings.Join(expectedMembers, ",") {
			return fmt.Errorf("rack %q members are %v, want %v", rack, actualMembers, expectedMembers)
		}
		if hyperNode.Status.NodeCount != int64(len(expectedMembers)) {
			return fmt.Errorf("rack %q nodeCount is %d, want %d", rack, hyperNode.Status.NodeCount, len(expectedMembers))
		}
	}
	return nil
}

func cleanupDiscoveredTopology() error {
	nodes, err := e2eutil.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: testTopologyLabel,
	})
	if err != nil {
		return err
	}
	for i := range nodes.Items {
		if err := setNodeTopologyLabel(nodes.Items[i].Name, ""); err != nil {
			return err
		}
	}
	return nil
}
