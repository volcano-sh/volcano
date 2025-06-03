/*
Copyright 2022 The Kubernetes Authors.

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

package dra

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	e2edra "k8s.io/kubernetes/test/e2e/dra"
	"k8s.io/kubernetes/test/e2e/feature"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	admissionapi "k8s.io/pod-security-admission/api"
)

// The e2e test cases in this file are copied from kubernetes test/e2e/dra/dra.go, only contains scheduling-related e2e testing.
var _ = ginkgo.Describe("DRA E2E Test", func() {
	f := framework.NewDefaultFramework("dra")

	// The driver containers have to run with sufficient privileges to
	// modify /var/lib/kubelet/plugins.
	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged

	// claimTests tries out several different combinations of pods with
	// claims, both inline and external.
	claimTests := func(b *builder, driver *e2edra.Driver) {
		ginkgo.It("supports simple pod referencing inline resource claim", func(ctx context.Context) {
			pod, template := b.podInline()
			b.create(ctx, pod, template)
			b.testPod(ctx, f.ClientSet, pod)
		})

		ginkgo.It("supports inline claim referenced by multiple containers", func(ctx context.Context) {
			pod, template := b.podInlineMultiple()
			b.create(ctx, pod, template)
			b.testPod(ctx, f.ClientSet, pod)
		})

		ginkgo.It("supports simple pod referencing external resource claim", func(ctx context.Context) {
			pod := b.podExternal()
			claim := b.externalClaim()
			b.create(ctx, claim, pod)
			b.testPod(ctx, f.ClientSet, pod)
		})

		ginkgo.It("supports external claim referenced by multiple pods", func(ctx context.Context) {
			pod1 := b.podExternal()
			pod2 := b.podExternal()
			pod3 := b.podExternal()
			claim := b.externalClaim()
			b.create(ctx, claim, pod1, pod2, pod3)

			for _, pod := range []*v1.Pod{pod1, pod2, pod3} {
				b.testPod(ctx, f.ClientSet, pod)
			}
		})

		ginkgo.It("supports external claim referenced by multiple containers of multiple pods", func(ctx context.Context) {
			pod1 := b.podExternalMultiple()
			pod2 := b.podExternalMultiple()
			pod3 := b.podExternalMultiple()
			claim := b.externalClaim()
			b.create(ctx, claim, pod1, pod2, pod3)

			for _, pod := range []*v1.Pod{pod1, pod2, pod3} {
				b.testPod(ctx, f.ClientSet, pod)
			}
		})

		ginkgo.It("supports init containers", func(ctx context.Context) {
			pod, template := b.podInline()
			pod.Spec.InitContainers = []v1.Container{pod.Spec.Containers[0]}
			pod.Spec.InitContainers[0].Name += "-init"
			// This must succeed for the pod to start.
			pod.Spec.InitContainers[0].Command = []string{"sh", "-c", "env | grep user_a=b"}
			b.create(ctx, pod, template)

			b.testPod(ctx, f.ClientSet, pod)
		})

		ginkgo.It("removes reservation from claim when pod is done", func(ctx context.Context) {
			pod := b.podExternal()
			claim := b.externalClaim()
			pod.Spec.Containers[0].Command = []string{"true"}
			b.create(ctx, claim, pod)

			ginkgo.By("waiting for pod to finish")
			framework.ExpectNoError(e2epod.WaitForPodNoLongerRunningInNamespace(ctx, f.ClientSet, pod.Name, pod.Namespace), "wait for pod to finish")
			ginkgo.By("waiting for claim to be unreserved")
			gomega.Eventually(ctx, func(ctx context.Context) (*resourceapi.ResourceClaim, error) {
				return f.ClientSet.ResourceV1beta1().ResourceClaims(pod.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
			}).WithTimeout(f.Timeouts.PodDelete).Should(gomega.HaveField("Status.ReservedFor", gomega.BeEmpty()), "reservation should have been removed")
		})

		ginkgo.It("deletes generated claims when pod is done", func(ctx context.Context) {
			pod, template := b.podInline()
			pod.Spec.Containers[0].Command = []string{"true"}
			b.create(ctx, template, pod)

			ginkgo.By("waiting for pod to finish")
			framework.ExpectNoError(e2epod.WaitForPodNoLongerRunningInNamespace(ctx, f.ClientSet, pod.Name, pod.Namespace), "wait for pod to finish")
			ginkgo.By("waiting for claim to be deleted")
			gomega.Eventually(ctx, func(ctx context.Context) ([]resourceapi.ResourceClaim, error) {
				claims, err := f.ClientSet.ResourceV1beta1().ResourceClaims(pod.Namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					return nil, err
				}
				return claims.Items, nil
			}).WithTimeout(f.Timeouts.PodDelete).Should(gomega.BeEmpty(), "claim should have been deleted")
		})

		ginkgo.It("does not delete generated claims when pod is restarting", func(ctx context.Context) {
			pod, template := b.podInline()
			pod.Spec.Containers[0].Command = []string{"sh", "-c", "sleep 1; exit 1"}
			pod.Spec.RestartPolicy = v1.RestartPolicyAlways
			b.create(ctx, template, pod)

			ginkgo.By("waiting for pod to restart twice")
			gomega.Eventually(ctx, func(ctx context.Context) (*v1.Pod, error) {
				return f.ClientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			}).WithTimeout(f.Timeouts.PodStartSlow).Should(gomega.HaveField("Status.ContainerStatuses", gomega.ContainElements(gomega.HaveField("RestartCount", gomega.BeNumerically(">=", 2)))))
		})

		ginkgo.It("must deallocate after use", func(ctx context.Context) {
			pod := b.podExternal()
			claim := b.externalClaim()
			b.create(ctx, claim, pod)

			gomega.Eventually(ctx, func(ctx context.Context) (*resourceapi.ResourceClaim, error) {
				return b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Get(ctx, claim.Name, metav1.GetOptions{})
			}).WithTimeout(f.Timeouts.PodDelete).ShouldNot(gomega.HaveField("Status.Allocation", (*resourceapi.AllocationResult)(nil)))

			b.testPod(ctx, f.ClientSet, pod)

			ginkgo.By(fmt.Sprintf("deleting pod %s", klog.KObj(pod)))
			framework.ExpectNoError(b.f.ClientSet.CoreV1().Pods(b.f.Namespace.Name).Delete(ctx, pod.Name, metav1.DeleteOptions{}))

			ginkgo.By("waiting for claim to get deallocated")
			gomega.Eventually(ctx, func(ctx context.Context) (*resourceapi.ResourceClaim, error) {
				return b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Get(ctx, claim.Name, metav1.GetOptions{})
			}).WithTimeout(f.Timeouts.PodDelete).Should(gomega.HaveField("Status.Allocation", (*resourceapi.AllocationResult)(nil)))
		})

		f.It("must be possible for the driver to update the ResourceClaim.Status.Devices once allocated", feature.DRAResourceClaimDeviceStatus, func(ctx context.Context) {
			pod := b.podExternal()
			claim := b.externalClaim()
			b.create(ctx, claim, pod)

			b.testPod(ctx, f.ClientSet, pod)

			allocatedResourceClaim, err := b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Get(ctx, claim.Name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			gomega.Expect(allocatedResourceClaim).ToNot(gomega.BeNil())
			gomega.Expect(allocatedResourceClaim.Status.Allocation).ToNot(gomega.BeNil())
			gomega.Expect(allocatedResourceClaim.Status.Allocation.Devices.Results).To(gomega.HaveLen(1))

			scheduledPod, err := b.f.ClientSet.CoreV1().Pods(b.f.Namespace.Name).Get(ctx, pod.Name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			gomega.Expect(scheduledPod).ToNot(gomega.BeNil())

			gomega.Expect(allocatedResourceClaim.Status.Allocation).ToNot(gomega.BeNil())
			gomega.Expect(allocatedResourceClaim.Status.Allocation.Devices.Results).To(gomega.HaveLen(1))

			ginkgo.By("Setting the device status a first time")
			allocatedResourceClaim.Status.Devices = append(allocatedResourceClaim.Status.Devices,
				resourceapi.AllocatedDeviceStatus{
					Driver:     allocatedResourceClaim.Status.Allocation.Devices.Results[0].Driver,
					Pool:       allocatedResourceClaim.Status.Allocation.Devices.Results[0].Pool,
					Device:     allocatedResourceClaim.Status.Allocation.Devices.Results[0].Device,
					Conditions: []metav1.Condition{{Type: "a", Status: "True", Message: "c", Reason: "d", LastTransitionTime: metav1.NewTime(time.Now().Truncate(time.Second))}},
					Data:       runtime.RawExtension{Raw: []byte(`{"foo":"bar"}`)},
					NetworkData: &resourceapi.NetworkDeviceData{
						InterfaceName:   "inf1",
						IPs:             []string{"10.9.8.0/24", "2001:db8::/64"},
						HardwareAddress: "bc:1c:b6:3e:b8:25",
					},
				})
			// Updates the ResourceClaim from the driver on the same node as the pod.
			plugin, ok := driver.Nodes[scheduledPod.Spec.NodeName]
			if !ok {
				framework.Failf("pod got scheduled to node %s without a plugin", scheduledPod.Spec.NodeName)
			}
			updatedResourceClaim, err := plugin.UpdateStatus(ctx, allocatedResourceClaim)
			framework.ExpectNoError(err)
			gomega.Expect(updatedResourceClaim).ToNot(gomega.BeNil())
			gomega.Expect(updatedResourceClaim.Status.Devices).To(gomega.Equal(allocatedResourceClaim.Status.Devices))

			ginkgo.By("Updating the device status")
			updatedResourceClaim.Status.Devices[0] = resourceapi.AllocatedDeviceStatus{
				Driver:     allocatedResourceClaim.Status.Allocation.Devices.Results[0].Driver,
				Pool:       allocatedResourceClaim.Status.Allocation.Devices.Results[0].Pool,
				Device:     allocatedResourceClaim.Status.Allocation.Devices.Results[0].Device,
				Conditions: []metav1.Condition{{Type: "e", Status: "True", Message: "g", Reason: "h", LastTransitionTime: metav1.NewTime(time.Now().Truncate(time.Second))}},
				Data:       runtime.RawExtension{Raw: []byte(`{"bar":"foo"}`)},
				NetworkData: &resourceapi.NetworkDeviceData{
					InterfaceName:   "inf2",
					IPs:             []string{"10.9.8.1/24", "2001:db8::1/64"},
					HardwareAddress: "bc:1c:b6:3e:b8:26",
				},
			}

			updatedResourceClaim2, err := plugin.UpdateStatus(ctx, updatedResourceClaim)
			framework.ExpectNoError(err)
			gomega.Expect(updatedResourceClaim2).ToNot(gomega.BeNil())
			gomega.Expect(updatedResourceClaim2.Status.Devices).To(gomega.Equal(updatedResourceClaim.Status.Devices))

			getResourceClaim, err := b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Get(ctx, claim.Name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			gomega.Expect(getResourceClaim).ToNot(gomega.BeNil())
			gomega.Expect(getResourceClaim.Status.Devices).To(gomega.Equal(updatedResourceClaim.Status.Devices))
		})
	}

	singleNodeTests := func() {
		nodes := e2edra.NewNodes(f, 1, 1)
		maxAllocations := 1
		numPods := 10
		generateResources := func() e2edra.Resources {
			resources := perNode(maxAllocations, nodes)()
			return resources
		}
		driver := e2edra.NewDriver(f, nodes, generateResources) // All tests get their own driver instance.
		b := newBuilder(f, driver)
		// We have to set the parameters *before* creating the class.
		b.classParameters = `{"x":"y"}`
		expectedEnv := []string{"admin_x", "y"}
		_, expected := b.parametersEnv()
		expectedEnv = append(expectedEnv, expected...)

		ginkgo.It("supports claim and class parameters", func(ctx context.Context) {
			pod, template := b.podInline()
			b.create(ctx, pod, template)
			b.testPod(ctx, f.ClientSet, pod, expectedEnv...)
		})

		ginkgo.It("supports reusing resources", func(ctx context.Context) {
			var objects []klog.KMetadata
			pods := make([]*v1.Pod, numPods)
			for i := 0; i < numPods; i++ {
				pod, template := b.podInline()
				pods[i] = pod
				objects = append(objects, pod, template)
			}

			b.create(ctx, objects...)

			// We don't know the order. All that matters is that all of them get scheduled eventually.
			var wg sync.WaitGroup
			wg.Add(numPods)
			for i := 0; i < numPods; i++ {
				pod := pods[i]
				go func() {
					defer ginkgo.GinkgoRecover()
					defer wg.Done()
					b.testPod(ctx, f.ClientSet, pod, expectedEnv...)
					err := f.ClientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
					framework.ExpectNoError(err, "delete pod")
					framework.ExpectNoError(e2epod.WaitForPodNotFoundInNamespace(ctx, f.ClientSet, pod.Name, pod.Namespace, time.Duration(numPods)*f.Timeouts.PodStartSlow))
				}()
			}
			wg.Wait()
		})

		ginkgo.It("supports sharing a claim concurrently", func(ctx context.Context) {
			var objects []klog.KMetadata
			objects = append(objects, b.externalClaim())
			pods := make([]*v1.Pod, numPods)
			for i := 0; i < numPods; i++ {
				pod := b.podExternal()
				pods[i] = pod
				objects = append(objects, pod)
			}

			b.create(ctx, objects...)

			// We don't know the order. All that matters is that all of them get scheduled eventually.
			f.Timeouts.PodStartSlow *= time.Duration(numPods)
			var wg sync.WaitGroup
			wg.Add(numPods)
			for i := 0; i < numPods; i++ {
				pod := pods[i]
				go func() {
					defer ginkgo.GinkgoRecover()
					defer wg.Done()
					b.testPod(ctx, f.ClientSet, pod, expectedEnv...)
				}()
			}
			wg.Wait()
		})

		ginkgo.It("retries pod scheduling after creating device class", func(ctx context.Context) {
			var objects []klog.KMetadata
			pod, template := b.podInline()
			deviceClassName := template.Spec.Spec.Devices.Requests[0].DeviceClassName
			class, err := f.ClientSet.ResourceV1beta1().DeviceClasses().Get(ctx, deviceClassName, metav1.GetOptions{})
			framework.ExpectNoError(err)
			deviceClassName += "-b"
			template.Spec.Spec.Devices.Requests[0].DeviceClassName = deviceClassName
			objects = append(objects, template, pod)
			b.create(ctx, objects...)

			framework.ExpectNoError(e2epod.WaitForPodNameUnschedulableInNamespace(ctx, f.ClientSet, pod.Name, pod.Namespace))

			class.UID = ""
			class.ResourceVersion = ""
			class.Name = deviceClassName
			b.create(ctx, class)

			b.testPod(ctx, f.ClientSet, pod, expectedEnv...)
		})

		ginkgo.It("retries pod scheduling after updating device class", func(ctx context.Context) {
			var objects []klog.KMetadata
			pod, template := b.podInline()

			// First modify the class so that it matches no nodes (for classic DRA) and no devices (structured parameters).
			deviceClassName := template.Spec.Spec.Devices.Requests[0].DeviceClassName
			class, err := f.ClientSet.ResourceV1beta1().DeviceClasses().Get(ctx, deviceClassName, metav1.GetOptions{})
			framework.ExpectNoError(err)
			originalClass := class.DeepCopy()
			class.Spec.Selectors = []resourceapi.DeviceSelector{{
				CEL: &resourceapi.CELDeviceSelector{
					Expression: "false",
				},
			}}
			class, err = f.ClientSet.ResourceV1beta1().DeviceClasses().Update(ctx, class, metav1.UpdateOptions{})
			framework.ExpectNoError(err)

			// Now create the pod.
			objects = append(objects, template, pod)
			b.create(ctx, objects...)

			framework.ExpectNoError(e2epod.WaitForPodNameUnschedulableInNamespace(ctx, f.ClientSet, pod.Name, pod.Namespace))

			// Unblock the pod.
			class.Spec.Selectors = originalClass.Spec.Selectors
			_, err = f.ClientSet.ResourceV1beta1().DeviceClasses().Update(ctx, class, metav1.UpdateOptions{})
			framework.ExpectNoError(err)

			b.testPod(ctx, f.ClientSet, pod, expectedEnv...)
		})

		ginkgo.It("runs a pod without a generated resource claim", func(ctx context.Context) {
			pod, _ /* template */ := b.podInline()
			created := b.create(ctx, pod)
			pod = created[0].(*v1.Pod)

			// Normally, this pod would be stuck because the
			// ResourceClaim cannot be created without the
			// template. We allow it to run by communicating
			// through the status that the ResourceClaim is not
			// needed.
			pod.Status.ResourceClaimStatuses = []v1.PodResourceClaimStatus{
				{Name: pod.Spec.ResourceClaims[0].Name, ResourceClaimName: nil},
			}
			_, err := f.ClientSet.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
			framework.ExpectNoError(err)
			framework.ExpectNoError(e2epod.WaitForPodRunningInNamespace(ctx, f.ClientSet, pod))
		})

		claimTests(b, driver)
	}

	// The following tests only make sense when there is more than one node.
	// They get skipped when there's only one node.
	multiNodeTests := func() {
		nodes := e2edra.NewNodes(f, 3, 8)

		ginkgo.Context("with node-local resources", func() {
			driver := e2edra.NewDriver(f, nodes, perNode(1, nodes))
			b := newBuilder(f, driver)

			ginkgo.It("uses all resources", func(ctx context.Context) {
				var objs []klog.KMetadata
				var pods []*v1.Pod
				for i := 0; i < len(nodes.NodeNames); i++ {
					pod, template := b.podInline()
					pods = append(pods, pod)
					objs = append(objs, pod, template)
				}
				b.create(ctx, objs...)

				for _, pod := range pods {
					err := e2epod.WaitForPodRunningInNamespace(ctx, f.ClientSet, pod)
					framework.ExpectNoError(err, "start pod")
				}

				// The pods all should run on different
				// nodes because the maximum number of
				// claims per node was limited to 1 for
				// this test.
				//
				// We cannot know for sure why the pods
				// ran on two different nodes (could
				// also be a coincidence) but if they
				// don't cover all nodes, then we have
				// a problem.
				used := make(map[string]*v1.Pod)
				for _, pod := range pods {
					pod, err := f.ClientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
					framework.ExpectNoError(err, "get pod")
					nodeName := pod.Spec.NodeName
					if other, ok := used[nodeName]; ok {
						framework.Failf("Pod %s got started on the same node %s as pod %s although claim allocation should have been limited to one claim per node.", pod.Name, nodeName, other.Name)
					}
					used[nodeName] = pod
				}
			})
		})
	}

	ginkgo.Context("on single node", func() {
		singleNodeTests()
	})

	ginkgo.Context("on multiple nodes", func() {
		multiNodeTests()
	})
})

// perNode returns a function which can be passed to NewDriver. The nodes
// parameter has be instantiated, but not initialized yet, so the returned
// function has to capture it and use it when being called.
func perNode(maxAllocations int, nodes *e2edra.Nodes) func() e2edra.Resources {
	return func() e2edra.Resources {
		return e2edra.Resources{
			NodeLocal:      true,
			MaxAllocations: maxAllocations,
			Nodes:          nodes.NodeNames,
		}
	}
}
