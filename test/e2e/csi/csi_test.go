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

package csi

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Installed by hack/install-csi-hostpath.sh with a limit of 1 volume per node.
	storageClass = "csi-hostpath-e2e"
	pluginPod    = "csi-hostpathplugin-0"

	lowPriority  = "csi-low-priority"
	highPriority = "csi-high-priority"

	// Reason reported by the NodeVolumeLimits predicate when a node has no attach slot left.
	maxVolumeCountExceeded = "exceed max volume count"
)

var _ = Describe("CSI volume limit E2E Test", func() {
	var (
		ctx  *e2eutil.TestContext
		node string
	)

	BeforeEach(func() {
		pod, err := e2eutil.KubeClient.CoreV1().Pods("default").Get(context.TODO(), pluginPod, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "CSI hostpath driver is not installed")
		node = pod.Spec.NodeName
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	It("schedules pods sharing one PVC on a node whose limit is one volume", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
		createPVC(ctx, "shared")

		var pods []*corev1.Pod
		for _, name := range []string{"shared-0", "shared-1", "shared-2"} {
			pods = append(pods, createPod(ctx, name, "shared", "", node, e2eutil.CPUResource("100m")))
		}
		for _, pod := range pods {
			Expect(e2eutil.WaitPodReady(ctx, pod)).To(Succeed(), "pod %s did not reach Running", pod.Name)
		}
	})

	DescribeTable("preempts a lower-priority pod only when preemption is enabled",
		func(preemptorReq corev1.ResourceList) {
			// Enqueue conflicts with preempt, so both phases run without it.
			withoutPreempt := setActions(func(actions []string) []string {
				return remove(actions, "enqueue", "preempt")
			})
			defer withoutPreempt.UndoChanged()

			ctx = e2eutil.InitTestContext(e2eutil.Options{
				PriorityClasses: map[string]int32{lowPriority: 10, highPriority: 100},
			})
			createPVC(ctx, "low-data")
			createPVC(ctx, "high-data")

			low := createPod(ctx, "low", "low-data", lowPriority, node, e2eutil.CPUResource("100m"))
			Expect(e2eutil.WaitPodReady(ctx, low)).To(Succeed())

			By("Without preemption, the high-priority pod stays Pending on the node's volume limit")
			high := createPod(ctx, "high", "high-data", highPriority, node, preemptorReq)
			Expect(e2eutil.WaitPodUnschedulable(ctx, ctx.Namespace, high.Name, e2eutil.OneMinute)).To(Succeed())
			Expect(scheduledMessage(ctx, high.Name)).To(ContainSubstring(maxVolumeCountExceeded))
			Consistently(func() corev1.PodPhase { return podPhase(ctx, low.Name) }, 10*time.Second, time.Second).
				Should(Equal(corev1.PodRunning), "the low-priority pod must not be evicted without preempt")

			By("With preemption, the high-priority pod evicts the low-priority one")
			Expect(withoutPreempt.UndoChanged()).To(Succeed())
			withPreempt := setActions(func(actions []string) []string {
				return remove(actions, "enqueue")
			})
			defer withPreempt.UndoChanged()

			Expect(e2eutil.WaitPodReady(ctx, high)).To(Succeed())
			Expect(e2eutil.WaitPodGone(ctx, low.Name, ctx.Namespace)).To(Succeed())
		},
		Entry("preemptor with a CPU request", e2eutil.CPUResource("100m")),
		// A pod whose only scheduling constraint is its PVC must still be able to preempt
		// a lower-priority pod that has CPU requests.
		Entry("preemptor with only a PVC", corev1.ResourceList{}),
	)
})

// setActions edits the scheduler's action list and returns the change so it can be undone.
func setActions(edit func([]string) []string) *e2eutil.ConfigMapCase {
	cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
	Expect(cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
		return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
			actions := strings.Join(edit(strings.Split(sc.Actions, ", ")), ", ")
			if actions == sc.Actions {
				return false
			}
			sc.Actions = actions
			return true
		})
	})).To(Succeed())
	return cmc
}

func remove(actions []string, names ...string) []string {
	kept := make([]string, 0, len(actions))
	for _, a := range actions {
		drop := false
		for _, n := range names {
			drop = drop || strings.TrimSpace(a) == n
		}
		if !drop {
			kept = append(kept, a)
		}
	}
	return kept
}

func createPVC(ctx *e2eutil.TestContext, name string) {
	_, err := ctx.Kubeclient.CoreV1().PersistentVolumeClaims(ctx.Namespace).Create(context.TODO(), &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: ptr.To(storageClass),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create pvc %s", name)
}

// createPod creates a Volcano pod pinned to the driver's node that mounts claim. Volcano's
// podgroup controller creates its single-member PodGroup with the pod's priority class.
func createPod(ctx *e2eutil.TestContext, name, claim, priorityClass, node string, req corev1.ResourceList) *corev1.Pod {
	return e2eutil.CreatePod(ctx, e2eutil.PodSpec{
		Name:                          name,
		Req:                           req,
		SchedulerName:                 "volcano",
		PriorityClassName:             priorityClass,
		NodeSelector:                  map[string]string{corev1.LabelHostname: node},
		TerminationGracePeriodSeconds: ptr.To(int64(0)),
		Volumes: []corev1.Volume{{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
			},
		}},
	})
}

func scheduledMessage(ctx *e2eutil.TestContext, name string) string {
	pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled {
			return cond.Message
		}
	}
	return ""
}

func podPhase(ctx *e2eutil.TestContext, name string) corev1.PodPhase {
	pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return pod.Status.Phase
}
