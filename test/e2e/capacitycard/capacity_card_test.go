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

package capacitycard

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

// Generate a random suffix to ensure resource name uniqueness
func generateRandomSuffix() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d", r.Intn(10000))
}

// Global constant definitions
const (
	// Actual card types in the cluster
	CardTypeTeslaK80 = "Tesla-K80"
	CardTypeRTX4090  = "NVIDIA-GeForce-RTX-4090"
	CardTypeH800     = "NVIDIA-H800"
	// Polling configuration
	PollInterval       = 500 * time.Millisecond
	QueueReadyTimeout  = 30 * time.Second
	JobProcessTimeout  = 60 * time.Second
	CleanupGracePeriod = 10 * time.Second
)

// Initialize random number generator
var _ = BeforeSuite(func() {
	rand.Seed(time.Now().UnixNano())
})

var _ = Describe("Capacity Card E2E Test", func() {
	Context("Capacity Card - Basic", func() {
		// Test 1: Basic queue capacity management test
		It("Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 1: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-1-%s", randomSuffix),
			})
			fmt.Printf("Test 1: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("capacity-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 1: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 1: Queue %s created successfully, %s card quota is 4\n", queueSpec.Name, CardTypeTeslaK80)

			// Clean up queue using e2eutil
			defer func() {
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("Test 1: Queue %s cleaned up\n", queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 1: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 1: Queue %s status is now open\n", queueSpec.Name)
		})
	})

	Context("Capacity Card - VCJob", func() {
		// Test 2: Job enqueueable check - success case
		It("Job Enqueueable Check - Success", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 2: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-2-%s", randomSuffix),
			})
			fmt.Printf("Test 2: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("enqueue-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("8"),
					v1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 2: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 2: Queue %s created successfully\n", queueSpec.Name)

			// Clean up queue using e2eutil
			defer func() {
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("Test 2: Queue %s cleaned up\n", queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 2: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 2: Queue %s status is now open\n", queueSpec.Name)

			// Create a job with card request
			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("enqueue-success-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
					},
				},
			}

			// Add job card request annotation to JobSpec
			jobSpec.Annotations = map[string]string{
				"volcano.sh/card.request": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
			}

			// Create job directly
			fmt.Printf("Test 2: Starting to create job %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("Test 2: Job %s created successfully\n", job.Name)

			// Clean up resources
			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("Test 2: Job %s cleaned up\n", job.Name)
			}()

			// Wait for job to be ready
			fmt.Printf("Test 2: Waiting for job to be ready\n")
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "Job failed to become ready within timeout")
			fmt.Printf("Test 2: Job %s is now ready\n", job.Name)
		})

		// Test 3: Task card resource allocation test
		It("Task Card Resource Allocation", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 3: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-3-%s", randomSuffix),
			})
			fmt.Printf("Test 3: Test context initialized, namespace %s created successfully\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("allocation-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 3: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 3: Queue %s created successfully\n", queueSpec.Name)

			defer func() {
				// Clean up queue using e2eutil
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("Test 3: Queue %s cleaned up\n", queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 3: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 3: Queue %s status is now open\n", queueSpec.Name)

			// Create a job with task-level card name request, set card name annotation to TaskSpec
			taskSpecs := []e2eutil.TaskSpec{
				{
					Name: "card-task",
					Min:  1,
					Rep:  1,
					Img:  e2eutil.DefaultNginxImage,
					Req: v1.ResourceList{
						v1.ResourceCPU:                    resource.MustParse("1"),
						v1.ResourceMemory:                 resource.MustParse("1Gi"),
						v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Limit: v1.ResourceList{
						v1.ResourceCPU:                    resource.MustParse("1"),
						v1.ResourceMemory:                 resource.MustParse("1Gi"),
						v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Labels: map[string]string{"card-test": "true"},
					Annotations: map[string]string{
						"volcano.sh/card.name": CardTypeTeslaK80,
					},
				},
			}

			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("task-card-test-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: taskSpecs,
			}

			// Create job directly
			fmt.Printf("Test 3: Starting to create job %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("Test 3: Job %s created successfully\n", job.Name)

			// Clean up resources
			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("Test 3: Job %s cleaned up\n", job.Name)
			}()

			// Wait for job to be ready
			fmt.Printf("Test 3: Waiting for job to be ready\n")
			err := e2eutil.WaitJobReady(ctx, job)
			Expect(err).NotTo(HaveOccurred(), "Job failed to become ready within timeout")
			fmt.Printf("Test 3: Job %s is now ready\n", job.Name)
		})

		// Test 4: Card resource quota exceeded test
		It("Card Resource Quota Exceeded", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 4: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("capacity-card-test-4-%s", randomSuffix),
			})
			fmt.Printf("Test 4: Test context initialized, namespace %s created successfully\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with limited card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("quota-limit-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 4: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 4: Queue %s created successfully, %s card quota is 2\n", queueSpec.Name, CardTypeTeslaK80)

			defer func() {
				// Clean up queue using e2eutil
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
				fmt.Printf("Test 4: Queue %s cleaned up\n", queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 4: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 4: Queue %s status is now open\n", queueSpec.Name)

			// Create first job, using part of card quota
			job1Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("first-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80),
				},
			}

			fmt.Printf("Test 4: Starting to create first job %s\n", job1Spec.Name)
			job1 := e2eutil.CreateJob(ctx, job1Spec)
			fmt.Printf("Test 4: Job 1 %s created successfully, requesting %s card resource as 1\n", job1.Name, CardTypeTeslaK80)
			fmt.Printf("Test 4: Job 1 %s validated successfully\n", job1.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job1)
				fmt.Printf("Test 4: Job 1 %s cleaned up\n", job1.Name)
			}()

			// Create second job, using remaining card quota
			job2Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("second-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80),
				},
			}

			fmt.Printf("Test 4: Starting to create second job %s\n", job2Spec.Name)
			job2 := e2eutil.CreateJob(ctx, job2Spec)
			fmt.Printf("Test 4: Job 2 %s created successfully, requesting %s card resource as 1\n", job2.Name, CardTypeTeslaK80)
			fmt.Printf("Test 4: Job 2 %s validated successfully\n", job2.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job2)
				fmt.Printf("Test 4: Job 2 %s cleaned up\n", job2.Name)
			}()

			// Create third job, attempting to use card resource exceeding remaining quota
			job3Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("third-quota-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80), // Exceeds remaining queue quota
				},
			}

			fmt.Printf("Test 4: Starting to create third job %s\n", job3Spec.Name)
			job3 := e2eutil.CreateJob(ctx, job3Spec)
			fmt.Printf("Test 4: Job 3 %s created successfully, requesting %s card resource as 3 (exceeding remaining quota)\n", job3.Name, CardTypeTeslaK80)
			fmt.Printf("Test 4: Job 3 %s validated successfully\n", job3.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job3)
				fmt.Printf("Test 4: Job 3 %s cleaned up\n", job3.Name)
			}()

			// Wait for job 1 to be ready (job 1 should run successfully as it doesn't exceed quota)
			fmt.Printf("Test 4: Waiting for job 1 to be ready\n")
			err := e2eutil.WaitJobReady(ctx, job1)
			Expect(err).NotTo(HaveOccurred(), "Job 1 failed to become ready within timeout")
			fmt.Printf("Test 4: Job 1 %s is now ready\n", job1.Name)

			// Wait for job 2 to be ready (job 2 should run successfully as it doesn't exceed quota)
			fmt.Printf("Test 4: Waiting for job 2 to be ready\n")
			err = e2eutil.WaitJobReady(ctx, job2)
			Expect(err).NotTo(HaveOccurred(), "Job 2 failed to become ready within timeout")
			fmt.Printf("Test 4: Job 2 %s is now ready\n", job2.Name)

			// For job 3 (exceeding quota), wait for some time then check status
			fmt.Printf("Test 4: Waiting for some time to check job 3 status (expected to not be fully ready due to insufficient quota)\n")
			time.Sleep(JobProcessTimeout / 2)
			e2eutil.CheckJobSchedulingFailed(ctx, job3)
		})

		// Test 5: RTX4090 card queue capacity management test
		It("RTX4090 Card Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 5: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("rtx4090-card-test-%s", randomSuffix),
			})
			fmt.Printf("Test 5: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with RTX4090 card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("rtx4090-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeRTX4090),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 5: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 5: Queue %s created successfully, %s card quota is 4\n", queueSpec.Name, CardTypeRTX4090)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 5: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 5: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 5: Queue %s status is now open\n", queueSpec.Name)
		})

		// Test 6: H800 card queue capacity management test
		It("H800 Card Queue Capacity Management", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 6: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("h800-card-test-%s", randomSuffix),
			})
			fmt.Printf("Test 6: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with H800 card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("h800-test-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeH800),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 6: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 6: Queue %s created successfully, %s card quota is 4\n", queueSpec.Name, CardTypeH800)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 6: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 6: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 6: Queue %s status is now open\n", queueSpec.Name)
		})

		// Test 7: Multiple card types mixed quota test
		It("Multiple Card Types Mixed Quota Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 7: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("multi-card-test-%s", randomSuffix),
			})
			fmt.Printf("Test 7: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with multiple card quotas
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("multi-card-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2, "%s": 2, "%s": 2}`,
						CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 7: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 7: Queue %s created successfully, mixed card quota configured: %s:2, %s:2, %s:2\n",
				queueSpec.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 7: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 7: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 7: Queue %s status is now open\n", queueSpec.Name)
		})

		// Test 8: Multiple card types job request test
		It("Multiple Card Types Job Request Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 8: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("multi-card-job-test-%s", randomSuffix),
			})
			fmt.Printf("Test 8: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with multiple card quotas
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("multi-card-job-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 8, "%s": 8, "%s": 8}`,
						CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 8: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 8: Queue %s created successfully, mixed card quota configured: %s:8, %s:8, %s:8\n",
				queueSpec.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 8: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 8: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 8: Queue %s status is now open\n", queueSpec.Name)

			// Create a job requesting multiple card types
			job1Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("multi-card-job-1-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task-1",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
					{
						Name: "task-2",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 8, "%s": 8}`,
						CardTypeTeslaK80, CardTypeRTX4090),
				},
			}

			// Create job directly (without using PodGroup)
			fmt.Printf("Test 8: Starting to create multiple card type job %s\n", job1Spec.Name)
			job1 := e2eutil.CreateJob(ctx, job1Spec)
			fmt.Printf("Test 8: Multiple card type job %s created successfully, requesting: %s:8, %s:8, %s:0\n",
				job1.Name, CardTypeTeslaK80, CardTypeRTX4090, CardTypeH800)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job1)
				fmt.Printf("Test 8: Job %s cleaned up\n", job1.Name)
			}()

			// Wait for job to be ready
			fmt.Printf("Test 8: Waiting for job to be ready\n")
			err := e2eutil.WaitJobReady(ctx, job1)
			Expect(err).NotTo(HaveOccurred(), "Job failed to become ready within timeout")
			fmt.Printf("Test 8: Job %s is now ready\n", job1.Name)

			// Create third job, attempting to use card resource exceeding remaining quota
			job2Spec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("multi-card-job-2-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task-1",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeTeslaK80), // Exceeds remaining queue quota
				},
			}

			fmt.Printf("Test 8: Starting to create third job %s\n", job2Spec.Name)
			job2 := e2eutil.CreateJob(ctx, job2Spec)
			fmt.Printf("Test 8: Job 2 %s created successfully, requesting %s card resource as 1 (exceeding remaining quota)\n", job2.Name, CardTypeTeslaK80)
			fmt.Printf("Test 8: Job 2 %s validated successfully\n", job2.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job2)
				fmt.Printf("Test 8: Job %s cleaned up\n", job2.Name)
			}()

			// For job 2 (exceeding quota), wait for some time then check status
			fmt.Printf("Test 8: Waiting for some time to check job 2 status (expected to not be fully ready due to insufficient quota)\n")
			time.Sleep(JobProcessTimeout / 2)
			e2eutil.CheckJobSchedulingFailed(ctx, job2)
		})

		// Test 9: Card type based priority scheduling test
		It("Card Type Based Priority Scheduling Test", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 9: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("priority-card-test-%s", randomSuffix),
			})
			fmt.Printf("Test 9: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create high priority queue - for H800 cards
			priorityQueueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("high-priority-queue-%s", randomSuffix),
				Weight: 100, // High weight
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeH800),
				},
			}

			// Create high priority queue using e2eutil function
			fmt.Printf("Test 9: Starting to create high priority queue %s\n", priorityQueueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, priorityQueueSpec)
			fmt.Printf("Test 9: High priority queue %s created successfully, %s card quota is 4\n", priorityQueueSpec.Name, CardTypeH800)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 9: Cleaning up high priority queue %s\n", priorityQueueSpec.Name)
				e2eutil.DeleteQueue(ctx, priorityQueueSpec.Name)
			}()

			// Create normal priority queue - for RTX4090 cards
			normalQueueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("normal-priority-queue-%s", randomSuffix),
				Weight: 10, // Low weight
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeRTX4090),
				},
			}

			// Create normal priority queue using e2eutil function
			fmt.Printf("Test 9: Starting to create normal priority queue %s\n", normalQueueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, normalQueueSpec)
			fmt.Printf("Test 9: Normal priority queue %s created successfully, %s card quota is 4\n", normalQueueSpec.Name, CardTypeRTX4090)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 9: Cleaning up normal priority queue %s\n", normalQueueSpec.Name)
				e2eutil.DeleteQueue(ctx, normalQueueSpec.Name)
			}()

			// Wait for high priority queue status to become open
			fmt.Printf("Test 9: Waiting for high priority queue %s status to become open\n", priorityQueueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), priorityQueueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "High priority queue failed to become open within timeout")

			// Wait for normal priority queue status to become open
			fmt.Printf("Test 9: Waiting for normal priority queue %s status to become open\n", normalQueueSpec.Name)
			queueOpenErr = e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), normalQueueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Normal priority queue failed to become open within timeout")

			fmt.Printf("Test 9: Both queues status are now open\n")

			// Create high priority job (H800 card)
			highPriorityJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("high-priority-job-%s", randomSuffix),
				Queue: priorityQueueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeH800,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeH800),
					"volcano.sh/job.priority": "high",
				},
			}

			// Create high priority job directly (without using PodGroup)
			fmt.Printf("Test 9: Starting to create high priority job %s\n", highPriorityJobSpec.Name)
			highPriorityJob := e2eutil.CreateJob(ctx, highPriorityJobSpec)
			fmt.Printf("Test 9: High priority job %s created successfully, requesting %s card resource\n", highPriorityJob.Name, CardTypeH800)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, highPriorityJob)
				fmt.Printf("Test 9: High priority job %s cleaned up\n", highPriorityJob.Name)
			}()

			// Create normal priority job (RTX4090 card)
			normalPriorityJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("normal-priority-job-%s", randomSuffix),
				Queue: normalQueueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeRTX4090),
					"volcano.sh/job.priority": "normal",
				},
			}

			// Create normal priority job directly (without using PodGroup)
			fmt.Printf("Test 9: Starting to create normal priority job %s\n", normalPriorityJobSpec.Name)
			normalPriorityJob := e2eutil.CreateJob(ctx, normalPriorityJobSpec)
			fmt.Printf("Test 9: Normal priority job %s created successfully, requesting %s card resource\n", normalPriorityJob.Name, CardTypeRTX4090)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, normalPriorityJob)
				fmt.Printf("Test 9: Normal priority job %s cleaned up\n", normalPriorityJob.Name)
			}()

			// Wait for high priority job to be ready
			fmt.Printf("Test 9: Waiting for high priority job to be ready\n")
			err := e2eutil.WaitJobReady(ctx, highPriorityJob)
			Expect(err).NotTo(HaveOccurred(), "High priority job failed to become ready within timeout")
			fmt.Printf("Test 9: High priority job %s is now ready\n", highPriorityJob.Name)

			// Wait for normal priority job to be ready
			fmt.Printf("Test 9: Waiting for normal priority job to be ready\n")
			err = e2eutil.WaitJobReady(ctx, normalPriorityJob)
			Expect(err).NotTo(HaveOccurred(), "Normal priority job failed to become ready within timeout")
			fmt.Printf("Test 9: Normal priority job %s is now ready\n", normalPriorityJob.Name)
		})

		// Test 10: Mixed jobs test with unlimited CPU memory
		It("Mixed Jobs with CardUnlimitedCpuMemory", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 10: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("mixed-jobs-test-%s", randomSuffix),
			})
			fmt.Printf("Test 10: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with card quota and cardUnlimitedCpuMemory=true
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("mixed-jobs-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 4}`, CardTypeTeslaK80),
				},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 10: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 10: Queue %s created successfully, configured Tesla-K80 card quota 4, unlimited CPU/memory\n", queueSpec.Name)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 10: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 10: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 10: Queue %s status is now open\n", queueSpec.Name)

			// 1. Create a CPU-only job
			cpuJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("cpu-only-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "cpu-task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}

			// Create CPU-only job
			fmt.Printf("Test 10: Starting to create CPU-only job %s\n", cpuJobSpec.Name)
			cpuJob := e2eutil.CreateJob(ctx, cpuJobSpec)
			fmt.Printf("Test 10: CPU-only job %s created successfully\n", cpuJob.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, cpuJob)
				fmt.Printf("Test 10: CPU-only job %s cleaned up\n", cpuJob.Name)
			}()

			// Wait for CPU-only job to be ready
			fmt.Printf("Test 10: Waiting for CPU-only job to be ready\n")
			err := e2eutil.WaitJobReady(ctx, cpuJob)
			Expect(err).NotTo(HaveOccurred(), "CPU-only job failed to become ready within timeout")
			fmt.Printf("Test 10: CPU-only job %s is now ready\n", cpuJob.Name)

			// 2. Create a job with card request
			cardJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("card-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "card-task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeTeslaK80,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 2}`, CardTypeTeslaK80),
				},
			}

			// Create job with card request
			fmt.Printf("Test 10: Starting to create job with card request %s\n", cardJobSpec.Name)
			cardJob := e2eutil.CreateJob(ctx, cardJobSpec)
			fmt.Printf("Test 10: Job with card request %s created successfully, requesting 2 Tesla-K80 cards\n", cardJob.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, cardJob)
				fmt.Printf("Test 10: Job with card request %s cleaned up\n", cardJob.Name)
			}()

			// Wait for job with card request to be ready
			fmt.Printf("Test 10: Waiting for job with card request to be ready\n")
			err = e2eutil.WaitJobReady(ctx, cardJob)
			Expect(err).NotTo(HaveOccurred(), "Job with card request failed to become ready within timeout")
			fmt.Printf("Test 10: Job with card request %s is now ready\n", cardJob.Name)

			// 3. Create an excess CPU-only job
			overCpuJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("over-cpu-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "over-cpu-task",
						Min:  1,
						Rep:  2,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}

			// Create excess CPU-only job
			fmt.Printf("Test 10: Starting to create excess CPU-only job %s\n", overCpuJobSpec.Name)
			overCpuJob := e2eutil.CreateJob(ctx, overCpuJobSpec)
			fmt.Printf("Test 10: Excess CPU-only job %s created successfully\n", overCpuJob.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, overCpuJob)
				fmt.Printf("Test 10: Excess CPU-only job %s cleaned up\n", overCpuJob.Name)
			}()

			// Wait for excess CPU-only job to fail scheduling
			fmt.Printf("Test 10: Waiting for excess CPU-only job to fail scheduling\n")
			time.Sleep(JobProcessTimeout / 2)
			e2eutil.CheckJobSchedulingFailed(ctx, overCpuJob)
		})

		// Test 11: No resource quota queue scheduling restriction test
		It("No Resource Quota Queue Scheduling Restriction", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 11: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("no-quota-test-%s", randomSuffix),
			})
			fmt.Printf("Test 11: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue without any resource quota (no card quota, CPU and memory guarantee)
			queueSpec := &e2eutil.QueueSpec{
				Name:        fmt.Sprintf("no-quota-queue-%s", randomSuffix),
				Weight:      10,
				Annotations: map[string]string{},
			}

			// Create queue using e2eutil function
			fmt.Printf("Test 11: Starting to create queue without resource quota %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 11: Queue without resource quota %s created successfully\n", queueSpec.Name)

			// Clean up resources
			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 11: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 11: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 11: Queue %s status is now open\n", queueSpec.Name)

			// Create a job, attempting to schedule to queue without resource quota
			jobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("test-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("1"),
							v1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			// Create job directly (without using PodGroup), and set card request annotations
			fmt.Printf("Test 11: Starting to create test job %s\n", jobSpec.Name)
			job := e2eutil.CreateJob(ctx, jobSpec)
			fmt.Printf("Test 11: Test job %s created successfully, attempting to schedule to queue without resource quota\n", job.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, job)
				fmt.Printf("Test 11: Job %s cleaned up\n", job.Name)
			}()

			// Create a card job, attempting to schedule to queue without resource quota
			cardJobSpec := &e2eutil.JobSpec{
				Name:  fmt.Sprintf("test-card-job-%s", randomSuffix),
				Queue: queueSpec.Name,
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "card-task",
						Min:  1,
						Rep:  1,
						Img:  e2eutil.DefaultNginxImage,
						Req: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Limit: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceMemory:                 resource.MustParse("1Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
						Annotations: map[string]string{
							"volcano.sh/card.name": CardTypeRTX4090,
						},
					},
				},
				Annotations: map[string]string{
					"volcano.sh/card.request": fmt.Sprintf(`{"%s": 1}`, CardTypeRTX4090),
				},
			}

			// Create job directly (without using PodGroup), and set card request annotations
			fmt.Printf("Test 11: Starting to create test job %s\n", cardJobSpec.Name)
			cardJob := e2eutil.CreateJob(ctx, cardJobSpec)
			fmt.Printf("Test 11: Test job %s created successfully, attempting to schedule to queue without resource quota\n", cardJob.Name)

			defer func() {
				// Delete job
				e2eutil.DeleteJob(ctx, cardJob)
				fmt.Printf("Test 11: Job %s cleaned up\n", cardJob.Name)
			}()

			// Wait for some time to let scheduler attempt scheduling
			fmt.Printf("Test 11: Waiting for scheduler to process jobs, timeout is %v\n", JobProcessTimeout/2)
			time.Sleep(JobProcessTimeout / 2)
			e2eutil.CheckJobSchedulingFailed(ctx, job)
			e2eutil.CheckJobSchedulingFailed(ctx, cardJob)
		})
	})

	Context("Capacity Card - Deployment", func() {
		// Test 12: Deployment GPU card resource allocation test
		It("Deployment GPU Card Resource Allocation", func() {
			randomSuffix := generateRandomSuffix()
			fmt.Printf("Test 12: Generated random suffix %s\n", randomSuffix)
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				Namespace: fmt.Sprintf("deployment-card-test-%s", randomSuffix),
			})
			fmt.Printf("Test 12: Test context initialized, namespace %s\n", ctx.Namespace)
			defer e2eutil.CleanupTestContext(ctx)

			// Create queue with card quota
			queueSpec := &e2eutil.QueueSpec{
				Name:   fmt.Sprintf("deployment-queue-%s", randomSuffix),
				Weight: 10,
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Annotations: map[string]string{
					"volcano.sh/card.quota": fmt.Sprintf(`{"%s": 2}`, CardTypeRTX4090),
				},
			}

			// Create queue using e2eutil
			fmt.Printf("Test 12: Starting to create queue %s\n", queueSpec.Name)
			e2eutil.CreateQueueWithQueueSpec(ctx, queueSpec)
			fmt.Printf("Test 12: Queue %s created successfully, RTX4090 card quota is 2\n", queueSpec.Name)

			defer func() {
				// Delete queue using e2eutil
				fmt.Printf("Test 12: Cleaning up queue %s\n", queueSpec.Name)
				e2eutil.DeleteQueue(ctx, queueSpec.Name)
			}()

			// Wait for queue status to become open
			fmt.Printf("Test 12: Waiting for queue %s status to become open\n", queueSpec.Name)
			queueOpenErr := e2eutil.WaitQueueStatus(func() (bool, error) {
				queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueSpec.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			})
			Expect(queueOpenErr).NotTo(HaveOccurred(), "Queue failed to become open within timeout")
			fmt.Printf("Test 12: Queue %s status is now open\n", queueSpec.Name)

			// Create Deployment and add GPU card request annotation
			deploymentName := fmt.Sprintf("gpu-card-deployment-%s", randomSuffix)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: ctx.Namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": deploymentName,
						},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": deploymentName,
							},
							Annotations: map[string]string{
								"volcano.sh/card.name":             CardTypeRTX4090,
								"scheduling.volcano.sh/queue-name": queueSpec.Name,
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:            "nginx",
									Image:           e2eutil.DefaultNginxImage,
									ImagePullPolicy: v1.PullIfNotPresent,
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:                    resource.MustParse("1"),
											v1.ResourceMemory:                 resource.MustParse("1Gi"),
											v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
										},
										Limits: v1.ResourceList{
											v1.ResourceCPU:                    resource.MustParse("1"),
											v1.ResourceMemory:                 resource.MustParse("1Gi"),
											v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
										},
									},
								},
							},
							SchedulerName: "volcano",
						},
					},
				},
			}

			// Create Deployment
			fmt.Printf("Test 12: Starting to create Deployment %s\n", deploymentName)
			_, err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("Test 12: Deployment %s created successfully, requesting RTX4090 card resource\n", deploymentName)

			// Clean up resources
			defer func() {
				fmt.Printf("Test 12: Starting to clean up resources\n")
				// Delete Deployment
				fmt.Printf("Test 12: Deleting Deployment %s\n", deploymentName)
				if err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).Delete(context.TODO(), deploymentName, metav1.DeleteOptions{}); err != nil {
					fmt.Printf("Test 12-Warning: Failed to delete Deployment %s: %v\n", deploymentName, err)
				} else {
					fmt.Printf("Test 12: Deployment %s deleted successfully\n", deploymentName)
				}
			}()

			// Wait for Deployment to be ready
			fmt.Printf("Test 12: Waiting for Deployment to be ready\n")
			err = e2eutil.WaitDeploymentReady(ctx, deploymentName)
			Expect(err).NotTo(HaveOccurred(), "Deployment failed to become ready within timeout")
			fmt.Printf("Test 12: Deployment %s is now ready\n", deploymentName)
		})
	})
})

// Helper function: int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
