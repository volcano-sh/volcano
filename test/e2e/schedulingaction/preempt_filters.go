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
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

type preemptFilterCase struct {
	configure     func(ns string, holder, incoming *v1.Pod)
	expectBlocked bool
}

var _ = Describe("Topology-aware preemption filters", Ordered, ContinueOnFailure, Serial, func() {
	var cmc *e2eutil.ConfigMapCase

	BeforeAll(func() {
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		Expect(cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, enableTopologyAwarePreemptionConfig)
		})).To(Succeed())
		// Wait for the scheduler ConfigMap volume to reload.
		time.Sleep(10 * time.Second)
	})

	AfterAll(func() {
		if cmc != nil {
			Expect(cmc.UndoChanged()).To(Succeed())
		}
	})

	DescribeTable("checks candidate constraints and reprieves unrelated pods", func(tc preemptFilterCase) {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				"filter-low":    10,
				"filter-holder": 20,
				"filter-high":   100,
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		bg := context.Background()
		req := e2eutil.CPUResource("10m")
		node, slots := e2eutil.ComputeNode(ctx, req)
		Expect(slots).To(BeNumerically(">=", 3))

		// Priority makes the unrelated pod the first tentative victim. With enough
		// CPU for all three pods, only the conflicting holder should be evicted.
		unrelatedSpec := newPreemptFilterPod(ctx.Namespace, "unrelated", "filter-low", node, req)
		holderSpec := newPreemptFilterPod(ctx.Namespace, "holder", "filter-holder", node, req)
		incomingSpec := newPreemptFilterPod(ctx.Namespace, "incoming", "filter-high", node, req)
		tc.configure(ctx.Namespace, holderSpec, incomingSpec)

		unrelated := createPreemptFilterPod(bg, ctx, unrelatedSpec)
		holder := createPreemptFilterPod(bg, ctx, holderSpec)
		waitPreemptFilterPodReady(bg, ctx, unrelated)
		waitPreemptFilterPodReady(bg, ctx, holder)

		incoming := createPreemptFilterPod(bg, ctx, incomingSpec)
		if tc.expectBlocked {
			Eventually(func() (bool, error) {
				events, err := ctx.Kubeclient.CoreV1().Events(ctx.Namespace).List(bg, metav1.ListOptions{})
				if err != nil {
					return false, err
				}
				for _, event := range events.Items {
					if event.Reason == "FailedScheduling" && strings.Contains(strings.ToLower(event.Message), "node affinity") {
						return true, nil
					}
				}
				return false, nil
			}, time.Minute, time.Second).Should(BeTrue())
		} else {
			waitPreemptFilterPodReady(bg, ctx, incoming)
			Eventually(func() bool {
				_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(bg, holder.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, time.Minute, time.Second).Should(BeTrue())
		}

		Consistently(func(g Gomega) {
			assertPodUntouched(g, bg, ctx, unrelated)
			if tc.expectBlocked {
				assertPodUntouched(g, bg, ctx, holder)
				pending, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(bg, incoming.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pending.Spec.NodeName).To(BeEmpty())
			}
		}, 10*time.Second, time.Second).Should(Succeed())
	},
		Entry("evicts the hostPort holder and keeps the unrelated lower-priority pod", preemptFilterCase{
			configure: func(_ string, holder, incoming *v1.Pod) {
				setHostPort(holder, 29091)
				setHostPort(incoming, 29091)
			},
		}),
		Entry("fixes topology spread and keeps the unrelated lower-priority pod", preemptFilterCase{
			configure: func(_ string, holder, incoming *v1.Pod) {
				// Node affinity limits eligible domains to one; minDomains=2 makes the
				// global minimum zero, so the matching holder must be evicted.
				minDomains := int32(2)
				labels := map[string]string{"role": "worker"}
				holder.Labels = labels
				incoming.Labels = labels
				incoming.Spec.TopologySpreadConstraints = []v1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						MinDomains:        &minDomains,
						TopologyKey:       v1.LabelHostname,
						WhenUnsatisfiable: v1.DoNotSchedule,
						LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
					},
				}
			},
		}),
		Entry("keeps both pods when the incoming pod fails node affinity", preemptFilterCase{
			configure: func(ns string, holder, incoming *v1.Pod) {
				// Share a hostPort so preemption would evict holder if candidate selection
				// failed to reject the node on the unsatisfiable node selector.
				setHostPort(holder, 29091)
				setHostPort(incoming, 29091)
				incoming.Spec.NodeSelector = map[string]string{"preempt-filters/absent": ns}
			},
			expectBlocked: true,
		}),
	)
})

func enableTopologyAwarePreemptionConfig(sc *e2eutil.SchedulerConfiguration) bool {
	sc.Actions = "allocate, preempt, backfill"

	// Test individual victim selection without gang minMember protection.
	disabled := false
	for i := range sc.Tiers {
		for j := range sc.Tiers[i].Plugins {
			if sc.Tiers[i].Plugins[j].Name == "gang" {
				sc.Tiers[i].Plugins[j].EnabledPreemptable = &disabled
			}
		}
	}

	for i := range sc.Configurations {
		if sc.Configurations[i].Name == "preempt" {
			if sc.Configurations[i].Arguments == nil {
				sc.Configurations[i].Arguments = map[string]string{}
			}
			sc.Configurations[i].Arguments["enableTopologyAwarePreemption"] = "true"
			return true
		}
	}
	sc.Configurations = append(sc.Configurations, e2eutil.Configuration{
		Name:      "preempt",
		Arguments: map[string]string{"enableTopologyAwarePreemption": "true"},
	})
	return true
}

func newPreemptFilterPod(namespace, name, priority, node string, req v1.ResourceList) *v1.Pod {
	grace := int64(1)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{"scheduling.k8s.io/group-name": name},
		},
		Spec: v1.PodSpec{
			SchedulerName:                 e2eutil.SchedulerName,
			PriorityClassName:             priority,
			RestartPolicy:                 v1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &grace,
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchFields: []v1.NodeSelectorRequirement{
									{
										Key:      "metadata.name",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{node},
									},
								},
							},
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:            "app",
					Image:           e2eutil.DefaultBusyBoxImage,
					ImagePullPolicy: v1.PullIfNotPresent,
					Command:         []string{"sleep", "3600"},
					Resources:       v1.ResourceRequirements{Requests: req},
				},
			},
		},
	}
}

func setHostPort(pod *v1.Pod, port int32) {
	pod.Spec.Containers[0].Ports = []v1.ContainerPort{
		{
			ContainerPort: port,
			HostPort:      port,
			Protocol:      v1.ProtocolTCP,
		},
	}
}

func createPreemptFilterPod(ctx context.Context, tc *e2eutil.TestContext, pod *v1.Pod) *v1.Pod {
	_, err := tc.Vcclient.SchedulingV1beta1().PodGroups(tc.Namespace).Create(ctx, &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: tc.Namespace},
		Spec: schedulingv1beta1.PodGroupSpec{
			MinMember:         1,
			Queue:             "default",
			PriorityClassName: pod.Spec.PriorityClassName,
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	created, err := tc.Kubeclient.CoreV1().Pods(tc.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return created
}

func waitPreemptFilterPodReady(ctx context.Context, tc *e2eutil.TestContext, pod *v1.Pod) {
	Eventually(func(g Gomega) {
		current, err := tc.Kubeclient.CoreV1().Pods(tc.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(current.UID).To(Equal(pod.UID))
		ready := false
		for _, cond := range current.Status.Conditions {
			if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
				ready = true
			}
		}
		g.Expect(ready).To(BeTrue())
	}, 2*time.Minute, time.Second).Should(Succeed())
}

func assertPodUntouched(g Gomega, ctx context.Context, tc *e2eutil.TestContext, pod *v1.Pod) {
	current, err := tc.Kubeclient.CoreV1().Pods(tc.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(current.UID).To(Equal(pod.UID))
	g.Expect(current.DeletionTimestamp).To(BeNil())
}
