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

package queueovercommit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Queue-Scoped Overcommit E2E Test", func() {
	ginkgo.It("rejects admission that exceeds an annotated queue budget", func() {
		const (
			queueName = "queue-scoped-overcommit"
			firstPG   = "first-podgroup"
			secondPG  = "second-podgroup"
		)

		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		resources := corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: queueName,
				Annotations: map[string]string{
					schedulingv1beta1.QueueOvercommitFactorAnnotationKey: "1",
				},
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight:   1,
				Deserved: resources,
			},
		}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer e2eutil.DeleteQueue(ctx, queueName)

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		createPodGroup := func(name string) {
			_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ctx.Namespace},
				Spec: schedulingv1beta1.PodGroupSpec{
					MinMember:    1,
					MinResources: &resources,
					Queue:        queueName,
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		createUnschedulablePod := func(name, podGroup string) {
			e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:          name,
				Req:           resources,
				SchedulerName: "volcano",
				RestartPolicy: corev1.RestartPolicyNever,
				NodeSelector:  map[string]string{"volcano.sh/e2e-node": "does-not-exist"},
				Annotations: map[string]string{
					schedulingv1beta1.KubeGroupNameAnnotationKey: podGroup,
				},
			})
		}

		ginkgo.By("Admitting the first PodGroup without allocating its Pod")
		createPodGroup(firstPG)
		createUnschedulablePod("first-pod", firstPG)
		err = e2eutil.WaitPodGroupPhase(ctx, &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: firstPG, Namespace: ctx.Namespace},
		}, schedulingv1beta1.PodGroupInqueue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Rejecting a second PodGroup that exceeds the queue budget")
		createPodGroup(secondPG)
		createUnschedulablePod("second-pod", secondPG)

		gomega.Eventually(func() error {
			podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(context.TODO(), secondPG, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if podGroup.Status.Phase != schedulingv1beta1.PodGroupPending {
				return fmt.Errorf("expected PodGroup %q to remain Pending, got %s", secondPG, podGroup.Status.Phase)
			}

			events, err := ctx.Kubeclient.CoreV1().Events(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, event := range events.Items {
				if event.InvolvedObject.Name == secondPG && event.Reason == string(schedulingv1beta1.PodGroupUnschedulableType) &&
					strings.Contains(event.Message, "queue overcommit admission budget insufficient") {
					return nil
				}
			}
			return fmt.Errorf("expected queue overcommit rejection event for PodGroup %q", secondPG)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.Succeed())
	})
})
