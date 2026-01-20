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

package schedulingaction

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Sharding E2E Test", func() {
	var ctx *e2eutil.TestContext

	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
		})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	It("Verify Hard Sharding Mode", func() {
		By("Check if at least 2 nodes exist")
		nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		if len(nodes.Items) < 2 {
			Skip("Not enough nodes for sharding E2E test")
		}

		node1 := nodes.Items[0].Name
		node2 := nodes.Items[1].Name

		By("Setup NodeShard for the volcano scheduler with only node1")
		// Default scheduler shard name is 'volcano'
		shardName := "volcano"
		nsh := &shardv1alpha1.NodeShard{
			ObjectMeta: metav1.ObjectMeta{
				Name: shardName,
			},
			Spec: shardv1alpha1.NodeShardSpec{
				NodesDesired: []string{node1},
			},
		}
		err = e2eutil.SetupNodeShard(ctx, nsh)
		Expect(err).NotTo(HaveOccurred())

		By("Updating NodeShard status to simulated active in-use nodes")
		// In a real environment, the sharding controller or scheduler would update this.
		// For an E2E test, we might need to wait for it or mock it if the controller is missing.
		nsh.Status.NodesInUse = []string{node1}
		err = e2eutil.UpdateNodeShardStatus(ctx, nsh)
		Expect(err).NotTo(HaveOccurred())

		By("Create a job that should only land on node1 in Hard Sharding Mode")
		// Note: This test assumes the scheduler is running in Hard Sharding Mode.
		// If it's not, it might land on node2, and we would need a way to detect/configure mode.

		job := &e2eutil.JobSpec{
			Name: "sharding-hard-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "t1",
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  1,
					Rep:  1,
				},
			},
		}

		// We add a node affinity for node2 to test if Hard Sharding correctly rejects it.
		// Wait, if we use NodeAffinity for node2, the scheduler in Hard Sharding mode (node1 only)
		// should keep the pod Pending because it can't find a node that satisfies BOTH.
		job.Tasks[0].Affinity = &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/hostname",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{node2},
								},
							},
						},
					},
				},
			},
		}

		vjob := e2eutil.CreateJob(ctx, job)

		By("Expect job to remain Pending if Hard Sharding is active and node2 is excluded")
		// This part is conditional on the environment setup.
		err = e2eutil.WaitJobStatePending(ctx, vjob)
		Expect(err).NotTo(HaveOccurred())

		// Now remove the affinity and see it scheduled on node1
		By("Delete and recreate job without node2 affinity")
		e2eutil.DeleteJob(ctx, vjob)
		job.Tasks[0].Affinity = nil
		vjob = e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, vjob)
		Expect(err).NotTo(HaveOccurred())

		By("Verify pod is scheduled on node1")
		Expect(e2eutil.VerifyPodScheduling(ctx, vjob, []string{node1})).NotTo(HaveOccurred())
	})

	It("Verify Soft Sharding Mode", func() {
		By("Check if at least 2 nodes exist")
		nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		if len(nodes.Items) < 2 {
			Skip("Not enough nodes for sharding E2E test")
		}

		node1 := nodes.Items[0].Name

		By("Setup NodeShard for the volcano scheduler with node1 in shard")
		shardName := "volcano-soft"
		nsh := &shardv1alpha1.NodeShard{
			ObjectMeta: metav1.ObjectMeta{
				Name: shardName,
			},
			Spec: shardv1alpha1.NodeShardSpec{
				NodesDesired: []string{node1},
			},
		}
		err = e2eutil.SetupNodeShard(ctx, nsh)
		Expect(err).NotTo(HaveOccurred())

		nsh.Status.NodesInUse = []string{node1}
		err = e2eutil.UpdateNodeShardStatus(ctx, nsh)
		Expect(err).NotTo(HaveOccurred())

		By("Create a job that should prefer node1 but can use node2 in Soft Sharding Mode")
		job := &e2eutil.JobSpec{
			Name: "sharding-soft-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "t1",
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  1,
					Rep:  1,
				},
			},
		}

		// To test "preference", we can occupy node1 and see if it goes to node2.
		// Or just verify it lands on node1 when both are free.
		vjob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, vjob)
		Expect(err).NotTo(HaveOccurred())

		By("Verify pod is scheduled on node1 (preferred)")
		Expect(e2eutil.VerifyPodScheduling(ctx, vjob, []string{node1})).NotTo(HaveOccurred())
	})
})
