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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	e2edra "k8s.io/kubernetes/test/e2e/dra"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	"k8s.io/utils/ptr"
)

// This file is copied from kubernetes test/e2e/dra/dra.go, and change the schedulerName of the pod to volcano.
const (
	schedulerName = "volcano"
)

// builder contains a running counter to make objects unique within thir
// namespace.
type builder struct {
	f      *framework.Framework
	driver *e2edra.Driver

	podCounter      int
	claimCounter    int
	classParameters string // JSON
}

// className returns the default device class name.
func (b *builder) className() string {
	return b.f.UniqueName + b.driver.NameSuffix + "-class"
}

// class returns the device class that the builder's other objects
// reference.
func (b *builder) class() *resourceapi.DeviceClass {
	class := &resourceapi.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: b.className(),
		},
	}
	class.Spec.Selectors = []resourceapi.DeviceSelector{{
		CEL: &resourceapi.CELDeviceSelector{
			Expression: fmt.Sprintf(`device.driver == "%s"`, b.driver.Name),
		},
	}}
	if b.classParameters != "" {
		class.Spec.Config = []resourceapi.DeviceClassConfiguration{{
			DeviceConfiguration: resourceapi.DeviceConfiguration{
				Opaque: &resourceapi.OpaqueDeviceConfiguration{
					Driver:     b.driver.Name,
					Parameters: runtime.RawExtension{Raw: []byte(b.classParameters)},
				},
			},
		}}
	}
	return class
}

// externalClaim returns external resource claim
// that test pods can reference
func (b *builder) externalClaim() *resourceapi.ResourceClaim {
	b.claimCounter++
	name := "external-claim" + b.driver.NameSuffix // This is what podExternal expects.
	if b.claimCounter > 1 {
		name += fmt.Sprintf("-%d", b.claimCounter)
	}
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: b.claimSpec(),
	}
}

// claimSpec returns the device request for a claim or claim template
// with the associated config
func (b *builder) claimSpec() resourceapi.ResourceClaimSpec {
	parameters, _ := b.parametersEnv()
	spec := resourceapi.ResourceClaimSpec{
		Devices: resourceapi.DeviceClaim{
			Requests: []resourceapi.DeviceRequest{{
				Name:            "my-request",
				DeviceClassName: b.className(),
			}},
			Config: []resourceapi.DeviceClaimConfiguration{{
				DeviceConfiguration: resourceapi.DeviceConfiguration{
					Opaque: &resourceapi.OpaqueDeviceConfiguration{
						Driver: b.driver.Name,
						Parameters: runtime.RawExtension{
							Raw: []byte(parameters),
						},
					},
				},
			}},
		},
	}

	return spec
}

// parametersEnv returns the default user env variables as JSON (config) and key/value list (pod env).
func (b *builder) parametersEnv() (string, []string) {
	return `{"a":"b"}`,
		[]string{"user_a", "b"}
}

// makePod returns a simple pod with no resource claims.
// The pod prints its env and waits.
func (b *builder) pod() *v1.Pod {
	pod := e2epod.MakePod(b.f.Namespace.Name, nil, nil, b.f.NamespacePodSecurityLevel, "env && sleep 100000")
	pod.Labels = make(map[string]string)
	pod.Spec.RestartPolicy = v1.RestartPolicyNever
	// Let kubelet kill the pods quickly. Setting
	// TerminationGracePeriodSeconds to zero would bypass kubelet
	// completely because then the apiserver enables a force-delete even
	// when DeleteOptions for the pod don't ask for it (see
	// https://github.com/kubernetes/kubernetes/blob/0f582f7c3f504e807550310d00f130cb5c18c0c3/pkg/registry/core/pod/strategy.go#L151-L171).
	//
	// We don't do that because it breaks tracking of claim usage: the
	// kube-controller-manager assumes that kubelet is done with the pod
	// once it got removed or has a grace period of 0. Setting the grace
	// period to zero directly in DeletionOptions or indirectly through
	// TerminationGracePeriodSeconds causes the controller to remove
	// the pod from ReservedFor before it actually has stopped on
	// the node.
	one := int64(1)
	pod.Spec.TerminationGracePeriodSeconds = &one
	pod.ObjectMeta.GenerateName = ""
	b.podCounter++
	pod.ObjectMeta.Name = fmt.Sprintf("tester%s-%d", b.driver.NameSuffix, b.podCounter)
	pod.Spec.SchedulerName = schedulerName
	return pod
}

// makePodInline adds an inline resource claim with default class name and parameters.
func (b *builder) podInline() (*v1.Pod, *resourceapi.ResourceClaimTemplate) {
	pod := b.pod()
	pod.Spec.Containers[0].Name = "with-resource"
	podClaimName := "my-inline-claim"
	pod.Spec.Containers[0].Resources.Claims = []v1.ResourceClaim{{Name: podClaimName}}
	pod.Spec.ResourceClaims = []v1.PodResourceClaim{
		{
			Name:                      podClaimName,
			ResourceClaimTemplateName: ptr.To(pod.Name),
		},
	}
	template := &resourceapi.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Spec: resourceapi.ResourceClaimTemplateSpec{
			Spec: b.claimSpec(),
		},
	}
	return pod, template
}

// podInlineMultiple returns a pod with inline resource claim referenced by 3 containers
func (b *builder) podInlineMultiple() (*v1.Pod, *resourceapi.ResourceClaimTemplate) {
	pod, template := b.podInline()
	pod.Spec.Containers = append(pod.Spec.Containers, *pod.Spec.Containers[0].DeepCopy(), *pod.Spec.Containers[0].DeepCopy())
	pod.Spec.Containers[1].Name = pod.Spec.Containers[1].Name + "-1"
	pod.Spec.Containers[2].Name = pod.Spec.Containers[1].Name + "-2"
	return pod, template
}

// podExternal adds a pod that references external resource claim with default class name and parameters.
func (b *builder) podExternal() *v1.Pod {
	pod := b.pod()
	pod.Spec.Containers[0].Name = "with-resource"
	podClaimName := "resource-claim"
	externalClaimName := "external-claim" + b.driver.NameSuffix
	pod.Spec.ResourceClaims = []v1.PodResourceClaim{
		{
			Name:              podClaimName,
			ResourceClaimName: &externalClaimName,
		},
	}
	pod.Spec.Containers[0].Resources.Claims = []v1.ResourceClaim{{Name: podClaimName}}
	return pod
}

// podShared returns a pod with 3 containers that reference external resource claim with default class name and parameters.
func (b *builder) podExternalMultiple() *v1.Pod {
	pod := b.podExternal()
	pod.Spec.Containers = append(pod.Spec.Containers, *pod.Spec.Containers[0].DeepCopy(), *pod.Spec.Containers[0].DeepCopy())
	pod.Spec.Containers[1].Name = pod.Spec.Containers[1].Name + "-1"
	pod.Spec.Containers[2].Name = pod.Spec.Containers[1].Name + "-2"
	return pod
}

// create takes a bunch of objects and calls their Create function.
func (b *builder) create(ctx context.Context, objs ...klog.KMetadata) []klog.KMetadata {
	var createdObjs []klog.KMetadata
	for _, obj := range objs {
		ginkgo.By(fmt.Sprintf("creating %T %s", obj, obj.GetName()))
		var err error
		var createdObj klog.KMetadata
		switch obj := obj.(type) {
		case *resourceapi.DeviceClass:
			createdObj, err = b.f.ClientSet.ResourceV1beta1().DeviceClasses().Create(ctx, obj, metav1.CreateOptions{})
			ginkgo.DeferCleanup(func(ctx context.Context) {
				err := b.f.ClientSet.ResourceV1beta1().DeviceClasses().Delete(ctx, createdObj.GetName(), metav1.DeleteOptions{})
				framework.ExpectNoError(err, "delete device class")
			})
		case *v1.Pod:
			createdObj, err = b.f.ClientSet.CoreV1().Pods(b.f.Namespace.Name).Create(ctx, obj, metav1.CreateOptions{})
		case *v1.ConfigMap:
			createdObj, err = b.f.ClientSet.CoreV1().ConfigMaps(b.f.Namespace.Name).Create(ctx, obj, metav1.CreateOptions{})
		case *resourceapi.ResourceClaim:
			createdObj, err = b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Create(ctx, obj, metav1.CreateOptions{})
		case *resourceapi.ResourceClaimTemplate:
			createdObj, err = b.f.ClientSet.ResourceV1beta1().ResourceClaimTemplates(b.f.Namespace.Name).Create(ctx, obj, metav1.CreateOptions{})
		case *resourceapi.ResourceSlice:
			createdObj, err = b.f.ClientSet.ResourceV1beta1().ResourceSlices().Create(ctx, obj, metav1.CreateOptions{})
			ginkgo.DeferCleanup(func(ctx context.Context) {
				err := b.f.ClientSet.ResourceV1beta1().ResourceSlices().Delete(ctx, createdObj.GetName(), metav1.DeleteOptions{})
				framework.ExpectNoError(err, "delete node resource slice")
			})
		case *appsv1.DaemonSet:
			createdObj, err = b.f.ClientSet.AppsV1().DaemonSets(b.f.Namespace.Name).Create(ctx, obj, metav1.CreateOptions{})
			// Cleanup not really needed, but speeds up namespace shutdown.
			ginkgo.DeferCleanup(func(ctx context.Context) {
				err := b.f.ClientSet.AppsV1().DaemonSets(b.f.Namespace.Name).Delete(ctx, obj.Name, metav1.DeleteOptions{})
				framework.ExpectNoError(err, "delete daemonset")
			})
		default:
			framework.Fail(fmt.Sprintf("internal error, unsupported type %T", obj), 1)
		}
		framework.ExpectNoErrorWithOffset(1, err, "create %T", obj)
		createdObjs = append(createdObjs, createdObj)
	}
	return createdObjs
}

// testPod runs pod and checks if container logs contain expected environment variables
func (b *builder) testPod(ctx context.Context, clientSet kubernetes.Interface, pod *v1.Pod, env ...string) {
	ginkgo.GinkgoHelper()
	err := e2epod.WaitForPodRunningInNamespace(ctx, clientSet, pod)
	framework.ExpectNoError(err, "start pod")

	if len(env) == 0 {
		_, env = b.parametersEnv()
	}
	for _, container := range pod.Spec.Containers {
		testContainerEnv(ctx, clientSet, pod, container.Name, false, env...)
	}
}

// envLineRE matches env output with variables set by test/e2e/dra/test-driver.
var envLineRE = regexp.MustCompile(`^(?:admin|user|claim)_[a-zA-Z0-9_]*=.*$`)

func testContainerEnv(ctx context.Context, clientSet kubernetes.Interface, pod *v1.Pod, containerName string, fullMatch bool, env ...string) {
	ginkgo.GinkgoHelper()
	log, err := e2epod.GetPodLogs(ctx, clientSet, pod.Namespace, pod.Name, containerName)
	framework.ExpectNoError(err, fmt.Sprintf("get logs for container %s", containerName))
	if fullMatch {
		// Find all env variables set by the test driver.
		var actualEnv, expectEnv []string
		for _, line := range strings.Split(log, "\n") {
			if envLineRE.MatchString(line) {
				actualEnv = append(actualEnv, line)
			}
		}
		for i := 0; i < len(env); i += 2 {
			expectEnv = append(expectEnv, env[i]+"="+env[i+1])
		}
		sort.Strings(actualEnv)
		sort.Strings(expectEnv)
		gomega.Expect(actualEnv).To(gomega.Equal(expectEnv), fmt.Sprintf("container %s log output:\n%s", containerName, log))
	} else {
		for i := 0; i < len(env); i += 2 {
			envStr := fmt.Sprintf("\n%s=%s\n", env[i], env[i+1])
			gomega.Expect(log).To(gomega.ContainSubstring(envStr), fmt.Sprintf("container %s env variables", containerName))
		}
	}
}

func newBuilder(f *framework.Framework, driver *e2edra.Driver) *builder {
	b := &builder{f: f, driver: driver}
	ginkgo.BeforeEach(b.setUp)
	return b
}

func newBuilderNow(ctx context.Context, f *framework.Framework, driver *e2edra.Driver) *builder {
	b := &builder{f: f, driver: driver}
	b.setUp(ctx)
	return b
}

func (b *builder) setUp(ctx context.Context) {
	b.podCounter = 0
	b.claimCounter = 0
	b.create(ctx, b.class())
	ginkgo.DeferCleanup(b.tearDown)
}

func (b *builder) tearDown(ctx context.Context) {
	// Before we allow the namespace and all objects in it do be deleted by
	// the framework, we must ensure that test pods and the claims that
	// they use are deleted. Otherwise the driver might get deleted first,
	// in which case deleting the claims won't work anymore.
	ginkgo.By("delete pods and claims")
	pods, err := b.listTestPods(ctx)
	framework.ExpectNoError(err, "list pods")
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		ginkgo.By(fmt.Sprintf("deleting %T %s", &pod, klog.KObj(&pod)))
		err := b.f.ClientSet.CoreV1().Pods(b.f.Namespace.Name).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if !apierrors.IsNotFound(err) {
			framework.ExpectNoError(err, "delete pod")
		}
	}
	gomega.Eventually(func() ([]v1.Pod, error) {
		return b.listTestPods(ctx)
	}).WithTimeout(time.Minute).Should(gomega.BeEmpty(), "remaining pods despite deletion")

	claims, err := b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).List(ctx, metav1.ListOptions{})
	framework.ExpectNoError(err, "get resource claims")
	for _, claim := range claims.Items {
		if claim.DeletionTimestamp != nil {
			continue
		}
		ginkgo.By(fmt.Sprintf("deleting %T %s", &claim, klog.KObj(&claim)))
		err := b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).Delete(ctx, claim.Name, metav1.DeleteOptions{})
		if !apierrors.IsNotFound(err) {
			framework.ExpectNoError(err, "delete claim")
		}
	}

	for host, plugin := range b.driver.Nodes {
		ginkgo.By(fmt.Sprintf("waiting for resources on %s to be unprepared", host))
		gomega.Eventually(plugin.GetPreparedResources).WithTimeout(time.Minute).Should(gomega.BeEmpty(), "prepared claims on host %s", host)
	}

	ginkgo.By("waiting for claims to be deallocated and deleted")
	gomega.Eventually(func() ([]resourceapi.ResourceClaim, error) {
		claims, err := b.f.ClientSet.ResourceV1beta1().ResourceClaims(b.f.Namespace.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return claims.Items, nil
	}).WithTimeout(time.Minute).Should(gomega.BeEmpty(), "claims in the namespaces")
}

func (b *builder) listTestPods(ctx context.Context) ([]v1.Pod, error) {
	pods, err := b.f.ClientSet.CoreV1().Pods(b.f.Namespace.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var testPods []v1.Pod
	for _, pod := range pods.Items {
		if pod.Labels["app.kubernetes.io/part-of"] == "dra-test-driver" {
			continue
		}
		testPods = append(testPods, pod)
	}
	return testPods, nil
}
