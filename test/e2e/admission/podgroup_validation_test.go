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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("PodGroup Validating E2E Test", func() {

	// Test PodGroup creation with valid queue (Open state)
	ginkgo.It("Should allow PodGroup creation with valid open queue", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// First create a queue with Open state
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue-open",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Small delay to ensure mutation is complete
		time.Sleep(100 * time.Millisecond)

		// Get the latest version of the queue after potential mutations
		updatedQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), createdQueue.Name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Update queue status to Open (since status is typically set by controller)
		updatedQueue.Status.State = schedulingv1beta1.QueueStateOpen
		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), updatedQueue, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Now create PodGroup that references this queue
		podGroup := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      "test-podgroup-valid",
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:             "test-queue-open",
				MinMember:         1,
				PriorityClassName: "",
			},
		}

		_, err = testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podGroup, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Delete(context.TODO(), podGroup.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = testCtx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	// Test PodGroup creation without queue (should succeed)
	ginkgo.It("Should allow PodGroup creation without queue specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create PodGroup without specifying queue
		podGroup := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      "test-podgroup-no-queue",
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:             "", // Empty queue
				MinMember:         1,
				PriorityClassName: "",
			},
		}

		_, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podGroup, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Delete(context.TODO(), podGroup.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

})
