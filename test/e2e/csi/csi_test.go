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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// Installed by hack/install-csi-hostpath.sh with a limit of 1 volume per node.
	storageClass = "csi-hostpath-e2e"
	pluginPod    = "csi-hostpathplugin-0"

	lowPriority  = "csi-low-priority"
	highPriority = "csi-high-priority"
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

		names := []string{"shared-0", "shared-1", "shared-2"}
		for _, name := range names {
			createPod(ctx, name, "shared", "", node)
		}
		for _, name := range names {
			waitRunning(ctx, name)
		}
	})

	It("preempts a lower-priority pod to free the volume slot", func() {
		// Enqueue conflicts with preempt.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				actions := strings.TrimPrefix(sc.Actions, "enqueue, ")
				if actions == sc.Actions {
					return false
				}
				sc.Actions = actions
				return true
			})
		})
		defer cmc.UndoChanged()

		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{lowPriority: 10, highPriority: 100},
		})
		createPVC(ctx, "low-data")
		createPVC(ctx, "high-data")

		createPod(ctx, "low", "low-data", lowPriority, node)
		waitRunning(ctx, "low")

		createPod(ctx, "high", "high-data", highPriority, node)
		waitRunning(ctx, "high")
		Expect(waitGone(ctx, "low")).To(Succeed())
	})
})

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

// createPod creates a pod in its own single-member PodGroup, pinned to the driver's node.
func createPod(ctx *e2eutil.TestContext, name, claim, priorityClass, node string) {
	pg := &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Namespace: ctx.Namespace, Name: name},
		Spec:       schedulingv1beta1.PodGroupSpec{MinMember: 1, PriorityClassName: priorityClass},
	}
	_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create podgroup %s", name)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ctx.Namespace,
			Name:        name,
			Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: name},
		},
		Spec: corev1.PodSpec{
			SchedulerName:                 "volcano",
			PriorityClassName:             priorityClass,
			NodeSelector:                  map[string]string{corev1.LabelHostname: node},
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			Containers: []corev1.Container{{
				Name:         "c",
				Image:        e2eutil.DefaultNginxImage,
				Resources:    corev1.ResourceRequirements{Requests: e2eutil.CPUResource("100m")},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
			}},
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
				},
			}},
		},
	}
	_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", name)
}

func waitRunning(ctx *e2eutil.TestContext, name string) {
	pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(e2eutil.WaitPodReady(ctx, pod)).To(Succeed(), "pod %s did not reach Running", name)
}

// waitGone waits for the evicted pod to be deleted.
func waitGone(ctx *e2eutil.TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.TODO(), time.Second, e2eutil.TwoMinute, true,
		func(c context.Context) (bool, error) {
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(c, name, metav1.GetOptions{})
			return errors.IsNotFound(err), nil
		})
}
