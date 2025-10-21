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

var _ = ginkgo.Describe("PodGroup Mutating E2E Test", func() {

	ginkgo.It("Should not modify podgroup with custom queue", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		customQueue := "custom-queue-podgroup"
		// First create custom-queue-podgroup
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:      customQueue,
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}
		_, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Poll and wait for queue status to be open
		gomega.Eventually(func() string {
			q, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), customQueue, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return string(q.Status.State)
		}, 30, 1).Should(gomega.Equal("Open")) // 30 seconds timeout, 1 second interval

		podgroup := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-queue-podgroup",
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:        customQueue,
				MinMember:    1,
				MinResources: nil,
			},
		}

		createdPodGroup, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podgroup, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPodGroup.Spec.Queue).To(gomega.Equal(customQueue))
	})

	ginkgo.It("Should set queue from namespace annotation when podgroup uses default queue", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// First, update the namespace with queue annotation
		namespaceQueueName := "namespace-queue"
		ns, err := testCtx.Kubeclient.CoreV1().Namespaces().Get(context.TODO(), testCtx.Namespace, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations[schedulingv1beta1.QueueNameAnnotationKey] = namespaceQueueName

		_, err = testCtx.Kubeclient.CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// First create namespace-queue
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespaceQueueName,
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}
		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Poll and wait for queue status to be open
		gomega.Eventually(func() string {
			q, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), namespaceQueueName, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return string(q.Status.State)
		}, 30, 1).Should(gomega.Equal("Open"))

		// Create podgroup with default queue
		podgroup := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-queue-podgroup",
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:        schedulingv1beta1.DefaultQueue,
				MinMember:    1,
				MinResources: nil,
			},
		}

		createdPodGroup, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podgroup, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPodGroup.Spec.Queue).To(gomega.Equal(namespaceQueueName))
	})

	ginkgo.It("Should keep default queue when namespace has no queue annotation", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Ensure namespace has no queue annotation
		ns, err := testCtx.Kubeclient.CoreV1().Namespaces().Get(context.TODO(), testCtx.Namespace, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if ns.Annotations != nil {
			delete(ns.Annotations, schedulingv1beta1.QueueNameAnnotationKey)
			_, err = testCtx.Kubeclient.CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// Create podgroup with default queue
		podgroup := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "no-annotation-podgroup",
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:        schedulingv1beta1.DefaultQueue,
				MinMember:    1,
				MinResources: nil,
			},
		}

		createdPodGroup, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podgroup, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPodGroup.Spec.Queue).To(gomega.Equal(schedulingv1beta1.DefaultQueue))
	})

	ginkgo.It("Should handle multiple podgroups in same namespace", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Set queue annotation in namespace
		namespaceQueueName := "namespace-queue-multiple"
		ns, err := testCtx.Kubeclient.CoreV1().Namespaces().Get(context.TODO(), testCtx.Namespace, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations[schedulingv1beta1.QueueNameAnnotationKey] = namespaceQueueName

		_, err = testCtx.Kubeclient.CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// First create namespace-queue-multiple
		queue1 := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespaceQueueName,
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}
		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() string {
			q, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), namespaceQueueName, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return string(q.Status.State)
		}, 30, 1).Should(gomega.Equal("Open"))

		// Then create custom-queue-podgroup-2
		customQueue := "custom-queue-podgroup-2"
		queue2 := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:      customQueue,
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}
		_, err = testCtx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() string {
			q, err := testCtx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), customQueue, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			return string(q.Status.State)
		}, 30, 1).Should(gomega.Equal("Open"))

		// Create multiple podgroups with different queue configurations
		podgroup1 := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "multiple-1-default",
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:        schedulingv1beta1.DefaultQueue,
				MinMember:    1,
				MinResources: nil,
			},
		}

		podgroup2 := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "multiple-2-custom",
				Namespace: testCtx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				Queue:        customQueue,
				MinMember:    1,
				MinResources: nil,
			},
		}

		createdPodGroup1, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podgroup1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		createdPodGroup2, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), podgroup2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// First podgroup should be mutated to use namespace queue
		gomega.Expect(createdPodGroup1.Spec.Queue).To(gomega.Equal(namespaceQueueName))

		// Second podgroup should keep its custom queue
		gomega.Expect(createdPodGroup2.Spec.Queue).To(gomega.Equal(customQueue))
	})
})
