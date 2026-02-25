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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("DRA Quota E2E Test", func() {
	It("Case 1: Job with DRA request within quota should be scheduled", func() {
		// Switch to capacity plugin
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
			for _, tier := range sc.Tiers {
				for i, plugin := range tier.Plugins {
					if plugin.Name == "proportion" {
						tier.Plugins[i] = e2eutil.PluginOption{Name: "capacity"}
						return true
					}
				}
			}
			return false
		}
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, modifier)
		})
		defer cmc.UndoChanged()

		q1 := "dra-quota-q1"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
			DRAQuota: map[string]*schedulingv1beta1.DRAQuota{
				q1: {
					Capability: map[string]schedulingv1beta1.DRAResourceQuota{
						"gpu.example.com": {Count: 4},
					},
				},
			},
			NodesNumLimit:      2,
			NodesResourceLimit: e2eutil.CPU2Mem2,
		})
		defer e2eutil.CleanupTestContext(ctx)

		By("Create job requesting 2 GPUs via ResourceClaim")
		job := createDRAJob(ctx, "dra-job1", q1, "gpu.example.com", 2)

		By("Wait for job to be scheduled")
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Case 2: Job exceeding DRA quota should stay pending", func() {
		q1 := "dra-quota-q1"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
			DRAQuota: map[string]*schedulingv1beta1.DRAQuota{
				q1: {
					Capability: map[string]schedulingv1beta1.DRAResourceQuota{
						"gpu.example.com": {Count: 2},
					},
				},
			},
			NodesNumLimit:      2,
			NodesResourceLimit: e2eutil.CPU2Mem2,
		})
		defer e2eutil.CleanupTestContext(ctx)

		By("Create job1 using all DRA quota")
		job1 := createDRAJob(ctx, "dra-job1", q1, "gpu.example.com", 2)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		By("Create job2 that would exceed quota")
		job2 := createDRAJob(ctx, "dra-job2", q1, "gpu.example.com", 1)

		By("Verify job2 stays pending")
		err = e2eutil.WaitJobPending(ctx, job2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Case 5: DRA quota freed after job deletion allows new scheduling", func() {
		q1 := "dra-quota-q1"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
			DRAQuota: map[string]*schedulingv1beta1.DRAQuota{
				q1: {
					Capability: map[string]schedulingv1beta1.DRAResourceQuota{
						"gpu.example.com": {Count: 2},
					},
				},
			},
			NodesNumLimit:      2,
			NodesResourceLimit: e2eutil.CPU2Mem2,
		})
		defer e2eutil.CleanupTestContext(ctx)

		By("Create and run job1 using all quota")
		job1 := createDRAJob(ctx, "dra-job1", q1, "gpu.example.com", 2)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		By("Delete job1 to free quota")
		e2eutil.DeleteJob(ctx, job1)
		e2eutil.WaitJobCleanedUp(ctx, job1)

		By("Create job2 using freed quota")
		job2 := createDRAJob(ctx, "dra-job2", q1, "gpu.example.com", 2)
		err = e2eutil.WaitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())
	})
})

// createDRAJob creates a VolcanoJob with a ResourceClaim requesting
// the specified number of devices from the given DeviceClass
func createDRAJob(ctx *e2eutil.TestContext, name, queue, deviceClass string, count int64) *batchv1alpha1.Job {
	claimName := name + "-claim"

	// Create ResourceClaim
	claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: ctx.Namespace,
		},
		Spec: resourcev1.ResourceClaimSpec{
			// Note: In k8s 1.30+ allocation mode is embedded in devices
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{
					{
						Name: "dev-req",
						Exactly: &resourcev1.ExactDeviceRequest{
							DeviceClassName: deviceClass,
							Count:           count,
							AllocationMode:  resourcev1.DeviceAllocationModeExactCount,
						},
					},
				},
			},
		},
	}

	e2eutil.CreateResourceClaim(ctx, claim)

	// Create VolcanoJob with ResourceClaim reference
	claimRefName := claimName
	jobSpec := &e2eutil.JobSpec{
		Name:  name,
		Queue: queue,
		Tasks: []e2eutil.TaskSpec{
			{
				Img: e2eutil.DefaultNginxImage,
				Req: e2eutil.CPU1Mem1,
				Min: 1,
				Rep: 1,
				ResourceClaims: []v1.PodResourceClaim{
					{
						Name:              "dra-claim",
						ResourceClaimName: &claimRefName,
					},
				},
			},
		},
	}

	return e2eutil.CreateJob(ctx, jobSpec)
}
