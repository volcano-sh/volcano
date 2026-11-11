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
	"os"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumerestrictions"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2epv "k8s.io/kubernetes/test/e2e/framework/pv"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	admissionapi "k8s.io/pod-security-admission/api"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Volume Restrictions", func() {
	f := framework.NewDefaultFramework("volume-restrictions")
	f.NamespacePodSecurityLevel = admissionapi.LevelBaseline
	var storageClass string
	ginkgo.BeforeEach(func(ctx context.Context) {
		storageClass = os.Getenv("VOLUME_RESTRICTIONS_STORAGE_CLASS")
		if storageClass == "" {
			storageClass = "csi-hostpath-sc"
		}
		_, err := f.ClientSet.StorageV1().StorageClasses().Get(ctx, storageClass, metav1.GetOptions{})
		if apierrors.IsNotFound(err) && os.Getenv("VOLUME_RESTRICTIONS_STORAGE_CLASS") == "" {
			e2eskipper.Skipf("requires an RWOP-capable CSI storage class %q; set VOLUME_RESTRICTIONS_STORAGE_CLASS to override", storageClass)
		}
		framework.ExpectNoError(err)
	})
	ginkgo.DescribeTable("PVC sharing through Volcano", func(ctx context.Context, mode v1.PersistentVolumeAccessMode) {
		claimSpec := e2epv.MakePersistentVolumeClaim(e2epv.PersistentVolumeClaimConfig{
			Name: "data", ClaimSize: "1Gi", StorageClassName: &storageClass,
			AccessModes: []v1.PersistentVolumeAccessMode{mode},
		}, f.Namespace.Name)
		claim, err := f.ClientSet.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Create(ctx, claimSpec, metav1.CreateOptions{})
		framework.ExpectNoError(err)
		makePod := func() *v1.Pod {
			pod, err := e2epod.MakeSecPod(&e2epod.Config{
				NS: f.Namespace.Name, PVCs: []*v1.PersistentVolumeClaim{claim},
				SecurityLevel: admissionapi.LevelBaseline,
			})
			framework.ExpectNoError(err)
			pod.Spec.SchedulerName = e2eutil.SchedulerName
			return pod
		}

		holder, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Create(ctx, makePod(), metav1.CreateOptions{})
		framework.ExpectNoError(err)
		framework.ExpectNoError(e2epod.WaitForPodNameRunningInNamespace(ctx, f.ClientSet, holder.Name, holder.Namespace))
		holder, err = f.ClientSet.CoreV1().Pods(holder.Namespace).Get(ctx, holder.Name, metav1.GetOptions{})
		framework.ExpectNoError(err)
		pending := makePod()
		// Use affinity rather than nodeName so that both pods go through Volcano.
		e2epod.SetNodeAffinity(&pending.Spec, holder.Spec.NodeName)
		contender, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Create(ctx, pending, metav1.CreateOptions{})
		framework.ExpectNoError(err)
		if mode == v1.ReadWriteOncePod {
			ginkgo.By("observing an RWOP scheduling rejection before binding")
			gomega.Eventually(ctx, func(g gomega.Gomega) {
				current, err := f.ClientSet.CoreV1().Pods(contender.Namespace).Get(ctx, contender.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(gomega.HaveOccurred())
				if current.Spec.NodeName != "" {
					gomega.StopTrying("RWOP contender was bound while the holder was still running").Now()
				}
				g.Expect(current.Spec.NodeName).To(gomega.BeEmpty())
				events, err := f.ClientSet.CoreV1().Events(contender.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + string(contender.UID)})
				g.Expect(err).NotTo(gomega.HaveOccurred())
				messages := ""
				for _, event := range events.Items {
					if event.Reason == "FailedScheduling" {
						messages += event.Message
					}
				}
				g.Expect(messages).To(gomega.ContainSubstring(volumerestrictions.ErrReasonReadWriteOncePodConflict))
			}, f.Timeouts.PodStart, time.Second).Should(gomega.Succeed())
			gomega.Consistently(ctx, func(g gomega.Gomega) {
				current, err := f.ClientSet.CoreV1().Pods(contender.Namespace).Get(ctx, contender.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(current.UID).To(gomega.Equal(contender.UID))
				g.Expect(current.Spec.NodeName).To(gomega.BeEmpty())
			}, 10*time.Second, time.Second).Should(gomega.Succeed())
			ginkgo.By("releasing the claim and scheduling the same waiting pod")
			e2epod.DeletePodOrFail(ctx, f.ClientSet, holder.Namespace, holder.Name)
		}
		framework.ExpectNoError(e2epod.WaitForPodNameRunningInNamespace(ctx, f.ClientSet, contender.Name, contender.Namespace))
		current, err := f.ClientSet.CoreV1().Pods(contender.Namespace).Get(ctx, contender.Name, metav1.GetOptions{})
		framework.ExpectNoError(err)
		gomega.Expect(current.UID).To(gomega.Equal(contender.UID))
		gomega.Expect(current.Spec.NodeName).To(gomega.Equal(holder.Spec.NodeName))
		if mode == v1.ReadWriteOnce {
			runningHolder, err := f.ClientSet.CoreV1().Pods(holder.Namespace).Get(ctx, holder.Name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			gomega.Expect(runningHolder.Status.Phase).To(gomega.Equal(v1.PodRunning))
		}
	},
		ginkgo.Entry("keeps an RWOP contender unbound until the holder is deleted", v1.ReadWriteOncePod),
		ginkgo.Entry("allows two RWO consumers on the same node", v1.ReadWriteOnce),
	)
})
