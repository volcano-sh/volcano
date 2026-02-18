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
		const queueName = "capacity-test-queue"
		const pgName = "test-podgroup"

		// Switch to capacity plugin
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		_ = cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				for _, tier := range sc.Tiers {
					for i, plugin := range tier.Plugins {
						if plugin.Name == "proportion" {
							tier.Plugins[i] = e2eutil.PluginOption{Name: "capacity"}
							return true
						}
					}
				}
				return false
			})
		})
		defer cmc.UndoChanged()

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
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
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(),
			&schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: queueName},
				Spec: schedulingv1beta1.QueueSpec{
					Weight:     1,
					Capability: slot,
				},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer e2eutil.DeleteQueue(ctx, queueName)

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
			&schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: pgName, Namespace: ctx.Namespace},
				Spec: schedulingv1beta1.PodGroupSpec{
					MinMember:    1,
					Queue:        queueName,
					MinResources: &slot,
				},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		createPod := func(name string, nodeSelector map[string]string) {
			_ = e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:   name,
				Req:    slot,
				Labels: map[string]string{"test": "capacity-reservation"},
				Annotations: map[string]string{
					"scheduling.k8s.io/group-name":           pgName,
					schedulingv1beta1.QueueAllocationGateKey: "true",
					schedulingv1beta1.QueueNameAnnotationKey: queueName,
				},
				SchedulerName: "volcano",
				RestartPolicy: corev1.RestartPolicyNever,
				NodeSelector:  nodeSelector,
			})
		}

		ginkgo.By("Pod-1 schedules and runs")
		createPod("pod-1", nil)
		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-1", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())

		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-1", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod-2 unschedulable, reserves capacity and stays pending")
		createPod("pod-2", map[string]string{"kubernetes.io/fake-node": "fake"})
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-2", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())
		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
			[]corev1.PodPhase{corev1.PodPending})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod-2 deleted, Pod-3 schedules and runs")
		createPod("pod-3", nil)
		err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), "pod-2", metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), "pod-3", metav1.GetOptions{})
			return err == nil && !e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())
		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Pod with multiple scheduling gates: Volcano removes only its gate, other gates remain", func() {
		const (
			queueName     = "capacity-multigate-queue"
			pgName        = "test-pg-multigate"
			otherGateName = "example.com/other-gate"
			podName       = "pod-multigate"
		)

		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		_ = cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				for _, tier := range sc.Tiers {
					for i, plugin := range tier.Plugins {
						if plugin.Name == "proportion" {
							tier.Plugins[i] = e2eutil.PluginOption{Name: "capacity"}
							return true
						}
					}
				}
				return false
			})
		})
		defer cmc.UndoChanged()

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
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
		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(),
			&schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: queueName},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Capability: slot},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer e2eutil.DeleteQueue(ctx, queueName)

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			q, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return q.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(),
			&schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: pgName, Namespace: ctx.Namespace},
				Spec:       schedulingv1beta1.PodGroupSpec{MinMember: 1, Queue: queueName, MinResources: &slot},
			}, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod with two gates, both gates remain")
		_ = e2eutil.CreatePod(ctx, e2eutil.PodSpec{
			Name: podName,
			Req:  slot,
			Annotations: map[string]string{
				"scheduling.k8s.io/group-name":           pgName,
				schedulingv1beta1.QueueAllocationGateKey: "true",
				schedulingv1beta1.QueueNameAnnotationKey: queueName,
			},
			SchedulerName:   "volcano",
			SchedulingGates: []corev1.PodSchedulingGate{{Name: otherGateName}},
		})

		gomega.Consistently(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil || pod.Status.Phase != corev1.PodPending {
				return false
			}
			return e2eutil.PodHasSchedulingGates(pod, schedulingv1beta1.QueueAllocationGateKey, otherGateName)
		}, 10*time.Second, 1*time.Second).Should(gomega.BeTrue(), "both gates remain and pod stays Pending")

		ginkgo.By("Secondary gate removed, only volcano gate remains")
		patchOnlyVolcano := []byte(`[{"op":"replace","path":"/spec/schedulingGates","value":[{"name":"` + schedulingv1beta1.QueueAllocationGateKey + `"}]}]`)
		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Patch(context.TODO(), podName, types.JSONPatchType, patchOnlyVolcano, metav1.PatchOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Pod schedules and runs")

		gomega.Eventually(func() bool {
			pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			return err == nil && e2eutil.PodHasSchedulingGates(pod)
		}, e2eutil.FiveMinute, 500*time.Millisecond).Should(gomega.BeTrue())

		err = e2eutil.WaitPodPhase(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ctx.Namespace}},
			[]corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
