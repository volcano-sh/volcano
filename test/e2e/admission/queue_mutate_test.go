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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Queue Mutating E2E Test", func() {

	ginkgo.It("Should set default reclaimable to true when not specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-reclaimable-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
				// Reclaimable not specified
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdQueue.Spec.Reclaimable).NotTo(gomega.BeNil())
		gomega.Expect(*createdQueue.Spec.Reclaimable).To(gomega.BeTrue())
	})

	ginkgo.It("Should set default weight to 1 when not specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		trueValue := true
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-weight-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				// Weight not specified (defaults to 0)
				Reclaimable: &trueValue,
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdQueue.Spec.Weight).To(gomega.Equal(int32(1)))
	})

	ginkgo.It("Should apply both default reclaimable and weight when not specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "both-defaults-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				// Both Weight and Reclaimable not specified
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdQueue.Spec.Weight).To(gomega.Equal(int32(1)))
		gomega.Expect(createdQueue.Spec.Reclaimable).NotTo(gomega.BeNil())
		gomega.Expect(*createdQueue.Spec.Reclaimable).To(gomega.BeTrue())
	})

	ginkgo.It("Should preserve existing field values when specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		falseValue := false
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "preserve-values-queue",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight:      5,           // Explicitly specified
				Reclaimable: &falseValue, // Explicitly specified as false
			},
		}

		createdQueue, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Should preserve explicitly specified values
		gomega.Expect(createdQueue.Spec.Weight).To(gomega.Equal(int32(5)))
		gomega.Expect(createdQueue.Spec.Reclaimable).NotTo(gomega.BeNil())
		gomega.Expect(*createdQueue.Spec.Reclaimable).To(gomega.BeFalse())
	})

	// Note: We are not testing hierarchy root prefix addition in e2e tests because:
	// 1. The validation policy runs before the mutating webhook
	// 2. The validation requires exact segment count matching before mutation can occur
	// 3. This functionality is already covered by unit tests which test the webhook logic directly
	// 4. In practice, users should provide properly formatted hierarchy annotations
})
