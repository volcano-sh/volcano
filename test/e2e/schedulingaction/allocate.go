/*
Copyright 2021 The Volcano Authors.

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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Job E2E Test", func() {
	ginkgo.It("allocate work when resource is enough", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "low"
		lowReqJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "high"
		job.Tasks[0].Req = slot2
		highReqJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, highReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when resource is NOT enough", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 1,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot3 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("3000m"),
			corev1.ResourceMemory: resource.MustParse("3072Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot2,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "middle"
		midReqJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, midReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "low"
		job.Tasks[0].Req = slot1
		lowReqJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "high"
		job.Tasks[0].Req = slot3
		highReqJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobPending(ctx, highReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when podgroup is pending", func() {
		ginkgo.Skip("TODO: need to fix this test. It's useless to directly associate podgroup with job. The job will still create its own podgroup, so the resources are sufficient and j2 can be running.")
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &e2eutil.ThirtyCPU,
			},
		}

		_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "j1"
		existJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, existJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "j2"
		allocateJob := e2eutil.CreateJobWithPodGroup(ctx, job, pgName, map[string]string{})
		err = e2eutil.WaitJobPending(ctx, allocateJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when job NOT satisfy predicates", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 1,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: 1,
					Rep: 1,
					Labels: map[string]string{
						"job": "for-test-predicate",
					},
				},
			},
		}

		job.Name = "j1"
		existJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, existJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "j2"
		job.Tasks[0].Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "job",
									Operator: metav1.LabelSelectorOpIn,
									Values: []string{
										"for-test-predicate",
									},
								},
							},
						},
						TopologyKey: corev1.LabelHostname,
					},
				},
			},
		}
		allocateJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobPending(ctx, allocateJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when queue is overused", func() {
		q1 := "q1"
		q2 := "q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{q1, q2},
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})

		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot3 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot4 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "j1-q1"
		job.Queue = q1
		queue1Job1 := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobStateReady(ctx, queue1Job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "j2-q1"
		job.Queue = q1
		job.Tasks[0].Req = slot2
		queue1Job2 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobStateReady(ctx, queue1Job2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "j3-q2"
		job.Queue = q2
		job.Tasks[0].Req = slot3
		queue2Job3 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobStateReady(ctx, queue2Job3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "j4-q1"
		job.Queue = q1
		job.Tasks[0].Req = slot4
		queue1Job4 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobPending(ctx, queue1Job4)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Scheduling gates block vcjob. vcjob allocated after removed.", func() {
		// less than min available pods are allocated
		q1 := "q1"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{q1},
			NodesNumLimit: 4,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})

		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      slot1,
					Min:      2,
					Rep:      2,
					SchGates: []v1.PodSchedulingGate{{Name: "example.com/g1"}},
				},
			},
		}

		job.Name = "j1-q1"
		job.Queue = q1
		queue1Job1 := e2eutil.CreateJob(ctx, job)
		// job should be unschedulable
		err := e2eutil.WaitJobUnschedulable(ctx, queue1Job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		//Remove the scheduling gates
		err = e2eutil.RemovePodSchGates(ctx, queue1Job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = e2eutil.WaitJobStateReady(ctx, queue1Job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Scheduling gates block Deployment. Deployment allocated after removed.", func() {
		// less than min available pods are allocated
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 4,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2048Mi")}

		dep := e2eutil.CreateDeploymentGated(ctx, "dep-gated", 4, e2eutil.DefaultNginxImage, slot1, []v1.PodSchedulingGate{{Name: "g1"}})

		// need to wait for podgroup to be created
		var pg *schedulingv1beta1.PodGroup
		err := wait.Poll(time.Second, time.Minute, func() (bool, error) {
			pgList, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
			if len(pgList.Items) == 1 {
				pg = &pgList.Items[0]
			}
			return len(pgList.Items) == 1, err
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		e2eutil.WaitPodGroupPhase(ctx, pg, schedulingv1beta1.PodGroupInqueue)

		podList, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		patchData := []byte(`[
			{
				"op": "replace",
				"path": "/spec/schedulingGates",
				"value": []
			}
		]`)
		for _, pod := range podList.Items {
			// a naive way to tell if the pod belongs to the deployment
			if strings.HasPrefix(pod.Name, dep.Name) {
				//remove scheduling gates
				_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Patch(context.TODO(), pod.Name, types.JSONPatchType, patchData, metav1.PatchOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}
		// Deployment should be running after gates removed
		e2eutil.WaitPodGroupPhase(ctx, pg, schedulingv1beta1.PodGroupRunning)
	})

	// When a vcjob has subgroups (via partitionPolicy) but no network topology constraint,
	// the allocate action should still schedule it correctly.
	ginkgo.It("allocate work when job has subgroups (partitionPolicy) but no network topology constraint", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &e2eutil.JobSpec{
			Name: "job-subgroups-no-topology",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 2,
					Rep: 2,
					PartitionPolicy: &batchv1alpha1.PartitionPolicySpec{
						TotalPartitions: 2,
						PartitionSize:   1,
						MinPartitions:   2,
					},
				},
			},
		}

		createdJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, createdJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Unschedulable pod with removed gate reserves queue capacity and blocks other pods", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		// Create queue with capacity for 1 pod (500m CPU, 512Mi memory)
		queueName := "capacity-test-queue"
		e2eutil.CreateQueue(ctx, queueName, corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}, "")

		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		// Pod 1: Normal pod that will run and complete
		job1 := &e2eutil.JobSpec{
			Name:  "pod-1",
			Queue: queueName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img:     "busybox:1.24",
					Req:     slot,
					Min:     1,
					Rep:     1,
					Command: "sleep 5",
				},
			},
		}

		// Create pod-1 and wait for it to run
		vcJob1 := e2eutil.CreateJob(ctx, job1)
		err := e2eutil.WaitJobReady(ctx, vcJob1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod-1 should be scheduled")

		// Wait for pod-1 to complete
		err = e2eutil.WaitTasksCompleted(ctx, vcJob1, 1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod-1 should complete")

		// Pod 2: Has nodeSelector for non-existent node (will be Unschedulable)
		// This pod will pass queue capacity check but fail node predicates
		job2 := &e2eutil.JobSpec{
			Name:     "pod-2-unschedulable",
			Queue:    queueName,
			NodeName: "non-existent-node-for-testing", // This will make pod unschedulable
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 1,
					Rep: 1,
				},
			},
		}

		// Create pod-2 - it should pass capacity but become Unschedulable
		vcJob2 := e2eutil.CreateJob(ctx, job2)

		// Wait for pod-2 to be marked as unschedulable (triggers autoscaler)
		err = e2eutil.WaitJobUnschedulable(ctx, vcJob2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod-2 should be unschedulable")

		// Verify pod-2 no longer has Volcano scheduling gate (it passed capacity)
		tasks2 := e2eutil.GetTasksOfJob(ctx, vcJob2)
		gomega.Expect(len(tasks2)).To(gomega.Equal(1), "pod-2 should have exactly one pod")
		pod2 := tasks2[0]

		hasVolcanoGate := false
		for _, gate := range pod2.Spec.SchedulingGates {
			if gate.Name == "volcano.sh/queue-allocation-gate" {
				hasVolcanoGate = true
				break
			}
		}
		gomega.Expect(hasVolcanoGate).To(gomega.BeFalse(),
			"pod-2 should not have Volcano gate (it passed capacity check)")

		// Pod 3: Normal pod that should be blocked by pod-2's reservation
		job3 := &e2eutil.JobSpec{
			Name:  "pod-3-blocked",
			Queue: queueName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 1,
					Rep: 1,
				},
			},
		}

		// Create pod-3
		vcJob3 := e2eutil.CreateJob(ctx, job3)

		// Wait for some scheduling attempts
		time.Sleep(15 * time.Second)

		// Verify pod-3 still has Volcano scheduling gate (blocked by pod-2's reservation)
		tasks3 := e2eutil.GetTasksOfJob(ctx, vcJob3)
		gomega.Expect(len(tasks3)).To(gomega.Equal(1), "pod-3 should have exactly one pod")
		pod3 := tasks3[0]

		hasVolcanoGate = false
		for _, gate := range pod3.Spec.SchedulingGates {
			if gate.Name == "volcano.sh/queue-allocation-gate" {
				hasVolcanoGate = true
				break
			}
		}
		gomega.Expect(hasVolcanoGate).To(gomega.BeTrue(),
			"pod-3 should still have Volcano gate (blocked by pod-2's capacity reservation)")

		// Verify pod-3 is in Pending phase with SchedulingGated condition
		gomega.Expect(pod3.Status.Phase).To(gomega.Equal(corev1.PodPending),
			"pod-3 should be in Pending phase")

		// Check for SchedulingGated condition
		hasSchedulingGatedCondition := false
		for _, cond := range pod3.Status.Conditions {
			if cond.Type == corev1.PodScheduled &&
				cond.Status == corev1.ConditionFalse &&
				cond.Reason == "SchedulingGated" {
				hasSchedulingGatedCondition = true
				break
			}
		}
		gomega.Expect(hasSchedulingGatedCondition).To(gomega.BeTrue(),
			"pod-3 should have SchedulingGated condition")

		// Cleanup: Delete pod-2 to free the reservation
		e2eutil.DeleteJob(ctx, vcJob2)

		// Now pod-3 should be able to schedule
		err = e2eutil.WaitJobReady(ctx, vcJob3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"pod-3 should be scheduled after pod-2 is deleted")
	})
})
