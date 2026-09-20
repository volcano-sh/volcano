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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

// groupTopologySchedulerConfig is the supported phase-1 configuration: group
// topology affinity is evaluated together with network topology awareness, and
// backfill is deliberately omitted until it can enforce the same constraints.
const groupTopologySchedulerConfig = `actions: "enqueue, allocate"
tiers:
- plugins:
  - name: priority
  - name: gang
    enablePreemptable: false
  - name: conformance
  - name: sla
- plugins:
  - name: overcommit
  - name: drf
    enablePreemptable: false
  - name: predicates
    arguments:
      predicate.DynamicResourceAllocationEnable: true
  - name: proportion
  - name: nodeorder
  - name: binpack
  - name: network-topology-aware
  - name: group-topology-affinity
    arguments:
      weight: 100
`

type podGroupAntiAffinityTopology struct {
	rackA string
	rackB string
	root  string
}

var _ = Describe("PodGroup Topology Anti-Affinity", Ordered, ContinueOnFailure, Serial, Label("podgroup-anti-affinity"), func() {
	var (
		testCtx        *e2eutil.TestContext
		configMapCase  *e2eutil.ConfigMapCase
		topology       podGroupAntiAffinityTopology
		peerNamespaces []string
	)

	BeforeAll(func() {
		configMapCase = e2eutil.NewConfigMapCase(volcanoSystemNamespace(), volcanoSchedulerConfigMapName())
		Expect(configMapCase.ChangeBy(replaceSchedulerConfig(groupTopologySchedulerConfig))).To(Succeed())
		restartVolcanoScheduler()
	})

	BeforeEach(func() {
		peerNamespaces = nil
		testCtx = e2eutil.InitTestContext(e2eutil.Options{NodesNumLimit: 8})
		topology = setupPodGroupAntiAffinityTopology(testCtx, "pg-aa-"+testCtx.Namespace)

		// Do not start a case until the scheduler has observed both the test
		// configuration and the newly-created HyperNode tree.
		probePG := createTopologyPodGroup(testCtx, testCtx.Namespace, "topology-ready-probe", map[string]string{"e2e-probe": "true"}, nil, nil, 1)
		probePod := createPodGroupPod(testCtx, testCtx.Namespace, "topology-ready-probe", probePG.Name, "kwok-node-7")
		Expect(e2eutil.WaitPodReady(testCtx, probePod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, probePG.Name, topology.rackB)).To(Succeed())
	})

	AfterEach(func() {
		for _, namespace := range peerNamespaces {
			foreground := metav1.DeletePropagationForeground
			Expect(testCtx.Kubeclient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})).To(Succeed())
			Expect(wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, e2eutil.FiveMinute, false,
				e2eutil.NamespaceNotExistWithName(testCtx, namespace))).To(Succeed())
		}
		if testCtx != nil {
			e2eutil.CleanupTestContext(testCtx)
			Expect(waitForNoHyperNodes(testCtx)).To(Succeed())
		}
	})

	AfterAll(func() {
		if configMapCase != nil {
			Expect(configMapCase.UndoChanged()).To(Succeed())
			restartVolcanoScheduler()
		}
	})

	It("enforces required anti-affinity in the same namespace", Label("normal-path"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "required-anchor", map[string]string{"workload": "required"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "required-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "required"}, nil)
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "required-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, nil, 1)
		challengerPod := createPodGroupPod(testCtx, testCtx.Namespace, "required-challenger-pod", challengerPG.Name, "")

		Expect(e2eutil.WaitPodReady(testCtx, challengerPod)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(BeElementOf("kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"))
	})

	It("enforces required anti-affinity across namespaces together with hard NTA after scheduler restart", Label("normal-path", "network-topology-combination"), func() {
		peerNamespace := createPeerNamespace(testCtx, "all", nil)
		peerNamespaces = append(peerNamespaces, peerNamespace)

		anchorPG := createTopologyPodGroup(testCtx, peerNamespace, "anchor", map[string]string{"workload": "anchor"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, peerNamespace, "anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, peerNamespace, anchorPG.Name, topology.rackA)).To(Succeed())

		restartVolcanoScheduler()

		term := requiredPodGroupAntiAffinityTerm(
			map[string]string{"workload": "anchor"},
			&metav1.LabelSelector{}, // an empty, non-nil selector selects all namespaces
		)
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{
			Mode:               schedulingv1beta1.HardNetworkTopologyMode,
			HighestTierAllowed: ptr.To(1),
		}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}},
			networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "challenger-a", challengerPG.Name, "")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "challenger-b", challengerPG.Name, "")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA, challengerPodB}, rackBNodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, challengerPG.Name, topology.rackB)).To(Succeed())
	})

	It("uses preferred anti-affinity to choose an unoccupied hard-NTA domain", Label("normal-path", "network-topology-combination"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "preferred-anchor", map[string]string{"workload": "preferred-anchor"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "preferred-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "preferred-anchor"}, nil)
		term.Weight = 100
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{
			Mode:               schedulingv1beta1.HardNetworkTopologyMode,
			HighestTierAllowed: ptr.To(1),
		}
		preferredPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "preferred", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Preferred: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		preferredPodA := createPodGroupPod(testCtx, testCtx.Namespace, "preferred-a", preferredPG.Name, "")
		preferredPodB := createPodGroupPod(testCtx, testCtx.Namespace, "preferred-b", preferredPG.Name, "")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{preferredPodA, preferredPodB}, rackBNodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, preferredPG.Name, topology.rackB)).To(Succeed())
	})

	It("keeps preferred anti-affinity and soft NTA non-blocking when only an occupied domain is allowed", Label("normal-path", "network-topology-combination"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "soft-anchor", map[string]string{"workload": "soft-anchor"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "soft-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "soft-anchor"}, nil)
		term.Weight = 100
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{Mode: schedulingv1beta1.SoftNetworkTopologyMode}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "soft-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Preferred: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "soft-challenger-a", challengerPG.Name, "kwok-node-1")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "soft-challenger-b", challengerPG.Name, "kwok-node-2")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA}, []string{"kwok-node-1"})
		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodB}, []string{"kwok-node-2"})
	})

	It("lets hard NTA place a gang when nil Namespace selector ignores another namespace", Label("normal-path", "network-topology-combination"), func() {
		peerNamespace := createPeerNamespace(testCtx, "default-scope", nil)
		peerNamespaces = append(peerNamespaces, peerNamespace)

		anchorPG := createTopologyPodGroup(testCtx, peerNamespace, "other-namespace-anchor", map[string]string{"workload": "namespace-default"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, peerNamespace, "other-namespace-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, peerNamespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "namespace-default"}, nil)
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{
			Mode:               schedulingv1beta1.HardNetworkTopologyMode,
			HighestTierAllowed: ptr.To(1),
		}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "namespace-default-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "namespace-default-challenger-a", challengerPG.Name, "kwok-node-1")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "namespace-default-challenger-b", challengerPG.Name, "kwok-node-2")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA, challengerPodB}, rackANodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, challengerPG.Name, topology.rackA)).To(Succeed())
	})

	It("applies required anti-affinity while soft NTA remains non-blocking", Label("normal-path", "network-topology-combination"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "required-soft-anchor",
			map[string]string{"workload": "required-soft"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "required-soft-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "required-soft"}, nil)
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{Mode: schedulingv1beta1.SoftNetworkTopologyMode}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "required-soft-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "required-soft-challenger-a", challengerPG.Name, "")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "required-soft-challenger-b", challengerPG.Name, "")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA, challengerPodB}, rackBNodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, challengerPG.Name, topology.rackB)).To(Succeed())
	})

	It("resolves topology tier names for anti-affinity and hard NTA together", Label("normal-path", "network-topology-combination"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "tier-name-anchor",
			map[string]string{"workload": "tier-name", "environment": "production"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "tier-name-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := schedulingv1beta1.PodGroupAffinityTerm{
			PodGroupSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "environment",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"production"},
			}}},
			TopologyTierName: "rack",
		}
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{
			Mode:            schedulingv1beta1.HardNetworkTopologyMode,
			HighestTierName: "rack",
		}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "tier-name-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "tier-name-challenger-a", challengerPG.Name, "")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "tier-name-challenger-b", challengerPG.Name, "")

		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA, challengerPodB}, rackBNodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, challengerPG.Name, topology.rackB)).To(Succeed())
	})

	It("combines multiple required terms", Label("normal-path"), func() {
		anchorAPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "multi-term-anchor-a", map[string]string{"workload": "multi-term-a"}, nil, nil, 1)
		anchorAPod := createPodGroupPod(testCtx, testCtx.Namespace, "multi-term-anchor-a-pod", anchorAPG.Name, "kwok-node-0")
		anchorBPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "multi-term-anchor-b", map[string]string{"workload": "multi-term-b"}, nil, nil, 1)
		anchorBPod := createPodGroupPod(testCtx, testCtx.Namespace, "multi-term-anchor-b-pod", anchorBPG.Name, "kwok-node-1")
		Expect(e2eutil.WaitPodReady(testCtx, anchorAPod)).To(Succeed())
		Expect(e2eutil.WaitPodReady(testCtx, anchorBPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorAPG.Name, topology.rackA)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorBPG.Name, topology.rackA)).To(Succeed())

		terms := []schedulingv1beta1.PodGroupAffinityTerm{
			requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "multi-term-a"}, nil),
			requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "multi-term-b"}, nil),
		}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "multi-term-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: terms}, nil, 1)
		challengerPod := createPodGroupPod(testCtx, testCtx.Namespace, "multi-term-challenger-pod", challengerPG.Name, "")

		Expect(e2eutil.WaitPodReady(testCtx, challengerPod)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(BeElementOf("kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"))
	})

	It("reacts to Namespace label changes without recreating the pending PodGroup", Label("dynamic-path"), func() {
		peerNamespace := createPeerNamespace(testCtx, "selected", map[string]string{"anti-affinity-scope": "selected"})
		peerNamespaces = append(peerNamespaces, peerNamespace)

		anchorPG := createTopologyPodGroup(testCtx, peerNamespace, "namespace-anchor", map[string]string{"workload": "namespace-anchor"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, peerNamespace, "namespace-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, peerNamespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "namespace-anchor"}, &metav1.LabelSelector{
			MatchLabels: map[string]string{"anti-affinity-scope": "selected"},
		})
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "namespace-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, nil, 1)
		challengerPod := createPodGroupPod(testCtx, testCtx.Namespace, "namespace-challenger-pod", challengerPG.Name, "kwok-node-1")

		Expect(waitForPodGroupUnschedulable(testCtx, testCtx.Namespace, challengerPG.Name)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(BeEmpty())

		Expect(updateNamespaceLabels(testCtx, peerNamespace, map[string]string{"anti-affinity-scope": "not-selected"})).To(Succeed())
		Expect(e2eutil.WaitPodReady(testCtx, challengerPod)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(Equal("kwok-node-1"))
	})

	It("reacts to matching PodGroup label changes without recreating the pending PodGroup", Label("dynamic-path"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "label-anchor", map[string]string{"workload": "selected"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "label-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "selected"}, nil)
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "label-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, nil, 1)
		challengerPod := createPodGroupPod(testCtx, testCtx.Namespace, "label-challenger-pod", challengerPG.Name, "kwok-node-1")

		Expect(waitForPodGroupUnschedulable(testCtx, testCtx.Namespace, challengerPG.Name)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(BeEmpty())

		Expect(updatePodGroupLabels(testCtx, testCtx.Namespace, anchorPG.Name, map[string]string{"workload": "not-selected"})).To(Succeed())
		Expect(e2eutil.WaitPodReady(testCtx, challengerPod)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(Equal("kwok-node-1"))
	})

	It("refreshes persisted placement after HyperNode membership changes", Label("dynamic-path"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "moving-anchor", map[string]string{"workload": "moving-anchor"}, nil, nil, 1)
		anchorPod := createPodGroupPod(testCtx, testCtx.Namespace, "moving-anchor-pod", anchorPG.Name, "kwok-node-0")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPod)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())

		Expect(updateHyperNodeMembers(testCtx, topology.rackA, []string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})).To(Succeed())
		Expect(updateHyperNodeMembers(testCtx, topology.rackB, []string{"kwok-node-0", "kwok-node-1", "kwok-node-2", "kwok-node-3"})).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackB)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "moving-anchor"}, nil)
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "moving-challenger", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, nil, 1)
		challengerPod := createPodGroupPod(testCtx, testCtx.Namespace, "moving-challenger-pod", challengerPG.Name, "kwok-node-5")

		Expect(e2eutil.WaitPodReady(testCtx, challengerPod)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPod)).To(Equal("kwok-node-5"))
	})

	It("excludes the current PodGroup from its own required selector", Label("normal-path"), func() {
		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "self"}, nil)
		selfPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "self", map[string]string{"workload": "self"},
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, nil, 1)
		selfPod := createPodGroupPod(testCtx, testCtx.Namespace, "self-pod", selfPG.Name, "kwok-node-0")

		Expect(e2eutil.WaitPodReady(testCtx, selfPod)).To(Succeed())
		Expect(podNodeName(testCtx, selfPod)).To(Equal("kwok-node-0"))
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, selfPG.Name, topology.rackA)).To(Succeed())
	})

	It("fails closed when required anti-affinity leaves hard NTA no domain and recovers after task release", Label("dynamic-path", "network-topology-combination"), func() {
		anchorPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "multi-domain-anchor", map[string]string{"workload": "multi-domain"}, nil, nil, 2)
		anchorPodA := createPodGroupPod(testCtx, testCtx.Namespace, "multi-domain-anchor-a", anchorPG.Name, "kwok-node-0")
		anchorPodB := createPodGroupPod(testCtx, testCtx.Namespace, "multi-domain-anchor-b", anchorPG.Name, "kwok-node-4")
		Expect(e2eutil.WaitPodReady(testCtx, anchorPodA)).To(Succeed())
		Expect(e2eutil.WaitPodReady(testCtx, anchorPodB)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.root)).To(Succeed())

		term := requiredPodGroupAntiAffinityTerm(map[string]string{"workload": "multi-domain"}, nil)
		networkTopology := &schedulingv1beta1.NetworkTopologySpec{
			Mode:               schedulingv1beta1.HardNetworkTopologyMode,
			HighestTierAllowed: ptr.To(1),
		}
		challengerPG := createTopologyPodGroup(testCtx, testCtx.Namespace, "no-domain", nil,
			&schedulingv1beta1.PodGroupAntiAffinity{Required: []schedulingv1beta1.PodGroupAffinityTerm{term}}, networkTopology, 2)
		challengerPodA := createPodGroupPod(testCtx, testCtx.Namespace, "no-domain-a", challengerPG.Name, "")
		challengerPodB := createPodGroupPod(testCtx, testCtx.Namespace, "no-domain-b", challengerPG.Name, "")

		Expect(waitForPodGroupUnschedulable(testCtx, testCtx.Namespace, challengerPG.Name)).To(Succeed())
		Expect(podNodeName(testCtx, challengerPodA)).To(BeEmpty())
		Expect(podNodeName(testCtx, challengerPodB)).To(BeEmpty())

		Expect(testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Delete(
			context.TODO(), anchorPodB.Name, metav1.DeleteOptions{},
		)).To(Succeed())
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, anchorPG.Name, topology.rackA)).To(Succeed())
		expectPodsReadyOnNodes(testCtx, []*v1.Pod{challengerPodA, challengerPodB}, rackBNodes)
		Expect(waitForPodGroupPlacement(testCtx, testCtx.Namespace, challengerPG.Name, topology.rackB)).To(Succeed())
	})
})

func replaceSchedulerConfig(config string) func(map[string]string) (bool, map[string]string) {
	return func(data map[string]string) (bool, map[string]string) {
		const key = "volcano-scheduler-ci.conf"
		old := data[key]
		data[key] = config
		return true, map[string]string{key: old}
	}
}

func setupPodGroupAntiAffinityTopology(ctx *e2eutil.TestContext, prefix string) podGroupAntiAffinityTopology {
	topology := podGroupAntiAffinityTopology{
		rackA: prefix + "-rack-a",
		rackB: prefix + "-rack-b",
		root:  prefix + "-root",
	}
	createHyperNodeWithMembers(ctx, topology.rackA, 1, "rack", topologyv1alpha1.MemberTypeNode,
		[]string{"kwok-node-0", "kwok-node-1", "kwok-node-2", "kwok-node-3"})
	createHyperNodeWithMembers(ctx, topology.rackB, 1, "rack", topologyv1alpha1.MemberTypeNode,
		[]string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})
	createHyperNodeWithMembers(ctx, topology.root, 2, "cluster", topologyv1alpha1.MemberTypeHyperNode,
		[]string{topology.rackA, topology.rackB})
	return topology
}

func createHyperNodeWithMembers(
	ctx *e2eutil.TestContext,
	name string,
	tier int,
	tierName string,
	memberType topologyv1alpha1.MemberType,
	members []string,
) {
	specs := make([]topologyv1alpha1.MemberSpec, 0, len(members))
	for _, member := range members {
		specs = append(specs, topologyv1alpha1.MemberSpec{
			Type: memberType,
			Selector: topologyv1alpha1.MemberSelector{
				ExactMatch: &topologyv1alpha1.ExactMatch{Name: member},
			},
		})
	}
	Expect(e2eutil.SetupHyperNode(ctx, &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:     tier,
			TierName: tierName,
			Members:  specs,
		},
	})).To(Succeed())
}

func createTopologyPodGroup(
	ctx *e2eutil.TestContext,
	namespace, name string,
	labels map[string]string,
	antiAffinity *schedulingv1beta1.PodGroupAntiAffinity,
	networkTopology *schedulingv1beta1.NetworkTopologySpec,
	minMember int32,
) *schedulingv1beta1.PodGroup {
	minResources := e2eutil.CPUResource(fmt.Sprintf("%dm", 100*minMember))
	pg := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
		Spec: schedulingv1beta1.PodGroupSpec{
			MinMember:       minMember,
			Queue:           e2eutil.DefaultQueue,
			MinResources:    &minResources,
			NetworkTopology: networkTopology,
		},
	}
	if antiAffinity != nil {
		pg.Spec.TopologyAffinity = &schedulingv1beta1.TopologyAffinitySpec{PodGroupAntiAffinity: antiAffinity}
	}
	created, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return created
}

func createPodGroupPod(ctx *e2eutil.TestContext, namespace, name, podGroup, nodeName string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: podGroup,
			},
		},
		Spec: v1.PodSpec{
			SchedulerName: "volcano",
			RestartPolicy: v1.RestartPolicyNever,
			Tolerations:   tolerations,
			Containers: []v1.Container{{
				Name:      name,
				Image:     e2eutil.DefaultNginxImage,
				Resources: v1.ResourceRequirements{Requests: e2eutil.CPUResource("100m")},
			}},
		},
	}
	if nodeName != "" {
		pod.Spec.NodeSelector = map[string]string{v1.LabelHostname: nodeName}
	}
	created, err := ctx.Kubeclient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return created
}

func requiredPodGroupAntiAffinityTerm(
	matchLabels map[string]string,
	namespaceSelector *metav1.LabelSelector,
) schedulingv1beta1.PodGroupAffinityTerm {
	return schedulingv1beta1.PodGroupAffinityTerm{
		PodGroupSelector:  &metav1.LabelSelector{MatchLabels: matchLabels},
		NamespaceSelector: namespaceSelector,
		TopologyTier:      ptr.To[int32](1),
	}
}

func podNodeName(ctx *e2eutil.TestContext, pod *v1.Pod) string {
	latest, err := ctx.Kubeclient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return latest.Spec.NodeName
}

var (
	rackANodes = []string{"kwok-node-0", "kwok-node-1", "kwok-node-2", "kwok-node-3"}
	rackBNodes = []string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"}
)

func expectPodsReadyOnNodes(ctx *e2eutil.TestContext, pods []*v1.Pod, expectedNodes []string) {
	for _, pod := range pods {
		Expect(e2eutil.WaitPodReady(ctx, pod)).To(Succeed())
		Expect(expectedNodes).To(ContainElement(podNodeName(ctx, pod)))
	}
}

func waitForPodGroupPlacement(ctx *e2eutil.TestContext, namespace, name, expected string) error {
	var actual string
	err := wait.PollUntilContextTimeout(context.TODO(), 500*time.Millisecond, e2eutil.TwoMinute, true,
		func(pollCtx context.Context) (bool, error) {
			pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Get(pollCtx, name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			actual = pg.Annotations[schedulerapi.JobAllocatedHyperNode]
			return actual == expected, nil
		})
	if err != nil {
		return fmt.Errorf("PodGroup %s/%s placement is %q, want %q: %w", namespace, name, actual, expected, err)
	}
	return nil
}

func waitForPodGroupUnschedulable(ctx *e2eutil.TestContext, namespace, name string) error {
	var lastCondition schedulingv1beta1.PodGroupCondition
	err := wait.PollUntilContextTimeout(context.TODO(), 500*time.Millisecond, e2eutil.TwoMinute, true,
		func(pollCtx context.Context) (bool, error) {
			pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Get(pollCtx, name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, condition := range pg.Status.Conditions {
				lastCondition = condition
				if condition.Type == schedulingv1beta1.PodGroupUnschedulableType && condition.Status == v1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("PodGroup %s/%s did not become unschedulable; last condition: %#v: %w",
			namespace, name, lastCondition, err)
	}
	return nil
}

func waitForNoHyperNodes(ctx *e2eutil.TestContext) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, e2eutil.TwoMinute, true,
		func(pollCtx context.Context) (bool, error) {
			hyperNodes, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().List(pollCtx, metav1.ListOptions{})
			if err != nil {
				return false, nil
			}
			return len(hyperNodes.Items) == 0, nil
		})
}

func createPeerNamespace(ctx *e2eutil.TestContext, suffix string, namespaceLabels map[string]string) string {
	name := fmt.Sprintf("%s-%s", ctx.Namespace, suffix)
	_, err := ctx.Kubeclient.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: namespaceLabels},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return name
}

func updateNamespaceLabels(ctx *e2eutil.TestContext, namespace string, namespaceLabels map[string]string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ns, err := ctx.Kubeclient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil {
			return err
		}
		ns.Labels = namespaceLabels
		_, err = ctx.Kubeclient.CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
		return err
	})
}

func updatePodGroupLabels(ctx *e2eutil.TestContext, namespace, name string, labels map[string]string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		pg.Labels = labels
		_, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(namespace).Update(context.TODO(), pg, metav1.UpdateOptions{})
		return err
	})
}

func updateHyperNodeMembers(ctx *e2eutil.TestContext, name string, members []string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		hyperNode, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		hyperNode.Spec.Members = make([]topologyv1alpha1.MemberSpec, 0, len(members))
		for _, member := range members {
			hyperNode.Spec.Members = append(hyperNode.Spec.Members, topologyv1alpha1.MemberSpec{
				Type: topologyv1alpha1.MemberTypeNode,
				Selector: topologyv1alpha1.MemberSelector{
					ExactMatch: &topologyv1alpha1.ExactMatch{Name: member},
				},
			})
		}
		_, err = ctx.Vcclient.TopologyV1alpha1().HyperNodes().Update(context.TODO(), hyperNode, metav1.UpdateOptions{})
		return err
	})
}

func restartVolcanoScheduler() {
	namespace := volcanoSystemNamespace()
	pods, err := e2eutil.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=volcano-scheduler",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(pods.Items).NotTo(BeEmpty())

	oldUIDs := make(map[types.UID]struct{}, len(pods.Items))
	for i := range pods.Items {
		oldUIDs[pods.Items[i].UID] = struct{}{}
		Expect(e2eutil.KubeClient.CoreV1().Pods(namespace).Delete(
			context.TODO(), pods.Items[i].Name, metav1.DeleteOptions{},
		)).To(Succeed())
	}

	Eventually(func() bool {
		current, listErr := e2eutil.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=volcano-scheduler",
		})
		if listErr != nil {
			return false
		}
		for i := range current.Items {
			if _, old := oldUIDs[current.Items[i].UID]; old {
				continue
			}
			for _, condition := range current.Items[i].Status.Conditions {
				if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}, e2eutil.TwoMinute, time.Second).Should(BeTrue())
}

func volcanoSystemNamespace() string {
	if namespace := os.Getenv("VOLCANO_E2E_NAMESPACE"); namespace != "" {
		return namespace
	}
	return "volcano-system"
}

func volcanoSchedulerConfigMapName() string {
	releaseName := os.Getenv("VOLCANO_E2E_RELEASE_NAME")
	if releaseName == "" {
		releaseName = "integration"
	}
	return releaseName + "-scheduler-configmap"
}
