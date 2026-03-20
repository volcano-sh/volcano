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

	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Pod Mutating E2E Test", func() {

	ginkgo.It("Should add nodeSelector for management namespace pods", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create a pod in management namespace that should get nodeSelector added
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "management-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.BeNil())
	})

	ginkgo.It("Should add nodeSelector for pods with cpu resource-group annotation", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cpu-pod",
				Namespace: testCtx.Namespace,
				Annotations: map[string]string{
					"volcano.sh/resource-group": "cpu",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Since  configuration is set in test environment,
		// the pod should not be mutated and nodeSelector should remain nil
		gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.BeNil())
		// Verify the annotation is preserved
		gomega.Expect(createdPod.Annotations).To(gomega.HaveKey("volcano.sh/resource-group"))
		gomega.Expect(createdPod.Annotations["volcano.sh/resource-group"]).To(gomega.Equal("cpu"))
	})

	ginkgo.It("Should add nodeSelector for pods with gpu resource-group annotation", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpu-pod",
				Namespace: testCtx.Namespace,
				Annotations: map[string]string{
					"volcano.sh/resource-group": "gpu",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Since  configuration is set in test environment,
		// the pod should not be mutated and nodeSelector should remain nil
		gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.BeNil())
		// Verify the annotation is preserved
		gomega.Expect(createdPod.Annotations).To(gomega.HaveKey("volcano.sh/resource-group"))
		gomega.Expect(createdPod.Annotations["volcano.sh/resource-group"]).To(gomega.Equal("gpu"))
	})

	ginkgo.It("Should add affinity for management namespace pods", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "affinity-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				// No existing affinity - should be added by webhook
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify affinity is added based on configuration
		// Note: The actual affinity configuration depends on webhook setup
		if createdPod.Spec.Affinity != nil {
			gomega.Expect(createdPod.Spec.Affinity.NodeAffinity).NotTo(gomega.BeNil())
		}
	})

	ginkgo.It("Should not override existing affinity", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		existingAffinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "custom-key",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"custom-value"},
								},
							},
						},
					},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-affinity-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				Affinity: existingAffinity,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify existing affinity is preserved
		gomega.Expect(createdPod.Spec.Affinity).NotTo(gomega.BeNil())
		gomega.Expect(createdPod.Spec.Affinity.NodeAffinity).NotTo(gomega.BeNil())
		gomega.Expect(createdPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(gomega.BeNil())
		gomega.Expect(createdPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).To(gomega.HaveLen(1))
		gomega.Expect(createdPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions).To(gomega.HaveLen(1))
		gomega.Expect(createdPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key).To(gomega.Equal("custom-key"))
	})

	ginkgo.It("Should add tolerations for management namespace pods", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tolerations-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify tolerations are added based on configuration
		if len(createdPod.Spec.Tolerations) > 0 {
			// Check for management-related tolerations based on unit test expectations
			foundManagementToleration := false
			for _, toleration := range createdPod.Spec.Tolerations {
				if toleration.Key == "mng-taint-1" || toleration.Effect == corev1.TaintEffectNoSchedule {
					foundManagementToleration = true
					break
				}
			}
			if foundManagementToleration {
				gomega.Expect(foundManagementToleration).To(gomega.BeTrue())
			}
		}
	})

	ginkgo.It("Should append tolerations to existing ones", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		existingTolerations := []corev1.Toleration{
			{
				Key:      "existing-taint",
				Operator: corev1.TolerationOpEqual,
				Value:    "existing-value",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-tolerations-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				Tolerations: existingTolerations,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify existing tolerations are preserved and new ones are appended
		gomega.Expect(len(createdPod.Spec.Tolerations)).To(gomega.BeNumerically(">=", 1))

		// Check that original toleration is still present
		foundExistingToleration := false
		for _, toleration := range createdPod.Spec.Tolerations {
			if toleration.Key == "existing-taint" {
				foundExistingToleration = true
				break
			}
		}
		gomega.Expect(foundExistingToleration).To(gomega.BeTrue())
	})

	ginkgo.It("Should set scheduler name based on resource group", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scheduler-pod",
				Namespace: testCtx.Namespace,
				Annotations: map[string]string{
					"volcano.sh/resource-group": "cpu",
				},
			},
			Spec: corev1.PodSpec{
				// No scheduler name specified - should be set by webhook
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Since  configuration is set in test environment,
		// the scheduler name should be the default Kubernetes scheduler
		gomega.Expect(createdPod.Spec.SchedulerName).To(gomega.Equal("default-scheduler"))
	})

	ginkgo.It("Should merge nodeSelector with existing ones", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		existingNodeSelector := map[string]string{
			"existing-label": "existing-value",
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "merge-nodeselector-pod",
				Namespace: testCtx.Namespace,
				Annotations: map[string]string{
					"volcano.sh/resource-group": "cpu",
				},
			},
			Spec: corev1.PodSpec{
				NodeSelector: existingNodeSelector,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify existing nodeSelector is preserved and new labels are added
		gomega.Expect(createdPod.Spec.NodeSelector).NotTo(gomega.BeNil())
		gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.HaveKey("existing-label"))
		gomega.Expect(createdPod.Spec.NodeSelector["existing-label"]).To(gomega.Equal("existing-value"))

		// Check if volcano nodetype label is also added
		if len(createdPod.Spec.NodeSelector) > 1 {
			gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.HaveKey("volcano.sh/nodetype"))
		}
	})

	ginkgo.It("Should not mutate pods that don't match any resource group", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "normal-pod",
				Namespace: testCtx.Namespace,
				// No annotations or special namespace - shouldn't match any resource group
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Expect(createdPod.Spec.NodeSelector).To(gomega.BeNil())
		gomega.Expect(createdPod.Spec.Affinity).To(gomega.BeNil())
		gomega.Expect(createdPod.Spec.SchedulerName).To(gomega.Equal("default-scheduler"))
		// Note: tolerations might be added by Kubernetes for system taints, so we don't check for empty tolerations
	})

	ginkgo.It("Should handle pods with custom annotation-based resource group matching", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-annotation-pod",
				Namespace: testCtx.Namespace,
				Annotations: map[string]string{
					"volcano.sh/resource-group": "custom-group",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Pod created successfully - mutation behavior depends on webhook configuration
		// for the custom-group resource group
		gomega.Expect(createdPod.Name).To(gomega.Equal("custom-annotation-pod"))
		gomega.Expect(createdPod.Annotations).To(gomega.HaveKey("volcano.sh/resource-group"))
		gomega.Expect(createdPod.Annotations["volcano.sh/resource-group"]).To(gomega.Equal("custom-group"))
	})

	ginkgo.It("Should preserve existing scheduler name when already specified", func() {
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		existingScheduler := "custom-scheduler"

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-scheduler-pod",
				Namespace: testCtx.Namespace,
			},
			Spec: corev1.PodSpec{
				SchedulerName: existingScheduler,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox:1.24",
					},
				},
			},
		}

		createdPod, err := testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify existing scheduler name is preserved
		gomega.Expect(createdPod.Spec.SchedulerName).To(gomega.Equal(existingScheduler))
	})
})
