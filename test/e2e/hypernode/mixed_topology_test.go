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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	mixedProfileLabel        = "volcano.sh/e2e-topology-profile"
	mixedShallowDomainLabel  = "volcano.sh/e2e-shallow-domain"
	mixedCompactClusterLabel = "volcano.sh/e2e-compact-hypercluster"
	mixedWideHyperNodeLabel  = "volcano.sh/e2e-wide-hypernode"
	mixedDeepSuperPodLabel   = "volcano.sh/e2e-deep-superpod"
	mixedDeepHyperNodeLabel  = "volcano.sh/e2e-deep-hypernode"
	mixedClusterLabel        = "volcano.sh/e2e-hypercluster"
	mixedTopologySourceKey   = "volcano.sh/network-topology-source"
	mixedTopologyProfileKey  = "volcano.sh/network-topology-profile"
)

type mixedTopologyHyperNodeSnapshot struct {
	UID  string
	Spec topologyv1alpha1.HyperNodeSpec
}

var mixedTopologyTolerations = []v1.Toleration{{
	Key:      "kwok.x-k8s.io/node",
	Operator: v1.TolerationOpEqual,
	Value:    "fake",
	Effect:   v1.TaintEffectNoSchedule,
}}

var _ = Describe("Mixed shallow and deep topology", Serial, func() {
	var testCtx *e2eutil.TestContext

	BeforeEach(func() {
		testCtx = e2eutil.InitTestContext(e2eutil.Options{NodesNumLimit: 8})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(testCtx)
	})

	It("discovers mixed profiles and isolates their update and deletion lifecycles", func() {
		controllerConfigMap, err := findControllerConfigMap(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		originalControllerConfig := controllerConfigMap.Data["volcano-controller.conf"]

		originalNodes, err := labelMixedTopologyNodes(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, originalControllerConfig)).To(Succeed())
			Expect(restoreNodeLabels(testCtx.Kubeclient, originalNodes)).To(Succeed())
		}()

		validConfig := mixedTopologyDiscoveryConfig()
		By("enabling single-tier shallow and three-tier deep label discovery profiles")
		Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, validConfig)).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 6, "", 2)
		}, 60*time.Second, time.Second).Should(BeTrue())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedGraph(testCtx)
		}, 60*time.Second, time.Second).Should(BeTrue(),
			"shallow and deep should remain separate complete trees while using the same semantic root tier name")
		deepSnapshot, err := mixedTopologyProfileSnapshot(testCtx, "topologydeep")
		Expect(err).NotTo(HaveOccurred())
		Expect(deepSnapshot).To(HaveLen(4))

		By("scheduling a job whose semantic hypercluster tier is tier 1 in shallow and tier 3 in deep")
		job := e2eutil.CreateJob(testCtx, &e2eutil.JobSpec{
			Name: "mixed-tier-name-job",
			NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
				Mode:            batchv1alpha1.HardNetworkTopologyMode,
				HighestTierName: "volcano.sh/hypercluster",
			},
			Tasks: []e2eutil.TaskSpec{{
				Name:        "worker",
				Img:         e2eutil.DefaultNginxImage,
				Req:         e2eutil.CPU5Mem5,
				Min:         4,
				Rep:         4,
				Tolerations: mixedTopologyTolerations,
			}},
		})
		defer e2eutil.DeleteJob(testCtx, job)
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.VerifyPodScheduling(testCtx, job,
			[]string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())

		By("changing a shallow Node without changing deep")
		Expect(setNodeLabel(testCtx.Kubeclient, "kwok-node-0", mixedShallowDomainLabel, "hn-shallow-new")).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 7, "hn-shallow-new", 3)
		}, 60*time.Second, time.Second).Should(BeTrue())
		currentDeep, err := mixedTopologyProfileSnapshot(testCtx, "topologydeep")
		Expect(err).NotTo(HaveOccurred())
		Expect(currentDeep).To(Equal(deepSnapshot), "a shallow-only domain change must not recreate or mutate deep")

		By("restoring the shallow domain without changing deep")
		Expect(setNodeLabel(testCtx.Kubeclient, "kwok-node-0", mixedShallowDomainLabel, "hn-shallow-0")).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 6, "", 2)
		}, 60*time.Second, time.Second).Should(BeTrue())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedGraph(testCtx)
		}, 60*time.Second, time.Second).Should(BeTrue())
		currentDeep, err = mixedTopologyProfileSnapshot(testCtx, "topologydeep")
		Expect(err).NotTo(HaveOccurred())
		Expect(currentDeep).To(Equal(deepSnapshot), "restoring shallow must preserve the independent deep tree")

		By("removing every shallow Node from its profile without changing deep")
		for i := 0; i < 4; i++ {
			Expect(setNodeLabel(testCtx.Kubeclient, fmt.Sprintf("kwok-node-%d", i), mixedProfileLabel, "disabled")).To(Succeed())
		}
		Eventually(func() (bool, error) {
			shallowSnapshot, err := mixedTopologyProfileSnapshot(testCtx, "topologyshallow")
			return len(shallowSnapshot) == 0, err
		}, 60*time.Second, time.Second).Should(BeTrue(), "all shallow HyperNodes should be deleted")
		Consistently(func() (map[string]mixedTopologyHyperNodeSnapshot, error) {
			return mixedTopologyProfileSnapshot(testCtx, "topologydeep")
		}, 2*time.Second, 200*time.Millisecond).Should(Equal(deepSnapshot),
			"deleting the shallow tree must not recreate or mutate deep")

		By("restoring shallow after its independent tree was deleted")
		for i := 0; i < 4; i++ {
			Expect(setNodeLabel(testCtx.Kubeclient, fmt.Sprintf("kwok-node-%d", i), mixedProfileLabel, "shallow")).To(Succeed())
		}
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedGraph(testCtx)
		}, 60*time.Second, time.Second).Should(BeTrue())
		currentDeep, err = mixedTopologyProfileSnapshot(testCtx, "topologydeep")
		Expect(err).NotTo(HaveOccurred())
		Expect(currentDeep).To(Equal(deepSnapshot), "recreating shallow must preserve the independent deep tree")
	})

	It("selects feasible hard topology trees without crossing semantic boundaries", func() {
		controllerConfigMap, err := findControllerConfigMap(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		originalControllerConfig := controllerConfigMap.Data["volcano-controller.conf"]

		originalNodes, err := labelMixedTopologyNodes(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, originalControllerConfig)).To(Succeed())
			Expect(restoreNodeLabels(testCtx.Kubeclient, originalNodes)).To(Succeed())
		}()

		By("enabling single-tier shallow and three-tier deep label discovery profiles")
		Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, mixedTopologyDiscoveryConfig())).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 6, "", 2)
		}, 60*time.Second, time.Second).Should(BeTrue())

		By("selecting shallow when deep is the only resource-infeasible tree")
		deepBlockers := createMixedTopologyBlockers(testCtx, "mixed-deep-blocker", []int{4, 5, 6, 7})
		shallowOnlyJob := createMixedTopologyHardJob(testCtx, "mixed-hard-shallow-only", "volcano.sh/hypercluster", 2)
		Expect(e2eutil.WaitJobReady(testCtx, shallowOnlyJob)).NotTo(HaveOccurred())
		hasShallow, hasDeep, err := mixedTopologyJobUsesProfiles(testCtx, shallowOnlyJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasShallow).To(BeTrue())
		Expect(hasDeep).To(BeFalse())
		shallowDomains, err := mixedTopologyJobDomains(testCtx, shallowOnlyJob, mixedShallowDomainLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(shallowDomains).To(HaveLen(1), "the hard gang must fit within one shallow domain")
		e2eutil.DeleteJob(testCtx, shallowOnlyJob)
		Expect(e2eutil.WaitJobCleanedUp(testCtx, shallowOnlyJob)).NotTo(HaveOccurred())
		deleteMixedTopologyBlockers(testCtx, deepBlockers)

		By("selecting deep when only its semantic hypernode domain can contain the gang")
		deepOnlyJob := createMixedTopologyHardJob(testCtx, "mixed-hard-deep-only", "volcano.sh/hypernode", 2)
		Expect(e2eutil.WaitJobReady(testCtx, deepOnlyJob)).NotTo(HaveOccurred())
		Expect(e2eutil.VerifyPodScheduling(testCtx, deepOnlyJob,
			[]string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())
		e2eutil.DeleteJob(testCtx, deepOnlyJob)
		Expect(e2eutil.WaitJobCleanedUp(testCtx, deepOnlyJob)).NotTo(HaveOccurred())

		By("keeping a hard gang in one real tree when both trees are feasible")
		bothFeasibleJob := createMixedTopologyHardJob(testCtx, "mixed-hard-both-feasible", "volcano.sh/hypercluster", 2)
		Expect(e2eutil.WaitJobReady(testCtx, bothFeasibleJob)).NotTo(HaveOccurred())
		hasShallow, hasDeep, err = mixedTopologyJobUsesProfiles(testCtx, bothFeasibleJob)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasShallow != hasDeep).To(BeTrue(), "a hard gang must select exactly one feasible real tree")
		e2eutil.DeleteJob(testCtx, bothFeasibleJob)
		Expect(e2eutil.WaitJobCleanedUp(testCtx, bothFeasibleJob)).NotTo(HaveOccurred())

		By("excluding shallow when the requested semantic superpod tier exists only in deep")
		deepTierOnlyJob := createMixedTopologyHardJob(testCtx, "mixed-hard-deep-tier-only", "volcano.sh/superpod", 2)
		Expect(e2eutil.WaitJobReady(testCtx, deepTierOnlyJob)).NotTo(HaveOccurred())
		Expect(e2eutil.VerifyPodScheduling(testCtx, deepTierOnlyJob,
			[]string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())
		domains, err := mixedTopologyJobDomains(testCtx, deepTierOnlyJob, mixedDeepSuperPodLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(domains).To(HaveLen(1), "the hard gang must fit within one deep superpod")
		e2eutil.DeleteJob(testCtx, deepTierOnlyJob)
		Expect(e2eutil.WaitJobCleanedUp(testCtx, deepTierOnlyJob)).NotTo(HaveOccurred())

		By("keeping the complete hard gang pending when neither tree is feasible")
		allBlockers := createMixedTopologyBlockers(testCtx, "mixed-all-blocker", []int{0, 1, 2, 3, 4, 5, 6, 7})
		defer deleteMixedTopologyBlockers(testCtx, allBlockers)
		neitherFeasibleJob := createMixedTopologyHardJob(testCtx, "mixed-hard-neither-feasible", "volcano.sh/hypercluster", 4)
		defer e2eutil.DeleteJob(testCtx, neitherFeasibleJob)
		Expect(e2eutil.WaitTaskPhase(testCtx, neitherFeasibleJob, []v1.PodPhase{v1.PodPending}, 4)).NotTo(HaveOccurred())
		Consistently(func() (bool, error) {
			return mixedTopologyJobPodsUnbound(testCtx, neitherFeasibleJob, 4)
		}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(),
			"an infeasible hard gang must not partially bind or cross shallow and deep")
	})

	It("keeps hard subgroups in their semantic domains across pod and scheduler restarts", func() {
		controllerConfigMap, err := findControllerConfigMap(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		originalControllerConfig := controllerConfigMap.Data["volcano-controller.conf"]

		originalNodes, err := labelMixedTopologyNodes(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, originalControllerConfig)).To(Succeed())
			Expect(restoreNodeLabels(testCtx.Kubeclient, originalNodes)).To(Succeed())
		}()

		By("enabling single-tier shallow and three-tier deep label discovery profiles")
		Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, mixedTopologyDiscoveryConfig())).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 6, "", 2)
		}, 60*time.Second, time.Second).Should(BeTrue())

		By("scheduling two hard subgroups on the deep-only superpod tier")
		job := e2eutil.CreateJob(testCtx, &e2eutil.JobSpec{
			Name: "mixed-hard-subgroup-job",
			NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
				Mode:            batchv1alpha1.HardNetworkTopologyMode,
				HighestTierName: "volcano.sh/hypercluster",
			},
			Tasks: []e2eutil.TaskSpec{{
				Name:        "worker",
				Img:         e2eutil.DefaultNginxImage,
				Req:         e2eutil.CPU5Mem5,
				Min:         4,
				Rep:         4,
				Tolerations: mixedTopologyTolerations,
				PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
					TotalPartitions: 2,
					PartitionSize:   2,
					MinPartitions:   2,
					NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
						Mode:            batchv1alpha1.HardNetworkTopologyMode,
						HighestTierName: "volcano.sh/superpod",
					},
				},
			}},
		})
		defer e2eutil.DeleteJob(testCtx, job)
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())

		domainsBefore, err := mixedTopologySubGroupDomains(testCtx, job, "deep", mixedDeepSuperPodLabel, 2, 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(sets.New(domainsBefore["0"], domainsBefore["1"])).To(HaveLen(2),
			"the two CPU-saturated subgroups must occupy different deep superpods")

		By("deleting one subgroup pod and waiting for a different pod instance")
		Expect(replaceMixedTopologyJobPod(testCtx, job, "0", 4)).To(Succeed())
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())

		By("verifying the replacement remains in the original subgroup superpod")
		domainsAfter, err := mixedTopologySubGroupDomains(testCtx, job, "deep", mixedDeepSuperPodLabel, 2, 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(domainsAfter).To(Equal(domainsBefore))

		By("restarting every Volcano Scheduler process and waiting for new ready instances")
		Expect(restartVolcanoScheduler(testCtx.Kubeclient)).To(Succeed())

		By("replacing a pod after the scheduler has lost its in-memory allocation state")
		Expect(replaceMixedTopologyJobPod(testCtx, job, "1", 4)).To(Succeed())
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())

		By("verifying scheduler recovery keeps every subgroup in its original superpod")
		domainsAfterRestart, err := mixedTopologySubGroupDomains(testCtx, job, "deep", mixedDeepSuperPodLabel, 2, 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(domainsAfterRestart).To(Equal(domainsBefore))
	})

	It("keeps a native hard numeric boundary in its real tree across scheduler recovery", func() {
		controllerConfigMap, err := findControllerConfigMap(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		originalControllerConfig := controllerConfigMap.Data["volcano-controller.conf"]

		originalNodes, err := labelMixedTopologyNodes(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, originalControllerConfig)).To(Succeed())
			Expect(restoreNodeLabels(testCtx.Kubeclient, originalNodes)).To(Succeed())
		}()

		By("enabling shallow and deep topology trees below the numeric cluster boundary")
		Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, mixedTopologyDiscoveryConfig())).To(Succeed())
		Eventually(func() (bool, error) {
			return mixedTopologyHasExpectedTiers(testCtx, 6, "", 2)
		}, 60*time.Second, time.Second).Should(BeTrue())

		By("forcing the initial native Hard allocation into the shallow real tree")
		deepBlockers := createMixedTopologyBlockers(testCtx, "mixed-numeric-recovery-blocker", []int{4, 5, 6, 7})
		defer func() {
			if len(deepBlockers) > 0 {
				deleteMixedTopologyBlockers(testCtx, deepBlockers)
			}
		}()
		job := createMixedTopologyNumericHardJob(testCtx, "mixed-hard-numeric-recovery", 4, 2)
		defer e2eutil.DeleteJob(testCtx, job)
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())
		hasShallow, hasDeep, err := mixedTopologyJobUsesProfiles(testCtx, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasShallow).To(BeTrue())
		Expect(hasDeep).To(BeFalse())
		domainsBefore, err := mixedTopologyJobDomains(testCtx, job, mixedShallowDomainLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(domainsBefore).To(HaveLen(1))

		By("making the sibling deep tree feasible before restarting the scheduler")
		deleteMixedTopologyBlockers(testCtx, deepBlockers)
		deepBlockers = nil
		Expect(restartVolcanoScheduler(testCtx.Kubeclient)).To(Succeed())

		By("replacing a pod after in-memory allocation state has been reconstructed")
		Expect(replaceMixedTopologyJobPod(testCtx, job, "", 2)).To(Succeed())
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())

		By("verifying the replacement cannot escape to the now-feasible sibling tree")
		hasShallow, hasDeep, err = mixedTopologyJobUsesProfiles(testCtx, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasShallow).To(BeTrue())
		Expect(hasDeep).To(BeFalse())
		domainsAfter, err := mixedTopologyJobDomains(testCtx, job, mixedShallowDomainLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(domainsAfter).To(Equal(domainsBefore))
	})

})

var _ = Describe("Mixed topology profile combinations", Serial, func() {
	var testCtx *e2eutil.TestContext

	BeforeEach(func() {
		testCtx = e2eutil.InitTestContext(e2eutil.Options{NodesNumLimit: 8})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(testCtx)
	})

	It("keeps three arbitrary topology depths independent", func() {
		controllerConfigMap, err := findControllerConfigMap(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		originalConfig := controllerConfigMap.Data["volcano-controller.conf"]
		originalNodes, err := labelThreeMixedTopologyNodes(testCtx.Kubeclient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, originalConfig)).To(Succeed())
			Expect(restoreNodeLabels(testCtx.Kubeclient, originalNodes)).To(Succeed())
		}()

		Expect(setControllerConfig(testCtx.Kubeclient, controllerConfigMap, threeMixedTopologyDiscoveryConfig())).To(Succeed())
		Eventually(func() (bool, error) {
			return threeMixedTopologyHasExpectedGraph(testCtx)
		}, 60*time.Second, time.Second).Should(BeTrue())

		By("selecting the only three-tier tree that can contain a four-pod hard gang")
		job := createMixedTopologyHardJob(testCtx, "three-profile-hard-job", "volcano.sh/hypercluster", 4)
		defer e2eutil.DeleteJob(testCtx, job)
		Expect(e2eutil.WaitJobReady(testCtx, job)).NotTo(HaveOccurred())
		Expect(e2eutil.VerifyPodScheduling(testCtx, job,
			[]string{"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())
	})
})

func labelThreeMixedTopologyNodes(client kubernetes.Interface) (map[string]*v1.Node, error) {
	originals := make(map[string]*v1.Node, 8)
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("kwok-node-%d", i)
		node, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		originals[name] = node.DeepCopy()
		if err := updateNodeLabels(client, name, func(labels map[string]string) {
			labels[mixedClusterLabel] = "hc-shared"
			switch {
			case i < 2:
				labels[mixedProfileLabel] = "compact"
				labels[mixedCompactClusterLabel] = "compact-0"
			case i < 4:
				labels[mixedProfileLabel] = "wide"
				labels[mixedWideHyperNodeLabel] = "wide-0"
			case i < 6:
				labels[mixedProfileLabel] = "deep"
				labels[mixedDeepHyperNodeLabel] = "deep-0"
				labels[mixedDeepSuperPodLabel] = "deep-sp-0"
			default:
				labels[mixedProfileLabel] = "deep"
				labels[mixedDeepHyperNodeLabel] = "deep-0"
				labels[mixedDeepSuperPodLabel] = "deep-sp-1"
			}
		}); err != nil {
			return nil, err
		}
	}
	return originals, nil
}

func threeMixedTopologyDiscoveryConfig() string {
	return `networkTopologyDiscovery:
  - source: label
    enabled: true
    config:
      networkTopologyTypes:
        topologycompact:
          nodeSelector:
            matchLabels:
              volcano.sh/e2e-topology-profile: compact
          levels:
            - nodeLabel: volcano.sh/e2e-compact-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: kubernetes.io/hostname
        topologywide:
          nodeSelector:
            matchLabels:
              volcano.sh/e2e-topology-profile: wide
          levels:
            - nodeLabel: volcano.sh/e2e-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: volcano.sh/e2e-wide-hypernode
              tierName: volcano.sh/hypernode
            - nodeLabel: kubernetes.io/hostname
        topologydeep:
          nodeSelector:
            matchLabels:
              volcano.sh/e2e-topology-profile: deep
          levels:
            - nodeLabel: volcano.sh/e2e-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: volcano.sh/e2e-deep-hypernode
              tierName: volcano.sh/hypernode
            - nodeLabel: volcano.sh/e2e-deep-superpod
              tierName: volcano.sh/superpod
            - nodeLabel: kubernetes.io/hostname
`
}

func threeMixedTopologyHasExpectedGraph(testCtx *e2eutil.TestContext) (bool, error) {
	hyperNodes, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{mixedTopologySourceKey: "label"}.AsSelector().String(),
	})
	if err != nil {
		return false, err
	}
	if len(hyperNodes.Items) != 7 {
		return false, nil
	}
	byProfile := map[string]map[int][]*topologyv1alpha1.HyperNode{}
	for i := range hyperNodes.Items {
		hn := &hyperNodes.Items[i]
		profile := hn.Labels[mixedTopologyProfileKey]
		if byProfile[profile] == nil {
			byProfile[profile] = map[int][]*topologyv1alpha1.HyperNode{}
		}
		byProfile[profile][hn.Spec.Tier] = append(byProfile[profile][hn.Spec.Tier], hn)
	}
	check := func(profile string, tierNames map[int]string, expectedLeafNodes sets.Set[string]) bool {
		byTier := byProfile[profile]
		if len(byTier) != len(tierNames) {
			return false
		}
		for tier, name := range tierNames {
			items := byTier[tier]
			if len(items) == 0 {
				return false
			}
			for _, hn := range items {
				if hn.Spec.TierName != name || hn.Labels[mixedTopologyProfileKey] != profile {
					return false
				}
			}
		}
		leafNodes := sets.New[string]()
		for _, leaf := range byTier[1] {
			for _, member := range leaf.Spec.Members {
				if member.Type != topologyv1alpha1.MemberTypeNode || member.Selector.ExactMatch == nil {
					return false
				}
				leafNodes.Insert(member.Selector.ExactMatch.Name)
			}
		}
		if !leafNodes.Equal(expectedLeafNodes) {
			return false
		}
		previousTier := byTier[1]
		maxTier := len(tierNames)
		for tier := 2; tier <= maxTier; tier++ {
			if len(byTier[tier]) != 1 {
				return false
			}
			expectedChildren := sets.New[string]()
			for _, child := range previousTier {
				expectedChildren.Insert(child.Name)
			}
			actualChildren := sets.New[string]()
			for _, member := range byTier[tier][0].Spec.Members {
				if member.Type != topologyv1alpha1.MemberTypeHyperNode || member.Selector.ExactMatch == nil {
					return false
				}
				actualChildren.Insert(member.Selector.ExactMatch.Name)
			}
			if !actualChildren.Equal(expectedChildren) {
				return false
			}
			previousTier = byTier[tier]
		}
		return true
	}
	return check("topologycompact", map[int]string{1: "volcano.sh/hypercluster"}, sets.New("kwok-node-0", "kwok-node-1")) &&
		check("topologywide", map[int]string{1: "volcano.sh/hypernode", 2: "volcano.sh/hypercluster"}, sets.New("kwok-node-2", "kwok-node-3")) &&
		check("topologydeep", map[int]string{1: "volcano.sh/superpod", 2: "volcano.sh/hypernode", 3: "volcano.sh/hypercluster"}, sets.New("kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7")), nil
}

func findControllerConfigMap(client kubernetes.Interface) (*v1.ConfigMap, error) {
	configMaps, err := client.CoreV1().ConfigMaps(v1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var matched *v1.ConfigMap
	for i := range configMaps.Items {
		configMap := &configMaps.Items[i]
		if !strings.HasSuffix(configMap.Name, "-controller-configmap") {
			continue
		}
		if _, exists := configMap.Data["volcano-controller.conf"]; !exists {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("multiple Volcano controller ConfigMaps found: %s/%s and %s/%s",
				matched.Namespace, matched.Name, configMap.Namespace, configMap.Name)
		}
		matched = configMap.DeepCopy()
	}
	if matched == nil {
		return nil, fmt.Errorf("Volcano controller ConfigMap not found")
	}
	return matched, nil
}

func setControllerConfig(client kubernetes.Interface, configMap *v1.ConfigMap, value string) error {
	current, err := client.CoreV1().ConfigMaps(configMap.Namespace).Get(
		context.Background(), configMap.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	current = current.DeepCopy()
	if current.Data == nil {
		current.Data = make(map[string]string)
	}
	current.Data["volcano-controller.conf"] = value
	_, err = client.CoreV1().ConfigMaps(configMap.Namespace).Update(
		context.Background(), current, metav1.UpdateOptions{})
	return err
}

func labelMixedTopologyNodes(client kubernetes.Interface) (map[string]*v1.Node, error) {
	originals := make(map[string]*v1.Node, 8)
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("kwok-node-%d", i)
		node, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		originals[name] = node.DeepCopy()
		if err := updateNodeLabels(client, name, func(labels map[string]string) {
			labels[mixedClusterLabel] = "hc-shared"
			if i < 4 {
				labels[mixedProfileLabel] = "shallow"
				labels[mixedShallowDomainLabel] = fmt.Sprintf("hn-shallow-%d", i/2)
			} else {
				labels[mixedProfileLabel] = "deep"
				labels[mixedDeepHyperNodeLabel] = "hn-deep"
				labels[mixedDeepSuperPodLabel] = fmt.Sprintf("sp-deep-%d", (i-4)/2)
			}
		}); err != nil {
			return nil, err
		}
	}
	return originals, nil
}

func restoreNodeLabels(client kubernetes.Interface, originals map[string]*v1.Node) error {
	for name, original := range originals {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			current = current.DeepCopy()
			current.Labels = original.Labels
			_, err = client.CoreV1().Nodes().Update(context.Background(), current, metav1.UpdateOptions{})
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func setNodeLabel(client kubernetes.Interface, nodeName, key, value string) error {
	return updateNodeLabels(client, nodeName, func(labels map[string]string) {
		labels[key] = value
	})
}

func updateNodeLabels(client kubernetes.Interface, nodeName string, update func(map[string]string)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		node = node.DeepCopy()
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		update(node.Labels)
		_, err = client.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
		return err
	})
}

func mixedTopologyHasExpectedTiers(testCtx *e2eutil.TestContext, expectedCount int, expectedDomain string, expectedShallowTier1 int) (bool, error) {
	hyperNodes, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{mixedTopologySourceKey: "label"}.AsSelector().String(),
	})
	if err != nil {
		return false, err
	}
	if len(hyperNodes.Items) != expectedCount {
		return false, nil
	}

	tiersByName := map[string]map[int]int{}
	domainFound := expectedDomain == ""
	for i := range hyperNodes.Items {
		hyperNode := &hyperNodes.Items[i]
		if tiersByName[hyperNode.Spec.TierName] == nil {
			tiersByName[hyperNode.Spec.TierName] = make(map[int]int)
		}
		tiersByName[hyperNode.Spec.TierName][hyperNode.Spec.Tier]++
		if hyperNode.Labels[mixedShallowDomainLabel] == expectedDomain {
			domainFound = true
		}
	}

	return domainFound &&
		tiersByName["volcano.sh/hypercluster"][1] == expectedShallowTier1 &&
		tiersByName["volcano.sh/superpod"][1] == 2 &&
		tiersByName["volcano.sh/hypernode"][2] == 1 &&
		tiersByName["volcano.sh/hypercluster"][3] == 1, nil
}

func mixedTopologyHasExpectedGraph(testCtx *e2eutil.TestContext) (bool, error) {
	hyperNodes, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{mixedTopologySourceKey: "label"}.AsSelector().String(),
	})
	if err != nil {
		return false, err
	}
	if len(hyperNodes.Items) != 6 {
		return false, nil
	}

	byProfile := map[string][]*topologyv1alpha1.HyperNode{}
	for i := range hyperNodes.Items {
		hyperNode := &hyperNodes.Items[i]
		profile := hyperNode.Labels[mixedTopologyProfileKey]
		byProfile[profile] = append(byProfile[profile], hyperNode)
	}

	validateProfile := func(profile string, expectedNodes sets.Set[string], expectedTierNames map[int]string) (sets.Set[string], bool) {
		byTier := map[int][]*topologyv1alpha1.HyperNode{}
		for _, hyperNode := range byProfile[profile] {
			if hyperNode.Spec.TierName != expectedTierNames[hyperNode.Spec.Tier] {
				return nil, false
			}
			byTier[hyperNode.Spec.Tier] = append(byTier[hyperNode.Spec.Tier], hyperNode)
		}
		if len(byTier[1]) != 2 || len(byProfile[profile]) != len(expectedTierNames)+1 {
			return nil, false
		}

		leafNodes := sets.New[string]()
		for _, leaf := range byTier[1] {
			if len(leaf.Spec.Members) != 2 {
				return nil, false
			}
			for _, member := range leaf.Spec.Members {
				if member.Type != topologyv1alpha1.MemberTypeNode || member.Selector.ExactMatch == nil {
					return nil, false
				}
				leafNodes.Insert(member.Selector.ExactMatch.Name)
			}
		}
		if !leafNodes.Equal(expectedNodes) {
			return nil, false
		}

		previousTier := byTier[1]
		topLevel := sets.New[string]()
		for tier := 2; tier <= len(expectedTierNames); tier++ {
			if len(byTier[tier]) != 1 {
				return nil, false
			}
			expectedMembers := sets.New[string]()
			for _, child := range previousTier {
				expectedMembers.Insert(child.Name)
			}
			actualMembers := sets.New[string]()
			for _, member := range byTier[tier][0].Spec.Members {
				if member.Type != topologyv1alpha1.MemberTypeHyperNode || member.Selector.ExactMatch == nil {
					return nil, false
				}
				actualMembers.Insert(member.Selector.ExactMatch.Name)
			}
			if !actualMembers.Equal(expectedMembers) {
				return nil, false
			}
			topLevel.Insert(byTier[tier][0].Name)
			previousTier = byTier[tier]
		}
		if len(expectedTierNames) == 1 {
			for _, leaf := range byTier[1] {
				topLevel.Insert(leaf.Name)
			}
		}
		return topLevel, true
	}

	shallowRoots, shallowValid := validateProfile("topologyshallow", sets.New[string](
		"kwok-node-0", "kwok-node-1", "kwok-node-2", "kwok-node-3"), map[int]string{
		1: "volcano.sh/hypercluster",
	})
	deepRoots, deepValid := validateProfile("topologydeep", sets.New[string](
		"kwok-node-4", "kwok-node-5", "kwok-node-6", "kwok-node-7"), map[int]string{
		1: "volcano.sh/superpod",
		2: "volcano.sh/hypernode",
		3: "volcano.sh/hypercluster",
	})
	for root := range shallowRoots {
		if deepRoots.Has(root) {
			return false, nil
		}
	}
	return shallowValid && deepValid, nil
}

func mixedTopologyProfileSnapshot(testCtx *e2eutil.TestContext, profile string) (map[string]mixedTopologyHyperNodeSnapshot, error) {
	hyperNodes, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{
			mixedTopologySourceKey:  "label",
			mixedTopologyProfileKey: profile,
		}.AsSelector().String(),
	})
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]mixedTopologyHyperNodeSnapshot, len(hyperNodes.Items))
	for i := range hyperNodes.Items {
		hyperNode := hyperNodes.Items[i].DeepCopy()
		snapshot[hyperNode.Name] = mixedTopologyHyperNodeSnapshot{
			UID:  string(hyperNode.UID),
			Spec: hyperNode.Spec,
		}
	}
	return snapshot, nil
}

func mixedTopologyJobUsesProfiles(testCtx *e2eutil.TestContext, job *batchv1alpha1.Job) (bool, bool, error) {
	hasShallow := false
	hasDeep := false
	for _, pod := range e2eutil.GetTasksOfJob(testCtx, job) {
		if pod.Spec.NodeName == "" {
			return false, false, fmt.Errorf("pod %s/%s is not scheduled", pod.Namespace, pod.Name)
		}
		node, err := testCtx.Kubeclient.CoreV1().Nodes().Get(context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			return false, false, err
		}
		switch node.Labels[mixedProfileLabel] {
		case "shallow":
			hasShallow = true
		case "deep":
			hasDeep = true
		default:
			return false, false, fmt.Errorf("pod %s/%s scheduled to node %s without a mixed topology profile", pod.Namespace, pod.Name, pod.Spec.NodeName)
		}
	}
	return hasShallow, hasDeep, nil
}

func createMixedTopologyHardJob(
	testCtx *e2eutil.TestContext,
	name, highestTierName string,
	replicas int32,
) *batchv1alpha1.Job {
	return e2eutil.CreateJob(testCtx, &e2eutil.JobSpec{
		Name: name,
		NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
			Mode:            batchv1alpha1.HardNetworkTopologyMode,
			HighestTierName: highestTierName,
		},
		Tasks: []e2eutil.TaskSpec{{
			Name:        "worker",
			Img:         e2eutil.DefaultNginxImage,
			Req:         e2eutil.CPU5Mem5,
			Min:         replicas,
			Rep:         replicas,
			Tolerations: mixedTopologyTolerations,
		}},
	})
}

func createMixedTopologyNumericHardJob(
	testCtx *e2eutil.TestContext,
	name string,
	highestTierAllowed int,
	replicas int32,
) *batchv1alpha1.Job {
	return e2eutil.CreateJob(testCtx, &e2eutil.JobSpec{
		Name: name,
		NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
			Mode:               batchv1alpha1.HardNetworkTopologyMode,
			HighestTierAllowed: ptr.To(highestTierAllowed),
		},
		Tasks: []e2eutil.TaskSpec{{
			Name:        "worker",
			Img:         e2eutil.DefaultNginxImage,
			Req:         e2eutil.CPU5Mem5,
			Min:         replicas,
			Rep:         replicas,
			Tolerations: mixedTopologyTolerations,
		}},
	})
}

func createMixedTopologyBlockers(testCtx *e2eutil.TestContext, prefix string, nodeIndexes []int) []*v1.Pod {
	pods := make([]*v1.Pod, 0, len(nodeIndexes))
	for _, nodeIndex := range nodeIndexes {
		pod := e2eutil.CreatePod(testCtx, e2eutil.PodSpec{
			Name:        fmt.Sprintf("%s-%d", prefix, nodeIndex),
			Node:        fmt.Sprintf("kwok-node-%d", nodeIndex),
			Req:         e2eutil.CPU4Mem4,
			Tolerations: mixedTopologyTolerations,
		})
		Expect(e2eutil.WaitPodReady(testCtx, pod)).NotTo(HaveOccurred())
		pods = append(pods, pod)
	}
	return pods
}

func deleteMixedTopologyBlockers(testCtx *e2eutil.TestContext, pods []*v1.Pod) {
	for _, pod := range pods {
		e2eutil.DeletePod(testCtx, pod)
	}
	for _, pod := range pods {
		Expect(e2eutil.WaitPodGone(testCtx, pod.Name, pod.Namespace)).NotTo(HaveOccurred())
	}
}

func mixedTopologyJobDomains(
	testCtx *e2eutil.TestContext,
	job *batchv1alpha1.Job,
	domainLabel string,
) (sets.Set[string], error) {
	domains := sets.New[string]()
	for _, pod := range e2eutil.GetTasksOfJob(testCtx, job) {
		if pod.Spec.NodeName == "" {
			return nil, fmt.Errorf("pod %s/%s is not scheduled", pod.Namespace, pod.Name)
		}
		node, err := testCtx.Kubeclient.CoreV1().Nodes().Get(
			context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		domain := node.Labels[domainLabel]
		if domain == "" {
			return nil, fmt.Errorf("node %s has no topology domain label %s", node.Name, domainLabel)
		}
		domains.Insert(domain)
	}
	return domains, nil
}

func mixedTopologyJobPodsUnbound(
	testCtx *e2eutil.TestContext,
	job *batchv1alpha1.Job,
	expectedPods int,
) (bool, error) {
	pods, err := testCtx.Kubeclient.CoreV1().Pods(job.Namespace).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	controlledPods := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !metav1.IsControlledBy(pod, job) {
			continue
		}
		controlledPods++
		if pod.Spec.NodeName != "" || pod.Status.Phase != v1.PodPending {
			return false, nil
		}
	}
	return controlledPods == expectedPods, nil
}

func mixedTopologySubGroupDomains(
	testCtx *e2eutil.TestContext,
	job *batchv1alpha1.Job,
	expectedProfile, domainLabel string,
	expectedPartitions, expectedPartitionSize int,
) (map[string]string, error) {
	pods := e2eutil.GetTasksOfJob(testCtx, job)
	expectedPods := expectedPartitions * expectedPartitionSize
	if len(pods) != expectedPods {
		return nil, fmt.Errorf("expected %d pods for job %s, got %d", expectedPods, job.Name, len(pods))
	}

	podCounts := make(map[string]int, expectedPartitions)
	domainsByPartition := make(map[string]sets.Set[string], expectedPartitions)
	for _, pod := range pods {
		partition, found := pod.Labels[batchv1alpha1.TaskPartitionID]
		if !found || partition == "" {
			return nil, fmt.Errorf("pod %s/%s has no partition label", pod.Namespace, pod.Name)
		}
		if pod.Spec.NodeName == "" {
			return nil, fmt.Errorf("pod %s/%s is not scheduled", pod.Namespace, pod.Name)
		}

		node, err := testCtx.Kubeclient.CoreV1().Nodes().Get(
			context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if profile := node.Labels[mixedProfileLabel]; profile != expectedProfile {
			return nil, fmt.Errorf("pod %s/%s is on profile %q, expected %q",
				pod.Namespace, pod.Name, profile, expectedProfile)
		}
		domain := node.Labels[domainLabel]
		if domain == "" {
			return nil, fmt.Errorf("node %s has no topology domain label %s", node.Name, domainLabel)
		}

		if domainsByPartition[partition] == nil {
			domainsByPartition[partition] = sets.New[string]()
		}
		domainsByPartition[partition].Insert(domain)
		podCounts[partition]++
	}

	result := make(map[string]string, expectedPartitions)
	for partitionIndex := 0; partitionIndex < expectedPartitions; partitionIndex++ {
		partition := fmt.Sprint(partitionIndex)
		if podCounts[partition] != expectedPartitionSize {
			return nil, fmt.Errorf("partition %s has %d pods, expected %d",
				partition, podCounts[partition], expectedPartitionSize)
		}
		domains := domainsByPartition[partition]
		if domains.Len() != 1 {
			return nil, fmt.Errorf("partition %s spans topology domains %v", partition, domains.UnsortedList())
		}
		result[partition] = domains.UnsortedList()[0]
	}
	if len(domainsByPartition) != expectedPartitions {
		return nil, fmt.Errorf("found unexpected partitions: %v", sets.KeySet(domainsByPartition).UnsortedList())
	}
	return result, nil
}

func replaceMixedTopologyJobPod(
	testCtx *e2eutil.TestContext,
	job *batchv1alpha1.Job,
	partition string,
	expectedPods int,
) error {
	pods, err := testCtx.Kubeclient.CoreV1().Pods(job.Namespace).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	initialUIDs := sets.New[string]()
	var victim *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !metav1.IsControlledBy(pod, job) {
			continue
		}
		initialUIDs.Insert(string(pod.UID))
		if victim == nil && (partition == "" || pod.Labels[batchv1alpha1.TaskPartitionID] == partition) {
			victim = pod.DeepCopy()
		}
	}
	if victim == nil {
		if partition == "" {
			return fmt.Errorf("job %s has no controlled pod", job.Name)
		}
		return fmt.Errorf("job %s has no pod in partition %s", job.Name, partition)
	}
	if len(initialUIDs) != expectedPods {
		return fmt.Errorf("job %s has %d pods before replacement, expected %d",
			job.Name, len(initialUIDs), expectedPods)
	}
	if err := testCtx.Kubeclient.CoreV1().Pods(victim.Namespace).Delete(
		context.Background(), victim.Name, metav1.DeleteOptions{}); err != nil {
		return err
	}

	lastState := "replacement not observed"
	err = wait.PollUntilContextTimeout(context.Background(), 250*time.Millisecond, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			currentPods, err := testCtx.Kubeclient.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}

			controlledPods := 0
			readyPods := 0
			replacementFound := false
			victimStillExists := false
			for i := range currentPods.Items {
				pod := &currentPods.Items[i]
				if !metav1.IsControlledBy(pod, job) {
					continue
				}
				controlledPods++
				if pod.UID == victim.UID {
					victimStillExists = true
				}
				if !initialUIDs.Has(string(pod.UID)) {
					replacementFound = true
				}
				if pod.Status.Phase == v1.PodRunning && mixedTopologyPodReady(pod) {
					readyPods++
				}
			}
			lastState = fmt.Sprintf("controlled=%d ready=%d replacement=%t victimPresent=%t",
				controlledPods, readyPods, replacementFound, victimStillExists)
			return controlledPods == expectedPods && readyPods == expectedPods &&
				replacementFound && !victimStillExists, nil
		})
	if err != nil {
		return fmt.Errorf("wait for replacement of pod %s/%s: %w (%s)",
			victim.Namespace, victim.Name, err, lastState)
	}
	return nil
}

func restartVolcanoScheduler(client kubernetes.Interface) error {
	const schedulerSelector = "app=volcano-scheduler"

	pods, err := client.CoreV1().Pods(v1.NamespaceAll).List(
		context.Background(), metav1.ListOptions{LabelSelector: schedulerSelector})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no Volcano Scheduler pods found")
	}

	oldUIDs := sets.New[string]()
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !mixedTopologyPodReady(pod) {
			return fmt.Errorf("Volcano Scheduler pod %s/%s is not ready before restart", pod.Namespace, pod.Name)
		}
		oldUIDs.Insert(string(pod.UID))
		if err := client.CoreV1().Pods(pod.Namespace).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	lastState := "new scheduler pod not observed"
	err = wait.PollUntilContextTimeout(context.Background(), 500*time.Millisecond, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			currentPods, err := client.CoreV1().Pods(v1.NamespaceAll).List(
				ctx, metav1.ListOptions{LabelSelector: schedulerSelector})
			if err != nil {
				return false, err
			}

			readyNewPods := 0
			oldPodPresent := false
			for i := range currentPods.Items {
				pod := &currentPods.Items[i]
				if oldUIDs.Has(string(pod.UID)) {
					oldPodPresent = true
					continue
				}
				if pod.DeletionTimestamp == nil && mixedTopologyPodReady(pod) {
					readyNewPods++
				}
			}
			lastState = fmt.Sprintf("readyNew=%d expected=%d oldPresent=%t",
				readyNewPods, len(oldUIDs), oldPodPresent)
			return readyNewPods >= len(oldUIDs) && !oldPodPresent, nil
		})
	if err != nil {
		return fmt.Errorf("wait for Volcano Scheduler restart: %w (%s)", err, lastState)
	}
	return nil
}

func mixedTopologyPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func mixedTopologyDiscoveryConfig() string {
	return `networkTopologyDiscovery:
  - source: label
    enabled: true
    config:
      networkTopologyTypes:
        topologyshallow:
          nodeSelector:
            matchLabels:
              volcano.sh/e2e-topology-profile: shallow
          levels:
            - nodeLabel: volcano.sh/e2e-shallow-domain
              tierName: volcano.sh/hypercluster
            - nodeLabel: kubernetes.io/hostname
        topologydeep:
          nodeSelector:
            matchLabels:
              volcano.sh/e2e-topology-profile: deep
          levels:
            - nodeLabel: volcano.sh/e2e-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: volcano.sh/e2e-deep-hypernode
              tierName: volcano.sh/hypernode
            - nodeLabel: volcano.sh/e2e-deep-superpod
              tierName: volcano.sh/superpod
            - nodeLabel: kubernetes.io/hostname
`
}
