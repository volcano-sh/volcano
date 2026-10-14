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

package schedulingbase

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("PodGroup workload workqueue E2E", func() {
	for _, kind := range []string{"ReplicaSet", "StatefulSet"} {
		It("recovers after rapid scale changes for "+kind, func() {
			ctx := e2eutil.InitTestContext(e2eutil.Options{})
			defer e2eutil.CleanupTestContext(ctx)
			labels := map[string]string{"app": "pg-workqueue"}
			template := corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SchedulerName: "volcano",
					Containers:    []corev1.Container{{Name: "nginx", Image: e2eutil.DefaultNginxImage}},
				},
			}
			var uid types.UID
			if kind == "ReplicaSet" {
				rs, err := ctx.Kubeclient.AppsV1().ReplicaSets(ctx.Namespace).Create(context.Background(), &appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "pg-workqueue", Namespace: ctx.Namespace},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: ptr.To[int32](1),
						Selector: &metav1.LabelSelector{MatchLabels: labels},
						Template: template,
					},
				}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				uid = rs.UID
			} else {
				_, err := ctx.Kubeclient.CoreV1().Services(ctx.Namespace).Create(context.Background(), &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "pg-workqueue", Namespace: ctx.Namespace},
					Spec:       corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Selector: labels, Ports: []corev1.ServicePort{{Port: 80}}},
				}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				sts, err := ctx.Kubeclient.AppsV1().StatefulSets(ctx.Namespace).Create(context.Background(), &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "pg-workqueue", Namespace: ctx.Namespace},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    ptr.To[int32](1),
						Selector:    &metav1.LabelSelector{MatchLabels: labels},
						ServiceName: "pg-workqueue",
						Template:    template,
					},
				}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				uid = sts.UID
			}
			pgName := batchv1alpha1.PodgroupNamePrefix + string(uid)

			expectReconciled := func() {
				Eventually(func() error {
					group, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(context.Background(), pgName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					owner := metav1.GetControllerOf(group)
					if owner == nil || owner.UID != uid {
						return fmt.Errorf("PodGroup %s has wrong owner: %v", pgName, owner)
					}
					return nil
				}, 2*time.Minute, 250*time.Millisecond).Should(Succeed())
				Eventually(func() (string, error) {
					pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=pg-workqueue"})
					if err != nil {
						return "", err
					}
					for _, pod := range pods.Items {
						if pod.DeletionTimestamp == nil {
							return pod.Annotations[scheduling.KubeGroupNameAnnotationKey], nil
						}
					}
					return "", nil
				}, 2*time.Minute, 250*time.Millisecond).Should(Equal(pgName))
			}
			scale := func(replicas int32) {
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					if kind == "ReplicaSet" {
						client := ctx.Kubeclient.AppsV1().ReplicaSets(ctx.Namespace)
						current, err := client.Get(context.Background(), "pg-workqueue", metav1.GetOptions{})
						if err != nil {
							return err
						}
						current.Spec.Replicas = ptr.To(replicas)
						_, err = client.Update(context.Background(), current, metav1.UpdateOptions{})
						return err
					}
					client := ctx.Kubeclient.AppsV1().StatefulSets(ctx.Namespace)
					current, err := client.Get(context.Background(), "pg-workqueue", metav1.GetOptions{})
					if err != nil {
						return err
					}
					current.Spec.Replicas = ptr.To(replicas)
					_, err = client.Update(context.Background(), current, metav1.UpdateOptions{})
					return err
				})
				Expect(err).NotTo(HaveOccurred())
			}

			expectReconciled()
			for range 3 {
				scale(0)
				scale(1)
				expectReconciled()
			}
			scale(0)
			Eventually(func() bool {
				_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Get(context.Background(), pgName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, 2*time.Minute, 250*time.Millisecond).Should(BeTrue())
		})
	}
})
