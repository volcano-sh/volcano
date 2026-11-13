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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Preempt with the PDB plugin E2E Test", func() {
	const (
		pdbLowPriority  = "pdb-preempt-low"
		pdbHighPriority = "pdb-preempt-high"
	)

	var (
		ctx *e2eutil.TestContext
		cmc *e2eutil.ConfigMapCase
	)

	BeforeEach(func() {
		// Enqueue conflicts with preempt. Enable the pdb plugin.
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		Expect(cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				sc.Actions = strings.TrimPrefix(sc.Actions, "enqueue, ")
				if len(sc.Tiers) > 0 && !sc.Tiers[0].ContainsPlugin("pdb") {
					sc.Tiers[0].Plugins = append(sc.Tiers[0].Plugins, e2eutil.PluginOption{Name: "pdb"})
				}
				return true
			})
		})).To(Succeed())

		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{pdbLowPriority: 10, pdbHighPriority: 100},
		})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
		Expect(cmc.UndoChanged()).To(Succeed())
	})

	It("preempts pods that carry no labels", func() {
		node, size := pdbPreemptNodeAndPodSize(ctx)

		createPod := func(name, priorityClass string) {
			_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec:       schedulingv1beta1.PodGroupSpec{MinMember: 1, Queue: "default", PriorityClassName: priorityClass},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// No labels on purpose.
			_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: name},
				},
				Spec: corev1.PodSpec{
					SchedulerName:                 "volcano",
					PriorityClassName:             priorityClass,
					NodeSelector:                  map[string]string{corev1.LabelHostname: node},
					TerminationGracePeriodSeconds: ptr.To(int64(0)),
					Containers: []corev1.Container{{
						Name:  "c",
						Image: e2eutil.DefaultNginxImage,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(size, resource.DecimalSI)},
						},
					}},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		isRunning := func(name string) func() bool {
			return func() bool {
				pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
				return err == nil && pod.Status.Phase == corev1.PodRunning
			}
		}

		createPod("victim-1", pdbLowPriority)
		createPod("victim-2", pdbLowPriority)
		Eventually(isRunning("victim-1"), 5*time.Minute, time.Second).Should(BeTrue())
		Eventually(isRunning("victim-2"), 5*time.Minute, time.Second).Should(BeTrue())

		createPod("preemptor", pdbHighPriority)
		Eventually(isRunning("preemptor"), 2*time.Minute, time.Second).Should(BeTrue(), "the preemptor should run after evicting an unlabeled pod")

		evicted := 0
		for _, name := range []string{"victim-1", "victim-2"} {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if errors.IsNotFound(err) || (err == nil && pod.DeletionTimestamp != nil) {
				evicted++
			}
		}
		Expect(evicted).To(BeNumerically(">=", 1))
	})
})

// pdbPreemptNodeAndPodSize returns a worker node and a per-pod CPU request (millicores) such that two pods fill it.
func pdbPreemptNodeAndPodSize(ctx *e2eutil.TestContext) (string, int64) {
	nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, n := range nodes.Items {
		if n.Spec.Unschedulable || len(n.Spec.Taints) > 0 {
			continue
		}
		pods, err := ctx.Kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			FieldSelector: "spec.nodeName=" + n.Name,
		})
		Expect(err).NotTo(HaveOccurred())
		free := n.Status.Allocatable.Cpu().MilliValue()
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			for _, c := range p.Spec.Containers {
				free -= c.Resources.Requests.Cpu().MilliValue()
			}
		}
		if free >= 200 {
			return n.Name, free / 2
		}
	}
	Fail("no schedulable node with enough free CPU")
	return "", 0
}
