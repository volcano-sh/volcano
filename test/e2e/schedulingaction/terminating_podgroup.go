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

package schedulingaction

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("PodGroup terminating E2E Test", func() {
	createPodGroup := func(ctx *e2eutil.TestContext, name string, minMember int32) *schedulingv1beta1.PodGroup {
		pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
			&schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ctx.Namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					MinMember: minMember,
					Queue:     e2eutil.DefaultQueue,
				},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return pg
	}

	// createStickyPod creates a pod that ignores SIGTERM so that it keeps
	// running (Releasing) for a long time after deletion.
	createStickyPod := func(ctx *e2eutil.TestContext, pgName, podName string) *corev1.Pod {
		grace := int64(300)
		return e2eutil.CreatePod(ctx, e2eutil.PodSpec{
			Name:          podName,
			SchedulerName: "volcano",
			RestartPolicy: corev1.RestartPolicyNever,
			Image:         e2eutil.DefaultBusyBoxImage,
			Command:       []string{"sh", "-c", "trap '' TERM; sleep 999999"},
			Req: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: pgName,
			},
			TerminationGracePeriodSeconds: &grace,
		})
	}

	waitPodTerminating := func(ctx *e2eutil.TestContext, podName string) {
		gomega.Eventually(func() bool {
			p, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return p.DeletionTimestamp != nil
		}, e2eutil.TwoMinute, 200*time.Millisecond).Should(gomega.BeTrue())
	}

	forceDeletePod := func(ctx *e2eutil.TestContext, podName string) {
		zero := int64(0)
		err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{
			GracePeriodSeconds: &zero,
		})
		if apierrors.IsNotFound(err) {
			return
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	ginkgo.DescribeTable("PodGroup should stay Running while scheduled members are terminating", func(memberCount int) {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		const pgName = "pg-sticky-releasing"
		podNames := make([]string, 0, memberCount)
		for i := 0; i < memberCount; i++ {
			podNames = append(podNames, fmt.Sprintf("sticky-releasing-%d", i))
		}
		defer func() {
			for _, podName := range podNames {
				forceDeletePod(ctx, podName)
			}
		}()

		ginkgo.By("Creating PodGroup and its members")
		pg := createPodGroup(ctx, pgName, int32(memberCount))
		pods := make([]*corev1.Pod, 0, memberCount)
		for _, podName := range podNames {
			pods = append(pods, createStickyPod(ctx, pgName, podName))
		}

		ginkgo.By("Waiting until all Pods and PodGroup are Running")
		for _, pod := range pods {
			err := e2eutil.WaitPodPhase(ctx, pod, []corev1.PodPhase{corev1.PodRunning})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
		err := e2eutil.WaitPodGroupPhase(ctx, pg, schedulingv1beta1.PodGroupRunning)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Deleting one member without waiting for its container to exit")
		startTime := metav1.Now()
		terminatingPod := podNames[0]
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), terminatingPod, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verifying the deleted Pod enters terminating state")
		waitPodTerminating(ctx, terminatingPod)

		ginkgo.By("Verifying PodGroup stays Running without new Unschedulable condition while one Pod is terminating")
		gomega.Consistently(func() error {
			current, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(context.TODO(), pgName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(current.Status.Phase).To(gomega.Equal(schedulingv1beta1.PodGroupRunning),
				"PodGroup phase should stay Running while one Pod is terminating")
			for _, c := range current.Status.Conditions {
				if c.Type == schedulingv1beta1.PodGroupUnschedulableType &&
					c.Status == corev1.ConditionTrue &&
					!c.LastTransitionTime.Before(&startTime) {
					gomega.Expect(c.Reason).NotTo(gomega.Equal(schedulingv1beta1.NotEnoughResourcesReason),
						"PodGroup should not be reported NotEnoughResources while one Pod is terminating")
				}
			}
			return nil
		}, 15*time.Second, 500*time.Millisecond).Should(gomega.Succeed())
	},
		ginkgo.Entry("single-Pod PodGroup", 1),
		ginkgo.Entry("partially terminating multi-Pod PodGroup", 2),
	)
})
