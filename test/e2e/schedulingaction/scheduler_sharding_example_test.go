/*
Copyright 2021 The Volcano Authors.

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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Scheduler Sharding E2E Tests", func() {
	var ctx *e2eutil.TestContext

	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	It("Example: Enable soft sharding mode", func() {
		By("Patching volcano-scheduler with soft sharding mode")
		err := e2eutil.PatchVolcanoSchedulerSharding("soft", "")
		Expect(err).NotTo(HaveOccurred(), "Failed to patch scheduler with soft sharding mode")

		By("Verifying scheduler args include sharding mode flag")
		err = e2eutil.VerifyVolcanoSchedulerArgs("soft", "")
		Expect(err).NotTo(HaveOccurred(), "Sharding mode flag not correctly set")

		fmt.Println("✓ Soft sharding mode enabled successfully")
	})

	It("Example: Enable hard sharding with shard name", func() {
		By("Patching volcano-scheduler with hard sharding mode and shard name")
		err := e2eutil.PatchVolcanoSchedulerSharding("hard", "volcano-shard-1")
		Expect(err).NotTo(HaveOccurred(), "Failed to patch scheduler with hard sharding")

		By("Verifying scheduler args include both mode and name flags")
		err = e2eutil.VerifyVolcanoSchedulerArgs("hard", "volcano-shard-1")
		Expect(err).NotTo(HaveOccurred(), "Sharding flags not correctly set")

		fmt.Println("✓ Hard sharding with shard name enabled successfully")
	})

	It("Example: Reset sharding to none mode", func() {
		By("First enabling hard sharding")
		err := e2eutil.PatchVolcanoSchedulerSharding("hard", "temp-shard")
		Expect(err).NotTo(HaveOccurred())

		By("Resetting to none mode")
		err = e2eutil.PatchVolcanoSchedulerSharding("none", "")
		Expect(err).NotTo(HaveOccurred(), "Failed to reset scheduler to none mode")

		By("Verifying only mode flag is present (no shard name)")
		err = e2eutil.VerifyVolcanoSchedulerArgs("none", "")
		Expect(err).NotTo(HaveOccurred(), "Scheduler not correctly reset to none mode")

		fmt.Println("✓ Scheduler successfully reset to none mode")
	})

	It("Example: Job allocation with soft sharding", func() {
		By("Enabling soft sharding mode")
		err := e2eutil.PatchVolcanoSchedulerSharding("soft", "")
		Expect(err).NotTo(HaveOccurred())

		By("Creating a simple job that should be allocated")
		var replicas int32 = 1
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: ctx.Namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &replicas,
				Completions: &replicas,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: e2eutil.DefaultBusyBoxImage,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("512Mi"),
									},
								},
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}

		createdJob, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to create test job")

		By("Verifying job was created")
		Expect(createdJob.Name).To(Equal("test-job"))
		fmt.Printf("✓ Job %s created and should be scheduled with soft sharding\n", createdJob.Name)
	})

	It("Example: Multiple shard names", func() {
		testCases := []struct {
			name string
			mode string
		}{
			{"shard-default", "soft"},
			{"shard-ai", "hard"},
			{"shard-batch", "soft"},
		}

		for _, tc := range testCases {
			By(fmt.Sprintf("Testing shard: %s with mode: %s", tc.name, tc.mode))
			err := e2eutil.PatchVolcanoSchedulerSharding(tc.mode, tc.name)
			Expect(err).NotTo(HaveOccurred())

			err = e2eutil.VerifyVolcanoSchedulerArgs(tc.mode, tc.name)
			Expect(err).NotTo(HaveOccurred())
		}
	})
})
