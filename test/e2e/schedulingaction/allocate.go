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
		ginkgo.By("Switching from proportion plugin to capacity plugin")
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
			for _, tier := range sc.Tiers {
				for i, plugin := range tier.Plugins {
					if plugin.Name == "proportion" {
						tier.Plugins[i] = e2eutil.PluginOption{
							Name: "capacity",
						}
						return true
					}
				}
			}
			return false
		}
		_ = cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, modifier)
		})
		defer cmc.UndoChanged()

		ginkgo.By("Setting up test context with limited queue capacity")
		queueName := "capacity-test-queue"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{queueName},
			NodesNumLimit: 2,
			DeservedResource: map[string]corev1.ResourceList{
				queueName: {
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		// Helper: Create a pod with scheduling gate
		createPodWithGate := func(name string, nodeSelector map[string]string) *corev1.Pod {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ctx.Namespace,
					Annotations: map[string]string{
						"scheduling.k8s.io/group-name":     "test-podgroup",
						"volcano.sh/queue-allocation-gate": "true",
						"volcano.sh/queue-name":            queueName,
					},
				},
				Spec: corev1.PodSpec{
					SchedulerName: "volcano",
					RestartPolicy: corev1.RestartPolicyNever,
					NodeSelector:  nodeSelector,
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: e2eutil.DefaultNginxImage,
							Resources: corev1.ResourceRequirements{
								Requests: slot,
							},
						},
					},
				},
			}
			_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(
				context.TODO(), pod, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create pod "+name)
			return pod
		}

		ginkgo.By("Creating PodGroup for test pods")
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-podgroup",
				Namespace: ctx.Namespace,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				Queue:        queueName,
				MinResources: &slot,
			},
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(
			context.TODO(), pg, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Creating pod-1 and verifying it schedules successfully")
		pod1 := createPodWithGate("pod-1", nil)
		err = e2eutil.WaitPodPhase(ctx, pod1, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(e2eutil.HasOnlyVolcanoSchedulingGate(ctx, "pod-1")).To(gomega.BeFalse(),
			"pod-1 should not have Volcano gate (it was scheduled)")

		ginkgo.By("Deleting pod-1 to free queue capacity")
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(
			context.TODO(), "pod-1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Creating pod-2 with impossible node selector (will be unschedulable)")
		createPodWithGate("pod-2", map[string]string{"kubernetes.io/fake-node": "fake-node"})

		ginkgo.By("Verifying pod-2 passes capacity check (gate removed)")
		gomega.Eventually(func() bool {
			return !e2eutil.HasOnlyVolcanoSchedulingGate(ctx, "pod-2")
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue(),
			"pod-2 should not have Volcano gate (it passed capacity check)")

		ginkgo.By("Verifying pod-2 remains unschedulable (Pending phase)")
		pod2Ref := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: ctx.Namespace}}
		err = e2eutil.WaitPodPhase(ctx, pod2Ref, []corev1.PodPhase{corev1.PodPending})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"pod-2 should remain Pending (unschedulable due to fake node selector)")

		ginkgo.By("Creating pod-3 and verifying it is blocked by pod-2's capacity reservation")
		createPodWithGate("pod-3", nil)
		gomega.Eventually(func() bool {
			return e2eutil.HasOnlyVolcanoSchedulingGate(ctx, "pod-3")
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue(),
			"pod-3 should still have Volcano gate (blocked by pod-2's reservation)")
		gomega.Expect(e2eutil.HasSchedulingGatedCondition(ctx, "pod-3")).To(gomega.BeTrue(),
			"pod-3 should have SchedulingGated condition")

		ginkgo.By("Deleting pod-2 to release capacity reservation")
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(
			context.TODO(), "pod-2", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verifying pod-3 can now schedule and run")
		pod3Ref := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: ctx.Namespace}}
		err = e2eutil.WaitPodPhase(ctx, pod3Ref, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"pod-3 should be running after pod-2 is deleted")
	})
})
