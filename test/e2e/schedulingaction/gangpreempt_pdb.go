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
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Gang Preempt PDB E2E Test", func() {
	const (
		gpLowPriority    = "gp-pdb-low"
		gpHighPriority   = "gp-pdb-high"
		protectedLabel   = "gp-protected"
		unprotectedLabel = "gp-unprotected"
	)

	var (
		ctx  *e2eutil.TestContext
		node string
		size int64 // millicores per pod, four of them fill the node
		cmc  *e2eutil.ConfigMapCase
	)

	BeforeEach(func() {
		// Enqueue conflicts with preemption. Swap preempt for gangpreempt and enable the pdb plugin.
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		Expect(cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				sc.Actions = strings.Replace(strings.TrimPrefix(sc.Actions, "enqueue, "), "preempt", "gangpreempt", 1)
				if len(sc.Tiers) > 0 && !sc.Tiers[0].ContainsPlugin("pdb") {
					sc.Tiers[0].Plugins = append(sc.Tiers[0].Plugins, e2eutil.PluginOption{Name: "pdb"})
				}
				return true
			})
		})).To(Succeed())

		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{gpLowPriority: 10, gpHighPriority: 100},
		})
		node, size = pickNodeAndPodSize(ctx)
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
		Expect(cmc.UndoChanged()).To(Succeed())
	})

	createPDB := func(name, label string, minAvailable int) {
		_, err := ctx.Kubeclient.PolicyV1().PodDisruptionBudgets(ctx.Namespace).Create(context.TODO(), &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: ptr.To(intstr.FromInt32(int32(minAvailable))),
				Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app": label}},
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	createGroup := func(name, priorityClass string, minMember int32) {
		_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:         minMember,
				Queue:             "default",
				PriorityClassName: priorityClass,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	createPod := func(name, group, priorityClass, label string) {
		_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      map[string]string{"app": label},
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: group},
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

	running := func(names ...string) {
		for _, name := range names {
			Eventually(func() corev1.PodPhase {
				pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
				if err != nil {
					return corev1.PodUnknown
				}
				return pod.Status.Phase
			}, 5*time.Minute, time.Second).Should(Equal(corev1.PodRunning), "pod %s should be Running", name)
		}
	}

	// The disruption controller publishes the budget asynchronously.
	noDisruptionsAllowed := func(name string) {
		Eventually(func() int32 {
			pdb, err := ctx.Kubeclient.PolicyV1().PodDisruptionBudgets(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil || pdb.Status.ObservedGeneration == 0 {
				return -1
			}
			return pdb.Status.DisruptionsAllowed
		}, 2*time.Minute, time.Second).Should(BeZero())
	}

	It("evicts only the unprotected members of a victim podgroup", func() {
		createPDB("protected-pdb", protectedLabel, 2)
		createGroup("victim", gpLowPriority, 2)
		for _, name := range []string{"protected-1", "protected-2"} {
			createPod(name, "victim", gpLowPriority, protectedLabel)
		}
		for _, name := range []string{"free-1", "free-2"} {
			createPod(name, "victim", gpLowPriority, unprotectedLabel)
		}
		running("protected-1", "protected-2", "free-1", "free-2")
		noDisruptionsAllowed("protected-pdb")

		createGroup("preemptor", gpHighPriority, 1)
		createPod("preemptor-0", "preemptor", gpHighPriority, "gp-preemptor")
		running("preemptor-0")

		for _, name := range []string{"protected-1", "protected-2"} {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "protected pod %s must not be evicted", name)
			Expect(pod.DeletionTimestamp).To(BeNil(), "protected pod %s must not be evicted", name)
		}
	})
})

// pickNodeAndPodSize returns a worker node and a per-pod CPU request (millicores) such that four pods fill it.
func pickNodeAndPodSize(ctx *e2eutil.TestContext) (string, int64) {
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
		if free >= 400 {
			return n.Name, free / 4
		}
	}
	Fail("no schedulable node with enough free CPU")
	return "", 0
}
