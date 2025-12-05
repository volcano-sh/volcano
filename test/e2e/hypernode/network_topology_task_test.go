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

package hypernode

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Network Topology Task Tests", func() {
	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 8,
		})

		// Setup the 3-tier topology structure
		//                          s6
		//                   /              \
		//                 s4               s5
		//              /     \          /     \
		//            s0      s1       s2       s3
		//          /  \     /  \     /  \     /  \
		//         n0  n1   n2  n3   n4  n5   n6  n7
		//
		By("Setup 3-tier hypernodes")
		hyperNodes := []struct {
			name  string
			nodes []string
			tier  int
		}{
			// Tier-1
			{"s0", []string{"kwok-node-0", "kwok-node-1"}, 1},
			{"s1", []string{"kwok-node-2", "kwok-node-3"}, 1},
			{"s2", []string{"kwok-node-4", "kwok-node-5"}, 1},
			{"s3", []string{"kwok-node-6", "kwok-node-7"}, 1},
			// Tier-2
			{"s4", []string{"s0", "s1"}, 2},
			{"s5", []string{"s2", "s3"}, 2},
			// Tier-3
			{"s6", []string{"s4", "s5"}, 3},
		}

		for _, hn := range hyperNodes {
			hyperNodeType := topologyv1alpha1.MemberTypeHyperNode
			if hn.tier == 1 {
				hyperNodeType = topologyv1alpha1.MemberTypeNode
			}
			spec := &topologyv1alpha1.HyperNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: hn.name,
				},
				Spec: topologyv1alpha1.HyperNodeSpec{
					Tier: hn.tier,
					Members: []topologyv1alpha1.MemberSpec{
						{
							Type: hyperNodeType,
							Selector: topologyv1alpha1.MemberSelector{
								ExactMatch: &topologyv1alpha1.ExactMatch{
									Name: hn.nodes[0],
								},
							},
						},
						{
							Type: hyperNodeType,
							Selector: topologyv1alpha1.MemberSelector{
								ExactMatch: &topologyv1alpha1.ExactMatch{
									Name: hn.nodes[1],
								},
							},
						},
					},
				},
			}

			err = e2eutil.SetupHyperNode(ctx, spec)
			Expect(err).NotTo(HaveOccurred())
		}

		// Wait for all hypernodes to be ready
		By("Wait for hypernodes to be ready")
		for _, hn := range hyperNodes {
			Eventually(func() error {
				_, err = ctx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), hn.name, metav1.GetOptions{})
				return err
			}, 30*time.Second, time.Second).Should(BeNil())
		}
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	Context("Both the Job and PartitionPolicy network topology are hard mode tier 1 tests", func() {
		It("Case 4.1: Schedule to node-2 and node-3 when resources are enough", func() {
			By("Create job that fits in s1's resources")

			// schedule pod to s1 (node-2 and node-3) to make sure the s1's binpack score is higher
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-4-1-pod-0", Node: "kwok-node-2", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
				{Name: "case-4-1-pod-1", Node: "kwok-node-3", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-4-1",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-4-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pods are scheduled to s1")
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-2", "kwok-node-3"})).NotTo(HaveOccurred())
		})

		It("Case 4.2: Pods remain pending when no hypernode has sufficient resources", func() {
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-4-2-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-2-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-4-2",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-4-2",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         2,
						Rep:         2,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   1,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}

			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 2)).NotTo(HaveOccurred())
		})

		It("Case 4.3: Pods remain pending when hypernode has sufficient resources but does not meet the topological requirements", func() {
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-4-3-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-3-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-4-3-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-3-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-4-3-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-3-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-4-3-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-4-3-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			job := &e2eutil.JobSpec{
				Name: "job-4-3",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-4-3",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         2,
						Rep:         2,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   1,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 2)).NotTo(HaveOccurred())
		})
	})

	Context("Job Hard Mode Tier 2 and PartitionPolicy Hard Mode Tier 1 Tests", func() {
		It("Case 5.1: Schedule to node-2 and node-3 when resources are enough", func() {
			By("Create job that fits in s1's resources")

			// schedule pod to s1 (node-2 and node-3) to make sure the s1's binpack score is higher
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-5-1-pod-0", Node: "kwok-node-2", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
				{Name: "case-5-1-pod-1", Node: "kwok-node-3", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-1",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pods are scheduled to s1")
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-2", "kwok-node-3"})).NotTo(HaveOccurred())
		})

		It("Case 5.2: Scheduling is performed in groups when resources are enough", func() {
			By(" Create job that fits in s0, s1, s2 and s3's resources")
			// s0, s1, s2 and s3 can each only host 3 pods, when creating job group replicas with a count of 4,
			// and grouping them in pairs with affinity, ultimately each tier 1 hypernode can only have 2 pods.
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-5-2-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU3Mem3, Tolerations: tolerations},
				{Name: "case-5-2-pod-1", Node: "kwok-node-2", Req: e2eutil.CPU3Mem3, Tolerations: tolerations},
				{Name: "case-5-2-pod-2", Node: "kwok-node-4", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-2-pod-3", Node: "kwok-node-5", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-2-pod-4", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-2-pod-5", Node: "kwok-node-7", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-2",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-2",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU3Mem3,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify each tier 1 hypernode having three pods")
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s0", 3)).NotTo(HaveOccurred())
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s1", 3)).NotTo(HaveOccurred())
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s4", 6)).NotTo(HaveOccurred())
		})

		It("Case 5.3: pod of job will be rescheduled to same hypernode when be killed", func() {
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-5-3-pod-0", Node: "kwok-node-6", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
				{Name: "case-5-3-pod-1", Node: "kwok-node-7", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-3",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-3",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}

			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())

			jobPods := e2eutil.GetTasksOfJob(ctx, topologyJob)
			Expect(len(jobPods)).To(Equal(4))

			By("Kill pod of job")
			e2eutil.DeletePod(ctx, jobPods[0])

			By("Wait for job running again")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pod of job is scheduled to same hypernode")
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-6", "kwok-node-7"})).NotTo(HaveOccurred())
		})

		It("Case 5.4: Schedule to s1 when s0's resources are insufficient and s1's score is higher", func() {
			By("Create job that fits in s0 and s1's resources")
			// s0 has insufficient resources, s2 and s3 has enough resources, s1 has enough resources and higher score
			// pods should be scheduled to s1

			podSpecs := []e2eutil.PodSpec{
				// allocate to s0(kwok-node-0, kwok-node-1) to make s0 has insufficient resources
				{Name: "case-5-4-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-4-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				// allocate to s1(kwok-node-2, kwok-node-3) to make s1 has enough resources and higher score
				{Name: "case-5-4-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
				{Name: "case-5-4-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-4",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-4",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}

			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pods are scheduled to s1")
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-2", "kwok-node-3"})).NotTo(HaveOccurred())
		})

		It("Case 5.5: Pods remain pending when no hypernode has sufficient resources", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-5-5-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-5-5-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-5",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-5",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         2,
						Rep:         2,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   1,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}

			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 2)).NotTo(HaveOccurred())
		})

		It("Case 5.6: Pods remain pending when hypernode has sufficient resources but does not meet the topological requirements", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-5-6-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-5-6-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-5-6",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-5-6",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         2,
						Rep:         2,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   1,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}

			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 2)).NotTo(HaveOccurred())
		})
	})

	Context("Job Hard Mode Tier 2 and PartitionPolicy Soft Mode Tests", func() {
		It("Case 6.1: Schedule to node-2 and node-3 when resources are enough", func() {
			By("Create job that fits in s1's resources")

			// schedule pod to s1 (node-2 and node-3) to make sure the s1's binpack score is higher
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-6-1-pod-0", Node: "kwok-node-2", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
				{Name: "case-6-1-pod-1", Node: "kwok-node-3", Req: e2eutil.CPU1Mem1, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-6-1",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-6-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode: batchv1alpha1.SoftNetworkTopologyMode,
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pods are scheduled to s1")
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-2", "kwok-node-3"})).NotTo(HaveOccurred())
		})

		It("Case 6.2: Scheduling is performed in groups when resources are enough", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")
			// make sure tier 1 has insufficient resources, and each left node has enough resource for just three pod,
			// set kwok-node-4 and kwok-node-6 has higher score of binpack to make sure our pod will be scheduled to it,
			// finally task will be scheduled to tier 2 s5
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-6-2-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU3Mem3, Tolerations: tolerations},
				{Name: "case-6-2-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU3Mem3, Tolerations: tolerations},
				{Name: "case-6-2-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU3Mem3, Tolerations: tolerations},
				{Name: "case-6-2-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU5Mem5, Tolerations: tolerations},
				{Name: "case-6-2-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-2-pod-5", Node: "kwok-node-6", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-6-2",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-6-2",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode: batchv1alpha1.SoftNetworkTopologyMode,
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify pods are scheduled to tier 2 s5")
			expectHyperNode, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), "s5", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to find s5 HyperNode")
			expectNodeList := e2eutil.GetNodesOfHyperNode(ctx, expectHyperNode, make([]string, 0))
			Expect(e2eutil.VerifyPodScheduling(ctx, topologyJob, expectNodeList)).NotTo(HaveOccurred())
		})

		It("Case 6.3: Pods remain pending when hypernode has sufficient resources but does not meet the topological requirements", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")

			podSpecs := []e2eutil.PodSpec{
				{Name: "case-6-3-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-6-3-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-3-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-3-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-3-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-6-3-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-3-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-6-3-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			job := &e2eutil.JobSpec{
				Name: "job-6-3",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-6-3",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode: batchv1alpha1.SoftNetworkTopologyMode,
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 2)).NotTo(HaveOccurred())
		})
	})

	Context("Job Soft Mode Tier 2 and PartitionPolicy Hard Mode Tier 1 Tests", func() {
		It("Case 7.1: Scheduling is performed in groups when resources are enough", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")
			// s0, s1, s2 and s3 can each only host 3 pods, when creating job group replicas with a count of 4,
			// and grouping them in pairs with affinity, ultimately each tier 1 hypernode can only have 2 pods.
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-7-1-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-7-1-pod-1", Node: "kwok-node-4", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-7-1-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-7-1-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-7-1-pod-4", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-7-1-pod-5", Node: "kwok-node-7", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-7-1",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode: batchv1alpha1.SoftNetworkTopologyMode,
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-7-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU3Mem3,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify each tier 1 hypernode having three pods")
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s0", 3)).NotTo(HaveOccurred())
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s2", 3)).NotTo(HaveOccurred())
		})

		It("Case 7.2: Pods remain pending when hypernode has sufficient resources but does not meet the topological requirements", func() {
			By("Create job that fits in s0, s1, s2 and s3's resources")
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-7-2-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-1", Node: "kwok-node-1", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-4", Node: "kwok-node-4", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-5", Node: "kwok-node-5", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-6", Node: "kwok-node-6", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
				{Name: "case-7-2-pod-7", Node: "kwok-node-7", Req: e2eutil.CPU2Mem2, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			job := &e2eutil.JobSpec{
				Name: "job-7-2",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode: batchv1alpha1.SoftNetworkTopologyMode,
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-7-2",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU4Mem4,
						Min:         3,
						Rep:         3,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 3,
							PartitionSize:   1,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Verify pods are pending")
			Expect(e2eutil.WaitTaskPhase(ctx, topologyJob, []v1.PodPhase{v1.PodPending}, 3)).NotTo(HaveOccurred())
		})
	})

	Context("No Job Affinity Only PartitionPolicy Hard Mode Tier 1 Tests", func() {
		It("Case 8.1: Scheduling is performed in groups when resources are enough", func() {
			By(" Create job that fits in s0, s1, s2 and s3's resources")
			// s0, s1, s2 and s3 can each only host 3 pods, when creating job group replicas with a count of 4,
			// and grouping them in pairs with affinity, ultimately each tier 1 hypernode can only have 2 pods.
			podSpecs := []e2eutil.PodSpec{
				{Name: "case-8-1-pod-0", Node: "kwok-node-0", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-8-1-pod-1", Node: "kwok-node-4", Req: e2eutil.CPU4Mem4, Tolerations: tolerations},
				{Name: "case-8-1-pod-2", Node: "kwok-node-2", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-8-1-pod-3", Node: "kwok-node-3", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-8-1-pod-4", Node: "kwok-node-6", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
				{Name: "case-8-1-pod-5", Node: "kwok-node-7", Req: e2eutil.CPU6Mem6, Tolerations: tolerations},
			}

			pods := make([]*v1.Pod, len(podSpecs))
			for i, podSpec := range podSpecs {
				pods[i] = e2eutil.CreatePod(ctx, podSpec)
			}

			defer func() {
				for _, pod := range pods {
					e2eutil.DeletePod(ctx, pod)
				}
			}()

			By("Wait for all pods to be ready")
			for _, pod := range pods {
				Expect(e2eutil.WaitPodReady(ctx, pod)).NotTo(HaveOccurred())
			}

			job := &e2eutil.JobSpec{
				Name: "job-8-1",
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-8-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU3Mem3,
						Min:         4,
						Rep:         4,
						Tolerations: tolerations,
						PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
							TotalPartitions: 2,
							PartitionSize:   2,
							NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
								Mode:               batchv1alpha1.HardNetworkTopologyMode,
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			defer func() {
				By("Delete job")
				e2eutil.DeleteJob(ctx, topologyJob)
			}()

			By("Wait for job running")
			Expect(e2eutil.WaitJobReady(ctx, topologyJob)).NotTo(HaveOccurred())

			By("Verify each tier 1 hypernode having three pods")
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s0", 3)).NotTo(HaveOccurred())
			Expect(e2eutil.VerifyHyperNodeScheduling(ctx, "s2", 3)).NotTo(HaveOccurred())
		})
	})
})
