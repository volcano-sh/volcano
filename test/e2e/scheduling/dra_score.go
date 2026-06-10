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

package scheduling

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	drautils "k8s.io/kubernetes/test/e2e/dra/utils"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	admissionapi "k8s.io/pod-security-admission/api"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("DRA Score E2E Test", func() {
	f := k8sframework.NewDefaultFramework("dra-score")
	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged
	nodes := drautils.NewNodes(f, 2, 2)

	driver := drautils.NewDriver(f, nodes, drautils.DriverResources(2))

	var deviceClassName string

	BeforeEach(func() {
		deviceClassName = "dra-score-class-" + f.Namespace.Name
		ensureDeviceClass(driver.Name, deviceClassName)
	})

	It("schedules pods on prioritized nodes based on DRA scores", func(ctx context.Context) {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dra-scoring-pod",
				Namespace: f.Namespace.Name,
			},
			Spec: v1.PodSpec{
				SchedulerName: "volcano",
				Containers: []v1.Container{
					{
						Name:  "nginx",
						Image: e2eutil.DefaultNginxImage,
					},
				},
				ResourceClaims: []v1.PodResourceClaim{
					{
						Name:                      "claim-req",
						ResourceClaimTemplateName: ptrString("dra-score-claim-template"),
					},
				},
			},
		}

		template := &resourcev1.ResourceClaimTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dra-score-claim-template",
				Namespace: f.Namespace.Name,
			},
			Spec: resourcev1.ResourceClaimTemplateSpec{
				Spec: resourcev1.ResourceClaimSpec{
					Devices: resourcev1.DeviceClaim{
						Requests: []resourcev1.DeviceRequest{
							{
								Name: "req-1",
								Exactly: &resourcev1.ExactDeviceRequest{
									DeviceClassName: deviceClassName,
									Count:           1,
									AllocationMode:  resourcev1.DeviceAllocationModeExactCount,
								},
							},
						},
					},
				},
			},
		}

		_, err := f.ClientSet.ResourceV1().ResourceClaimTemplates(f.Namespace.Name).Create(ctx, template, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.ClientSet.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2epod.WaitForPodRunningInNamespace(ctx, f.ClientSet, pod)
		Expect(err).NotTo(HaveOccurred())

		scheduledPod, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Get(ctx, pod.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(scheduledPod.Spec.NodeName).NotTo(BeEmpty())
	})
})

func ensureDeviceClass(driverName, className string) {
	class := &resourcev1.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: className,
		},
		Spec: resourcev1.DeviceClassSpec{
			Selectors: []resourcev1.DeviceSelector{{
				CEL: &resourcev1.CELDeviceSelector{
					Expression: fmt.Sprintf(`device.driver == %q`, driverName),
				},
			}},
		},
	}

	_, err := e2eutil.KubeClient.ResourceV1().DeviceClasses().Create(context.Background(), class, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	DeferCleanup(func() {
		err := e2eutil.KubeClient.ResourceV1().DeviceClasses().Delete(context.Background(), className, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})
}

func ptrString(s string) *string {
	return &s
}
