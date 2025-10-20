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

package admission

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Queue Validating Webhook E2E Test", func() {

	// Test basic queue creation with valid configurations
	ginkgo.It("Should allow queue creation with valid state (Open)", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-open",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
			Status: schedulingv1beta1.QueueStatus{
				State: schedulingv1beta1.QueueStateOpen,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow queue creation with valid state (Closed)", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-closed-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
			Status: schedulingv1beta1.QueueStatus{
				State: schedulingv1beta1.QueueStateClosed,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow queue creation without state set", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-no-state",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// Test invalid state rejection
	// Status will not be sent to admission webhook
	ginkgo.XIt("Should reject queue creation with invalid state", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-invalid-state",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
			Status: schedulingv1beta1.QueueStatus{
				State: "wrong",
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("queue state must be in"))
	})

	// Test weight validation
	// will be fixed by mutation webhook
	ginkgo.XIt("Should reject queue creation with zero weight", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-zero-weight",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 0,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("queue weight must be a positive integer"))
	})

	// validate by crd
	ginkgo.It("Should reject queue creation with negative weight", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-negative-weight",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: -1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("spec.weight in body should be greater than or equal to 1"))
	})

	// Test resource validation
	ginkgo.It("Should allow queue creation with only deserved resource", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-deserved-only",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Deserved: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow queue creation with only guarantee resource", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-guarantee-only",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Guarantee: schedulingv1beta1.Guarantee{
					Resource: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject queue creation with capability less than deserved", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-capability-less-deserved",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Capability: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Deserved: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("deserved should less equal than capability"))
	})

	ginkgo.It("Should reject queue creation with capability less than guarantee", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-capability-less-guarantee",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Capability: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Guarantee: schedulingv1beta1.Guarantee{
					Resource: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("2"),
						v1.ResourceMemory: resource.MustParse("3Gi"),
					},
				},
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("guarantee should less equal than capability"))
	})

	ginkgo.It("Should reject queue creation with deserved less than guarantee", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-deserved-less-guarantee",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Deserved: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("2"),
					v1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Guarantee: schedulingv1beta1.Guarantee{
					Resource: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("2"),
						v1.ResourceMemory: resource.MustParse("3Gi"),
					},
				},
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("guarantee should less equal than deserved"))
	})

	// Test hierarchical queue annotations
	ginkgo.It("Should reject queue creation with mismatched hierarchy and weights", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-hierarchy-mismatch",
				Annotations: map[string]string{
					schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
					schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3/4",
				},
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("must have the same length"))
	})

	ginkgo.It("Should reject queue creation with negative hierarchical weight", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-negative-hierarchy-weight",
				Annotations: map[string]string{
					schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
					schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/-1/3",
				},
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject queue creation with invalid hierarchical weight format", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-invalid-hierarchy-weight",
				Annotations: map[string]string{
					schedulingv1beta1.KubeHierarchyAnnotationKey:       "root/a/b",
					schedulingv1beta1.KubeHierarchyWeightAnnotationKey: "1/a/3",
				},
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	// Test queue update scenarios
	ginkgo.It("Should allow queue state update from Open to Closed", func() {
		queueName := "test-queue-update-state"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create queue with Open state
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: queueName,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
			Status: schedulingv1beta1.QueueStatus{
				State: schedulingv1beta1.QueueStateOpen,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Update to Closed state
			latestQueue.Status.State = schedulingv1beta1.QueueStateClosed

			_, updateErr = testCtx.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), latestQueue, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error or success, break
			break
		}

		gomega.Expect(updateErr).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queueName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// validate by crd
	ginkgo.It("Should reject queue weight update to negative value", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create queue with positive weight
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-update-weight",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Try to update to negative weight
		createdQueue.Spec.Weight = -1
		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), createdQueue, metav1.UpdateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("spec.weight in body should be greater than or equal to 1"))

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// Test queue deletion scenarios
	ginkgo.It("Should allow deletion of regular queues", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-deletable",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// Test hierarchical queue scenarios
	ginkgo.It("Should allow hierarchical queue creation with valid parent", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create parent queue first
		parentQueue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-parent-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Parent: "root",
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), parentQueue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Create child queue
		childQueue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-child-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Parent: "test-parent-queue",
			},
		}

		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), childQueue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), childQueue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), parentQueue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// VAP cannot validate parent queue existence
	ginkgo.XIt("Should reject hierarchical queue creation with non-existent parent", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		childQueue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-orphan-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				Parent: "non-existent-parent",
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), childQueue, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get parent queue"))
	})
})
