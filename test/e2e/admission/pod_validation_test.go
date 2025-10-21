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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Pod Validating E2E Test", func() {

	ginkgo.It("Should allow pod creation with valid annotations", func() {
		podName := "valid-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable: "2",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.WaitPodPhase(testCtx, pod, []corev1.PodPhase{corev1.PodPending, corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow pod creation with valid percentage annotation", func() {
		podName := "valid-percentage-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMaxUnavailable: "50%",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.WaitPodPhase(testCtx, pod, []corev1.PodPhase{corev1.PodPending, corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject pod creation with invalid negative annotation value", func() {
		podName := "invalid-negative-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable: "-1",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("must be a positive integer"))
	})

	ginkgo.It("Should reject pod creation with invalid zero annotation value", func() {
		podName := "invalid-zero-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable: "0",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("must be a positive integer"))
	})

	ginkgo.It("Should reject pod creation with invalid percentage annotation (0%)", func() {
		podName := "invalid-zero-percent-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMaxUnavailable: "0%",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("percentage which between 1% ~ 99%"))
	})

	ginkgo.It("Should reject pod creation with invalid percentage annotation (100%)", func() {
		podName := "invalid-hundred-percent-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMaxUnavailable: "100%",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("percentage which between 1% ~ 99%"))
	})

	ginkgo.It("Should reject pod creation with invalid string annotation", func() {
		podName := "invalid-string-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable: "invalid",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject pod creation with multiple conflicting annotations", func() {
		podName := "conflicting-annotations-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable:   "2",
					schedulingv1beta1.JDBMaxUnavailable: "50%",
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("not allow configure multiple annotations"))
	})

	ginkgo.It("Should allow pod creation without annotations when using volcano scheduler", func() {
		podName := "no-annotations-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.WaitPodPhase(testCtx, pod, []corev1.PodPhase{corev1.PodPending, corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow pod creation with non-volcano scheduler (bypass validation)", func() {
		podName := "non-volcano-scheduler-pod"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      podName,
				Annotations: map[string]string{
					schedulingv1beta1.JDBMinAvailable: "-1", // This would normally fail validation
				},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "default-scheduler", // Using default scheduler, not volcano
				Containers:    util.CreateContainers(util.DefaultNginxImage, "", "", util.OneCPU, util.OneCPU, 0),
			},
		}

		_, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = util.WaitPodPhase(testCtx, pod, []corev1.PodPhase{corev1.PodPending, corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
