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

var _ = Describe("Network Topology Tests", func() {
	var ctx *e2eutil.TestContext

	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 8, // Need 8 nodes for the 3-tier topology
		})

		// Setup the 3-tier topology structure
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
			spec := &topologyv1alpha1.HyperNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: hn.name,
				},
				Spec: topologyv1alpha1.HyperNodeSpec{
					Tier: hn.tier,
					Members: []topologyv1alpha1.MemberSpec{
						{
							Type: topologyv1alpha1.MemberTypeNode,
							Selector: topologyv1alpha1.MemberSelector{
								ExactMatch: &topologyv1alpha1.ExactMatch{
									Name: hn.nodes[0],
								},
							},
						},
						{
							Type: topologyv1alpha1.MemberTypeNode,
							Selector: topologyv1alpha1.MemberSelector{
								ExactMatch: &topologyv1alpha1.ExactMatch{
									Name: hn.nodes[1],
								},
							},
						},
					},
				},
			}

			err := e2eutil.SetupHyperNode(ctx, spec)
			Expect(err).NotTo(HaveOccurred())
		}

		// Wait for all hypernodes to be ready
		By("Wait for hypernodes to be ready")
		for _, hn := range hyperNodes {
			Eventually(func() error {
				_, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), hn.name, metav1.GetOptions{})
				return err
			}, 30*time.Second, time.Second).Should(BeNil())
		}
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	Context("Hard Mode Tests", func() {
		It("Case 1.1: Schedule to node-0 and node-1 when resources are enough", func() {
			By("Create job that fits in s0's resources")
			tolerations := []v1.Toleration{
				{
					Key:      "kwok.x-k8s.io/node",
					Operator: v1.TolerationOpEqual,
					Value:    "fake",
					Effect:   v1.TaintEffectNoSchedule,
				},
			}

			// schedule pod to node-0 and node-1 to make sure the node-0 and node-1's binpack score is higher
			pod0 := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:        "pod-0",
				Node:        "kwok-node-0",
				Req:         e2eutil.CPU1Mem1,
				Tolerations: tolerations,
			})
			pod1 := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:        "pod-1",
				Node:        "kwok-node-1",
				Req:         e2eutil.CPU1Mem1,
				Tolerations: tolerations,
			})

			By("Wait for pod-0 and pod-1 to be ready")
			err := e2eutil.WaitPodReady(ctx, pod0)
			Expect(err).NotTo(HaveOccurred())
			err = e2eutil.WaitPodReady(ctx, pod1)
			Expect(err).NotTo(HaveOccurred())

			defer func() {
				ctx.Kubeclient.CoreV1().Pods(pod0.Namespace).Delete(context.TODO(), pod0.Name, metav1.DeleteOptions{})
				ctx.Kubeclient.CoreV1().Pods(pod1.Namespace).Delete(context.TODO(), pod1.Name, metav1.DeleteOptions{})
			}()

			job := &e2eutil.JobSpec{
				Name: "job-1",
				NetworkTopology: &batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
				Tasks: []e2eutil.TaskSpec{
					{
						Name:        "task-1",
						Img:         e2eutil.DefaultNginxImage,
						Req:         e2eutil.CPU2Mem2,
						Min:         2,
						Rep:         2,
						Tolerations: tolerations,
					},
				},
			}
			topologyJob := e2eutil.CreateJob(ctx, job)

			By("Wait for job running")
			err = e2eutil.WaitJobReady(ctx, topologyJob)
			Expect(err).NotTo(HaveOccurred())

			By("Verify pods are scheduled to kwok-node-0 and kwok-node-1")
			err = e2eutil.VerifyPodScheduling(ctx, topologyJob, []string{"kwok-node-0", "kwok-node-1"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Case 1.2: Schedule across node-0 and node-1 when node-0 resources are insufficient", func() {
			By("Create placeholder pod to consume resources on node-0")
			// Create fake nodes using kwok with the topology structure
			// Setup hypernode s0 containing node-0 and node-1
			// Create a job that requires more resources than available on node-0
			// Verify pods are scheduled across node-0 and node-1
			Skip("TODO: Implement this test")

		})

		It("Case 1.3: Pods remain pending when no hypernode has sufficient resources", func() {
			// Create fake nodes using kwok with the topology structure
			// Setup hypernodes s0-s3 with limited resources
			// Create a job requiring more resources than any hypernode can provide
			// Verify pods remain in pending state
			Skip("TODO: Implement this test")
		})
	})

	Context("Tier Tests", func() {
		It("Case 2: Schedule to tier 2 when tier 1 resources are insufficient", func() {
			// Create fake nodes using kwok with the topology structure
			// Setup tier 1 hypernodes with insufficient resources
			// Setup tier 2 hypernode s4 with some existing pods and sufficient resources
			// Create a job
			// Verify all pods are scheduled to hypernode s4
			Skip("TODO: Implement this test")
		})

		It("Case 3: Schedule to same hypernode when partial pods already running", func() {
			// Create fake nodes using kwok with the topology structure
			// Setup hypernode s4 with some pods from the job already running
			// Create remaining pods for the same job
			// Verify new pods are scheduled to the same hypernode s4
			Skip("TODO: Implement this test")
		})
	})

	Context("Soft Mode Tests", func() {
		It("Case 4: Schedule to single hypernode in soft mode", func() {
			// Create fake nodes using kwok with the topology structure
			// Setup tier 1 with insufficient resources
			// Setup tier 2 hypernodes s4 and s5 with sufficient resources
			// Create a job requiring 4 nodes worth of resources
			// Verify all pods are scheduled to either s4 or s5, but not both
			Skip("TODO: Implement this test")
		})
	})
})
