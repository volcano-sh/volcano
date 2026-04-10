/*
Copyright 2024 The Volcano Authors.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	drautils "k8s.io/kubernetes/test/e2e/dra/utils"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	testDeviceClass              = "gpu.example.com"
	testQueueDRADeviceClassCount = "deviceclass/" + testDeviceClass
	testQueueDRADeviceClassCores = "cores.deviceclass/" + testDeviceClass
)

type draClaimSpec struct {
	deviceClass      string
	count            int64
	allocationMode   resourcev1.DeviceAllocationMode
	capacityRequests map[string]string
}

var _ = Describe("DRA Quota E2E Test", Serial, func() {
	f := k8sframework.NewDefaultFramework("dra-quota")
	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged
	nodes := drautils.NewNodes(f, 4, 4)
	driver := drautils.NewDriver(f, nodes, drautils.ToDriverResources(
		nil,
		buildQuotaDriverDevices()...,
	))

	var cmc *e2eutil.ConfigMapCase

	BeforeEach(func() {
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		err := cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
				changed := false
				for i := range sc.Tiers {
					for j := range sc.Tiers[i].Plugins {
						if sc.Tiers[i].Plugins[j].Name == "proportion" {
							sc.Tiers[i].Plugins[j].Name = "capacity"
							changed = true
						}
						if sc.Tiers[i].Plugins[j].Name == "predicates" {
							if sc.Tiers[i].Plugins[j].Arguments == nil {
								sc.Tiers[i].Plugins[j].Arguments = map[string]string{}
							}
							if sc.Tiers[i].Plugins[j].Arguments["predicate.DynamicResourceAllocationEnable"] != "true" {
								sc.Tiers[i].Plugins[j].Arguments["predicate.DynamicResourceAllocationEnable"] = "true"
								changed = true
							}
						}
						if sc.Tiers[i].Plugins[j].Name == "capacity" {
							if sc.Tiers[i].Plugins[j].Arguments == nil {
								sc.Tiers[i].Plugins[j].Arguments = map[string]string{}
							}
							if sc.Tiers[i].Plugins[j].Arguments["capacity.DynamicResourceAllocationEnable"] != "true" {
								sc.Tiers[i].Plugins[j].Arguments["capacity.DynamicResourceAllocationEnable"] = "true"
								changed = true
							}
							if sc.Tiers[i].Plugins[j].Arguments["capacity.DRAConsumableCapacityEnable"] != "true" {
								sc.Tiers[i].Plugins[j].Arguments["capacity.DRAConsumableCapacityEnable"] = "true"
								changed = true
							}
						}
					}
				}
				return changed
			}
			return e2eutil.ModifySchedulerConfig(data, modifier)
		})
		Expect(err).NotTo(HaveOccurred())
		ensureQuotaDeviceClass(driver.Name)
	})

	AfterEach(func() {
		if cmc != nil {
			Expect(cmc.UndoChanged()).NotTo(HaveOccurred())
		}
	})

	It("tracks direct ResourceClaim quota and queue allocated status", func() {
		queueName := "dra-direct-quota"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("4"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		job := createDirectClaimDRAJob(ctx, "dra-direct-job", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       2,
		}, 1)

		Expect(e2eutil.WaitJobReady(ctx, job)).NotTo(HaveOccurred())
		waitQueueAllocatedResources(ctx, queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("2"),
		})
	})

	It("tracks ResourceClaimTemplate-derived claims in quota", func() {
		queueName := "dra-template-quota"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("4"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		job := createTemplateClaimDRAJob(ctx, "dra-template-job", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       2,
		}, 1)

		Expect(e2eutil.WaitJobReady(ctx, job)).NotTo(HaveOccurred())
		waitQueueAllocatedResources(ctx, queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("2"),
		})
	})

	It("charges a shared ResourceClaim only once across pods", func() {
		queueName := "dra-shared-quota"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("1"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		claimName := "dra-shared-claim"
		createResourceClaim(ctx, claimName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
		})

		job := createDRAJobWithPodClaim(ctx, "dra-shared-job", queueName, v1.PodResourceClaim{
			Name:              "dra-claim",
			ResourceClaimName: &claimName,
		}, 2, 1)

		Expect(e2eutil.WaitTasksReady(ctx, job, 2)).NotTo(HaveOccurred())
		waitQueueAllocatedResources(ctx, queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("1"),
		})
	})

	It("keeps jobs pending when direct-claim count quota is exceeded", func() {
		queueName := "dra-count-pending"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("2"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		job1 := createDirectClaimDRAJob(ctx, "dra-count-job-1", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       2,
		}, 1)
		Expect(e2eutil.WaitJobReady(ctx, job1)).NotTo(HaveOccurred())

		job2 := createDirectClaimDRAJob(ctx, "dra-count-job-2", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
		}, 1)
		Expect(e2eutil.WaitJobPending(ctx, job2)).NotTo(HaveOccurred())
	})

	It("enforces capacity-dimension quota and exposes allocated capacity", func() {
		queueName := "dra-capacity-quota"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("2"),
			v1.ResourceName(testQueueDRADeviceClassCores): resource.MustParse("400"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		job1 := createDirectClaimDRAJob(ctx, "dra-capacity-job-1", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
			capacityRequests: map[string]string{
				"cores": "300",
			},
		}, 1)
		Expect(e2eutil.WaitJobReady(ctx, job1)).NotTo(HaveOccurred())
		waitQueueAllocatedResources(ctx, queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("1"),
			v1.ResourceName(testQueueDRADeviceClassCores): resource.MustParse("300"),
		})

		job2 := createDirectClaimDRAJob(ctx, "dra-capacity-job-2", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
			capacityRequests: map[string]string{
				"cores": "200",
			},
		}, 1)
		Expect(e2eutil.WaitJobPending(ctx, job2)).NotTo(HaveOccurred())
	})

	It("re-schedules after capacity quota is released", func() {
		queueName := "dra-capacity-recover"
		ctx := newDRAQuotaContext(queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("2"),
			v1.ResourceName(testQueueDRADeviceClassCores): resource.MustParse("400"),
		})
		defer e2eutil.CleanupTestContext(ctx)

		job1 := createDirectClaimDRAJob(ctx, "dra-capacity-recover-job-1", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
			capacityRequests: map[string]string{
				"cores": "400",
			},
		}, 1)
		Expect(e2eutil.WaitJobReady(ctx, job1)).NotTo(HaveOccurred())

		job2 := createDirectClaimDRAJob(ctx, "dra-capacity-recover-job-2", queueName, draClaimSpec{
			deviceClass: testDeviceClass,
			count:       1,
			capacityRequests: map[string]string{
				"cores": "300",
			},
		}, 1)
		Expect(e2eutil.WaitJobPending(ctx, job2)).NotTo(HaveOccurred())

		e2eutil.DeleteJob(ctx, job1)
		Expect(e2eutil.WaitJobCleanedUp(ctx, job1)).NotTo(HaveOccurred())
		Expect(e2eutil.WaitJobReady(ctx, job2)).NotTo(HaveOccurred())
		waitQueueAllocatedResources(ctx, queueName, v1.ResourceList{
			v1.ResourceName(testQueueDRADeviceClassCount): resource.MustParse("1"),
			v1.ResourceName(testQueueDRADeviceClassCores): resource.MustParse("300"),
		})
	})
})

func newDRAQuotaContext(queueName string, capability v1.ResourceList) *e2eutil.TestContext {
	return e2eutil.InitTestContext(e2eutil.Options{
		Queues: []string{queueName},
		CapabilityResource: map[string]v1.ResourceList{
			queueName: capability,
		},
		NodesNumLimit:      2,
		NodesResourceLimit: e2eutil.CPU2Mem2,
	})
}

func createDirectClaimDRAJob(
	ctx *e2eutil.TestContext,
	name, queue string,
	claimSpec draClaimSpec,
	replicas int32,
) *batchv1alpha1.Job {
	claimName := name + "-claim"
	createResourceClaim(ctx, claimName, claimSpec)
	return createDRAJobWithPodClaim(ctx, name, queue, v1.PodResourceClaim{
		Name:              "dra-claim",
		ResourceClaimName: &claimName,
	}, replicas, replicas)
}

func createTemplateClaimDRAJob(
	ctx *e2eutil.TestContext,
	name, queue string,
	claimSpec draClaimSpec,
	replicas int32,
) *batchv1alpha1.Job {
	templateName := name + "-claim-template"
	createResourceClaimTemplate(ctx, templateName, claimSpec)
	return createDRAJobWithPodClaim(ctx, name, queue, v1.PodResourceClaim{
		Name:                      "dra-claim",
		ResourceClaimTemplateName: &templateName,
	}, replicas, replicas)
}

func createDRAJobWithPodClaim(
	ctx *e2eutil.TestContext,
	name, queue string,
	podClaim v1.PodResourceClaim,
	replicas int32,
	minAvailable int32,
) *batchv1alpha1.Job {
	if replicas <= 0 {
		replicas = 1
	}
	if minAvailable <= 0 {
		minAvailable = replicas
	}

	return e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
		Name:  name,
		Queue: queue,
		Min:   minAvailable,
		Tasks: []e2eutil.TaskSpec{
			{
				Img: e2eutil.DefaultNginxImage,
				Req: e2eutil.CPU1Mem1,
				Min: replicas,
				Rep: replicas,
				ResourceClaims: []v1.PodResourceClaim{
					podClaim,
				},
			},
		},
	})
}

func createResourceClaim(ctx *e2eutil.TestContext, name string, spec draClaimSpec) *resourcev1.ResourceClaim {
	claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ctx.Namespace,
		},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: buildDeviceClaim(spec),
		},
	}
	return e2eutil.CreateResourceClaim(ctx, claim)
}

func createResourceClaimTemplate(ctx *e2eutil.TestContext, name string, spec draClaimSpec) *resourcev1.ResourceClaimTemplate {
	template := &resourcev1.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ctx.Namespace,
		},
		Spec: resourcev1.ResourceClaimTemplateSpec{
			Spec: resourcev1.ResourceClaimSpec{
				Devices: buildDeviceClaim(spec),
			},
		},
	}
	return e2eutil.CreateResourceClaimTemplate(ctx, template)
}

func buildDeviceClaim(spec draClaimSpec) resourcev1.DeviceClaim {
	return resourcev1.DeviceClaim{
		Requests: []resourcev1.DeviceRequest{
			buildDeviceRequest(spec),
		},
	}
}

func buildDeviceRequest(spec draClaimSpec) resourcev1.DeviceRequest {
	exactly := &resourcev1.ExactDeviceRequest{
		DeviceClassName: spec.deviceClass,
		Count:           spec.count,
		AllocationMode:  spec.allocationMode,
	}
	if exactly.AllocationMode == "" {
		exactly.AllocationMode = resourcev1.DeviceAllocationModeExactCount
	}
	if capacity := buildCapacityRequirements(spec.capacityRequests); capacity != nil {
		exactly.Capacity = capacity
	}

	return resourcev1.DeviceRequest{
		Name:    "dev-req",
		Exactly: exactly,
	}
}

func buildCapacityRequirements(requests map[string]string) *resourcev1.CapacityRequirements {
	if len(requests) == 0 {
		return nil
	}

	capacityRequests := make(map[resourcev1.QualifiedName]resource.Quantity, len(requests))
	for dim, quantity := range requests {
		capacityRequests[resourcev1.QualifiedName(dim)] = resource.MustParse(quantity)
	}
	return &resourcev1.CapacityRequirements{Requests: capacityRequests}
}

func waitQueueAllocatedResources(ctx *e2eutil.TestContext, queueName string, expected v1.ResourceList) {
	err := e2eutil.WaitQueueStatus(func() (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return queueAllocatedContains(queue.Status.Allocated, expected), nil
	})
	Expect(err).NotTo(HaveOccurred(), "queue %s did not expose expected DRA allocated resources", queueName)
}

func queueAllocatedContains(actual, expected v1.ResourceList) bool {
	for name, expectedQty := range expected {
		actualQty, found := actual[name]
		if !found {
			return false
		}
		if actualQty.Cmp(expectedQty) != 0 {
			return false
		}
	}
	return true
}

func ensureQuotaDeviceClass(driverName string) {
	class := &resourcev1.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDeviceClass,
		},
		Spec: resourcev1.DeviceClassSpec{
			Selectors: []resourcev1.DeviceSelector{{
				CEL: &resourcev1.CELDeviceSelector{
					Expression: fmt.Sprintf(`device.driver == %q`, driverName),
				},
			}},
		},
	}

	_, err := e2eutil.KubeClient.ResourceV1().DeviceClasses().Create(context.TODO(), class, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "failed to create DeviceClass %s", testDeviceClass)
	}

	DeferCleanup(func() {
		err := e2eutil.KubeClient.ResourceV1().DeviceClasses().Delete(context.TODO(), testDeviceClass, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to delete DeviceClass %s", testDeviceClass)
		}
	})
}

func buildQuotaDriverDevices() []resourcev1.Device {
	devices := make([]resourcev1.Device, 0, 4)
	for i := 0; i < 4; i++ {
		devices = append(devices, resourcev1.Device{
			Name: fmt.Sprintf("quota-device-%02d", i),
			Capacity: map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{
				"cores": {
					Value: resource.MustParse("400"),
				},
			},
		})
	}
	return devices
}
