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

package unschedulablejobcache

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	fwk "k8s.io/kube-scheduler/framework"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	skipMetricName               = "volcano_unschedulable_job_cache_skips_total"
	wakeupMetricName             = "volcano_unschedulable_job_cache_wakeups_total"
	watchdogExpirationMetricName = "volcano_unschedulable_job_cache_watchdog_expirations_total"
	eventWakeupTimeout           = time.Minute
	watchdogExpirationTimeout    = 30 * time.Second
)

var _ = Describe("Unschedulable Job Cache", func() {
	Describe("Quota plugin hints", func() {
		Describe("proportion", func() {
			It("wakes a Job when its Queue capability increases", func() {
				runQueueCapabilityWakeupCase("proportion", "proportion-hint-queue", "proportion-queue-wakeup")
			})

			It("wakes a rejected task when its Queue capability increases", func() {
				runTaskQuotaWakeupCase("proportion", "proportion-task-hint-queue", "proportion-task-queue-wakeup")
			})

			It("wakes a rejected task when a quota-consuming Pod is deleted", func() {
				runTaskPodReleaseWakeupCase("proportion", "proportion-pod-hint-queue", "proportion-pod-wakeup")
			})
		})

		Describe("capacity", func() {
			It("wakes a Job when its Queue capability increases", func() {
				runQueueCapabilityWakeupCase("capacity", "capacity-hint-queue", "capacity-queue-wakeup")
			})

			It("wakes a rejected task when its Queue capability increases", func() {
				runTaskQuotaWakeupCase("capacity", "capacity-task-hint-queue", "capacity-task-queue-wakeup")
			})

			It("wakes a rejected task when a quota-consuming Pod is deleted", func() {
				runTaskPodReleaseWakeupCase("capacity", "capacity-pod-hint-queue", "capacity-pod-wakeup")
			})
		})
	})

	Describe("Generic event hints and cache behavior", func() {
		It("wakes a multi-replica Job when one rejected task is helped by a Node update", func() {
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				NodesNumLimit: 1,
				NodesResourceLimit: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("128Mi"),
				},
			})
			defer e2eutil.CleanupTestContext(ctx)

			labelKey := "volcano.sh/unschedulable-cache-e2e"
			labelValue := ctx.Namespace
			targetNode, availableSlots := e2eutil.ComputeNode(ctx, smallRequest())
			Expect(targetNode).NotTo(BeEmpty())
			Expect(availableSlots).To(BeNumerically(">=", 2))
			skipBaseline := schedulerMetricValue(ctx, skipMetricName, map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "node-label-wakeup",
				"stage":         "allocate",
			})
			wakeupBaseline := schedulerMetricValue(ctx, wakeupMetricName, map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "node-label-wakeup",
			})
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "node-label-wakeup",
				Namespace: ctx.Namespace,
				Min:       2,
				Tasks: []e2eutil.TaskSpec{{
					Name: "worker",
					Img:  e2eutil.DefaultBusyBoxImage,
					Rep:  2,
					Min:  2,
					Req:  smallRequest(),
					Affinity: &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
								Key: labelKey, Operator: v1.NodeSelectorOpIn, Values: []string{labelValue},
							}}}},
						},
					}},
					Command: "sleep 300",
				}},
			})
			By("waiting for the Job to become unschedulable")
			Expect(e2eutil.WaitJobStatePending(ctx, job)).To(Succeed())
			Expect(e2eutil.WaitJobUnschedulable(ctx, job)).To(Succeed())
			waitForMetricIncrease(ctx, skipMetricName, map[string]string{
				"job_namespace": job.Namespace,
				"job_name":      job.Name,
				"stage":         "allocate",
			}, skipBaseline)
			expectJobUnbound(ctx, job)

			By("adding the required label to a worker Node")
			node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), targetNode, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			oldLabelValue, hadLabel := node.Labels[labelKey]
			patch := fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, labelKey, labelValue)
			_, err = ctx.Kubeclient.CoreV1().Nodes().Patch(context.TODO(), targetNode,
				types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				var restorePatch string
				if hadLabel {
					restorePatch = fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, labelKey, oldLabelValue)
				} else {
					restorePatch = fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, labelKey)
				}
				_, cleanupErr := ctx.Kubeclient.CoreV1().Nodes().Patch(context.TODO(), targetNode,
					types.StrategicMergePatchType, []byte(restorePatch), metav1.PatchOptions{})
				Expect(cleanupErr).NotTo(HaveOccurred())
			})

			By("verifying the Node event wakes and schedules the Job")
			waitForMetricIncrease(ctx, wakeupMetricName, map[string]string{
				"job_namespace": job.Namespace,
				"job_name":      job.Name,
				"resource":      string(fwk.Node),
			}, wakeupBaseline)
			waitForJobReady(ctx, job)
			tasks := e2eutil.GetTasksOfJob(ctx, job)
			Expect(tasks).To(HaveLen(2))
			for _, task := range tasks {
				Expect(task.Spec.NodeName).To(Equal(targetNode))
			}
		})

		It("skips a resource-blocked Job until a scheduled Pod is deleted", func() {
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				NodesNumLimit: 1,
				NodesResourceLimit: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("256Mi"),
				},
			})
			defer e2eutil.CleanupTestContext(ctx)

			By("occupying the only schedulable CPU slot")
			blocker := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
				Name:          "resource-blocker",
				Req:           v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				RestartPolicy: v1.RestartPolicyNever,
			})
			Expect(e2eutil.WaitPodReady(ctx, blocker)).To(Succeed())
			skipBaseline := schedulerMetricValue(ctx, skipMetricName, map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "pod-delete-wakeup",
				"stage":         "allocate",
			})
			wakeupBaseline := schedulerMetricValue(ctx, wakeupMetricName, map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "pod-delete-wakeup",
			})

			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "pod-delete-wakeup",
				Namespace: ctx.Namespace,
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name:    "worker",
					Img:     e2eutil.DefaultBusyBoxImage,
					Rep:     1,
					Min:     1,
					Req:     v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					Command: "sleep 300",
				}},
			})
			By("waiting for the Job to be cached and skipped")
			Expect(e2eutil.WaitJobStatePending(ctx, job)).To(Succeed())
			Expect(e2eutil.WaitJobUnschedulable(ctx, job)).To(Succeed())
			waitForMetricIncrease(ctx, skipMetricName, map[string]string{
				"job_namespace": job.Namespace,
				"job_name":      job.Name,
				"stage":         "allocate",
			}, skipBaseline)
			expectJobUnbound(ctx, job)

			By("deleting the resource-consuming Pod")
			e2eutil.DeletePod(ctx, blocker)

			By("verifying the Pod delete event wakes and schedules the Job")
			waitForMetricIncrease(ctx, wakeupMetricName, map[string]string{
				"job_namespace": job.Namespace,
				"job_name":      job.Name,
				"resource":      string(fwk.Pod),
			}, wakeupBaseline)
			waitForJobReady(ctx, job)
		})

		It("does not affect Jobs that can be scheduled normally", func() {
			ctx := e2eutil.InitTestContext(e2eutil.Options{})
			defer e2eutil.CleanupTestContext(ctx)
			skipBaseline := schedulerMetricValue(ctx, skipMetricName, map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "cached-blocked-job",
				"stage":         "allocate",
			})

			blockedJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "cached-blocked-job",
				Namespace: ctx.Namespace,
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name: "worker",
					Img:  e2eutil.DefaultBusyBoxImage,
					Rep:  1,
					Min:  1,
					Req:  smallRequest(),
					Affinity: &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
								Key: "volcano.sh/nonexistent-e2e-label", Operator: v1.NodeSelectorOpExists,
							}}}},
						},
					}},
					Command: "sleep 300",
				}},
			})
			Expect(e2eutil.WaitJobStatePending(ctx, blockedJob)).To(Succeed())
			Expect(e2eutil.WaitJobUnschedulable(ctx, blockedJob)).To(Succeed())
			waitForMetricIncrease(ctx, skipMetricName, map[string]string{
				"job_namespace": blockedJob.Namespace,
				"job_name":      blockedJob.Name,
				"stage":         "allocate",
			}, skipBaseline)
			expectJobUnbound(ctx, blockedJob)

			By("creating another Job that is immediately schedulable")
			normalJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "normal-job",
				Namespace: ctx.Namespace,
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name:    "worker",
					Img:     e2eutil.DefaultBusyBoxImage,
					Rep:     1,
					Min:     1,
					Req:     smallRequest(),
					Command: "sleep 300",
				}},
			})

			waitForJobReady(ctx, normalJob)
			expectJobUnbound(ctx, blockedJob)
		})

		It("does not suppress preemption for a cached high-priority Job", func() {
			configureScheduler("proportion", "allocate")
			ctx := e2eutil.InitTestContext(e2eutil.Options{
				PriorityClasses: map[string]int32{
					"cache-high-priority": 100,
					"cache-low-priority":  10,
				},
				NodesNumLimit: 1,
				NodesResourceLimit: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1"),
					v1.ResourceMemory: resource.MustParse("256Mi"),
				},
			})
			defer e2eutil.CleanupTestContext(ctx)

			By("filling the only CPU slot with a preemptable low-priority Job")
			lowJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "cache-preemptee",
				Namespace: ctx.Namespace,
				Pri:       "cache-low-priority",
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name:    "worker",
					Img:     e2eutil.DefaultBusyBoxImage,
					Rep:     1,
					Min:     1,
					Req:     v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					Labels:  map[string]string{schedulingv1beta1.PodPreemptable: "true"},
					Command: "sleep 300",
				}},
			})
			waitForJobReady(ctx, lowJob)

			skipLabels := map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "cache-preemptor",
				"stage":         "allocate",
			}
			skipBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
			highJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "cache-preemptor",
				Namespace: ctx.Namespace,
				Pri:       "cache-high-priority",
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name:    "worker",
					Img:     e2eutil.DefaultBusyBoxImage,
					Rep:     1,
					Min:     1,
					Req:     v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					Command: "sleep 300",
				}},
			})
			Expect(e2eutil.WaitJobUnschedulable(ctx, highJob)).To(Succeed())
			waitForMetricIncrease(ctx, skipMetricName, skipLabels, skipBaseline)
			expectJobUnbound(ctx, highJob)

			By("enabling preemption while the high-priority Job remains cached")
			configureScheduler("proportion", "allocate, preempt")
			waitForJobReady(ctx, highJob)
		})

		It("retries a cached Job after the watchdog duration expires", func() {
			setSchedulerMaxSkipDuration("5s")

			ctx := e2eutil.InitTestContext(e2eutil.Options{})
			defer e2eutil.CleanupTestContext(ctx)

			expirationLabels := map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "watchdog-retry",
			}
			skipLabels := map[string]string{
				"job_namespace": ctx.Namespace,
				"job_name":      "watchdog-retry",
				"stage":         "allocate",
			}
			expirationBaseline := schedulerMetricValue(ctx, watchdogExpirationMetricName, expirationLabels)
			skipBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
			job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
				Name:      "watchdog-retry",
				Namespace: ctx.Namespace,
				Min:       1,
				Tasks: []e2eutil.TaskSpec{{
					Name: "worker",
					Img:  e2eutil.DefaultBusyBoxImage,
					Rep:  1,
					Min:  1,
					Req:  smallRequest(),
					Affinity: &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
								Key: "volcano.sh/watchdog-never-matches", Operator: v1.NodeSelectorOpExists,
							}}}},
						},
					}},
					Command: "sleep 300",
				}},
			})

			By("waiting for the Job to be cached without triggering a matching event")
			Expect(e2eutil.WaitJobUnschedulable(ctx, job)).To(Succeed())
			waitForMetricIncrease(ctx, skipMetricName, skipLabels, skipBaseline)

			By("verifying the shortened watchdog expires the record")
			Expect(e2eutil.WaitSchedulerCounterIncrease(context.TODO(), ctx.Kubeclient,
				watchdogExpirationMetricName, expirationLabels, expirationBaseline, watchdogExpirationTimeout)).To(Succeed())

			By("verifying the still-blocked Job is evaluated and cached again")
			retryBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
			waitForMetricIncrease(ctx, skipMetricName, skipLabels, retryBaseline)
			expectJobUnbound(ctx, job)
		})
	})
})

// setSchedulerMaxSkipDuration rolls the scheduler with a test-only watchdog
// duration and restores its original arguments after the spec.
func setSchedulerMaxSkipDuration(duration string) {
	const namespace = "volcano-system"
	deployments, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).List(
		context.TODO(), metav1.ListOptions{LabelSelector: "app=volcano-scheduler"})
	Expect(err).NotTo(HaveOccurred())
	Expect(deployments.Items).To(HaveLen(1))

	name := deployments.Items[0].Name
	originalArgs := append([]string(nil), deployments.Items[0].Spec.Template.Spec.Containers[0].Args...)
	generation := updateSchedulerArgs(namespace, name, withMaxSkipDuration(originalArgs, duration))
	waitForSchedulerRollout(namespace, name, generation)

	DeferCleanup(func() {
		generation := updateSchedulerArgs(namespace, name, originalArgs)
		waitForSchedulerRollout(namespace, name, generation)
	})
}

func withMaxSkipDuration(args []string, duration string) []string {
	const prefix = "--unschedulable-job-cache-max-skip-duration="
	updated := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if len(arg) >= len(prefix) && arg[:len(prefix)] == prefix {
			continue
		}
		updated = append(updated, arg)
	}
	return append(updated, prefix+duration)
}

func updateSchedulerArgs(namespace, name string, args []string) int64 {
	var generation int64
	Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		deployment, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
			context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		deployment.Spec.Template.Spec.Containers[0].Args = append([]string(nil), args...)
		updated, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Update(
			context.TODO(), deployment, metav1.UpdateOptions{})
		if err == nil {
			generation = updated.Generation
		}
		return err
	})).To(Succeed())
	return generation
}

func waitForSchedulerRollout(namespace, name string, generation int64) {
	Eventually(func() bool {
		deployment, err := e2eutil.KubeClient.AppsV1().Deployments(namespace).Get(
			context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		return deployment.Status.ObservedGeneration >= generation &&
			deployment.Status.UpdatedReplicas == desired &&
			deployment.Status.AvailableReplicas == desired
	}, eventWakeupTimeout, 500*time.Millisecond).Should(BeTrue())
}

// runQueueCapabilityWakeupCase verifies a quota plugin's complete Queue-hint
// path: enqueue rejection, cache skip, Queue Update wakeup, and normal retry.
func runQueueCapabilityWakeupCase(pluginName, queueName, jobName string) {
	configureQuotaPlugin(pluginName)

	// The initial 100m capability is deliberately below the Job's 200m gang
	// request, while the Node has enough capacity once the Queue limit is raised.
	ctx := e2eutil.InitTestContext(e2eutil.Options{
		Queues: []string{queueName},
		CapabilityResource: map[string]v1.ResourceList{
			queueName: {v1.ResourceCPU: resource.MustParse("100m")},
		},
		NodesNumLimit: 1,
		NodesResourceLimit: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
	})
	defer e2eutil.CleanupTestContext(ctx)

	skipLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"stage":         "enqueue",
	}
	wakeupLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"resource":      string(api.QueueEvent),
	}
	skipBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
	wakeupBaseline := schedulerMetricValue(ctx, wakeupMetricName, wakeupLabels)

	job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
		Name:      jobName,
		Namespace: ctx.Namespace,
		Queue:     queueName,
		Min:       1,
		Tasks: []e2eutil.TaskSpec{{
			Name: "worker",
			Img:  e2eutil.DefaultBusyBoxImage,
			Rep:  1,
			Min:  1,
			Req: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("200m"),
				v1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Command: "sleep 300",
		}},
	})

	By(fmt.Sprintf("waiting for %s to reject and cache the Job", pluginName))
	Expect(e2eutil.WaitJobStatePending(ctx, job)).To(Succeed())
	Expect(e2eutil.WaitJobUnschedulable(ctx, job)).To(Succeed())
	waitForMetricIncrease(ctx, skipMetricName, skipLabels, skipBaseline)
	expectJobUnbound(ctx, job)

	By("raising the Queue capability above the Job's minimum request")
	updateQueueCPUCapability(ctx, queueName, "1")

	By(fmt.Sprintf("verifying %s's Queue hint wakes and schedules the Job", pluginName))
	waitForMetricIncrease(ctx, wakeupMetricName, wakeupLabels, wakeupBaseline)
	waitForJobReady(ctx, job)
}

// runTaskQuotaWakeupCase isolates the task-level Allocatable extension point.
// The Job is admitted while only enqueue runs; quota is then tightened before
// allocate starts, so the concrete Task—not the Job—is rejected and cached.
func runTaskQuotaWakeupCase(pluginName, queueName, jobName string) {
	configureScheduler(pluginName, "enqueue")

	ctx := e2eutil.InitTestContext(e2eutil.Options{
		Queues: []string{queueName},
		CapabilityResource: map[string]v1.ResourceList{
			queueName: {v1.ResourceCPU: resource.MustParse("1")},
		},
		NodesNumLimit: 1,
		NodesResourceLimit: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
	})
	defer e2eutil.CleanupTestContext(ctx)

	job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
		Name:      jobName,
		Namespace: ctx.Namespace,
		Queue:     queueName,
		Min:       1,
		Tasks: []e2eutil.TaskSpec{{
			Name: "worker",
			Img:  e2eutil.DefaultBusyBoxImage,
			Rep:  1,
			Min:  1,
			Req: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("600m"),
				v1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Command: "sleep 300",
		}},
	})

	By("waiting for the Job to pass Queue admission without allocating tasks")
	waitForJobPodGroupPhase(ctx, job, schedulingv1beta1.PodGroupInqueue)
	expectJobUnbound(ctx, job)

	// The Job was admitted against 1 CPU. Lowering the limit to 500m after
	// admission makes its 600m Task fail the quota plugin's Allocatable check.
	By("lowering Queue capability below the admitted Task request")
	updateQueueCPUCapability(ctx, queueName, "500m")

	skipLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"stage":         "allocate",
	}
	wakeupLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"resource":      string(api.QueueEvent),
	}
	skipBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
	wakeupBaseline := schedulerMetricValue(ctx, wakeupMetricName, wakeupLabels)

	// Starting allocate only after admission prevents the tighter capability
	// from turning this case back into a Job-level enqueue rejection.
	configureScheduler(pluginName, "allocate")
	By(fmt.Sprintf("waiting for %s to reject and cache the Task", pluginName))
	// Allocatable rejection has no PodGroup event of its own. A subsequent
	// allocate-stage skip is the direct observable proof that it was cached.
	waitForMetricIncrease(ctx, skipMetricName, skipLabels, skipBaseline)
	expectJobUnbound(ctx, job)

	By("restoring Queue capability so the rejected Task fits")
	updateQueueCPUCapability(ctx, queueName, "1")

	By(fmt.Sprintf("verifying %s's Queue hint wakes and schedules the Task", pluginName))
	waitForMetricIncrease(ctx, wakeupMetricName, wakeupLabels, wakeupBaseline)
	waitForJobReady(ctx, job)
}

// runTaskPodReleaseWakeupCase caches a Task that would exceed Queue quota when
// combined with an allocated Pod, then verifies that Pod deletion releases the
// quota and wakes the Task through the quota plugin's podHint.
func runTaskPodReleaseWakeupCase(pluginName, queueName, jobName string) {
	configureScheduler(pluginName, "enqueue")

	ctx := e2eutil.InitTestContext(e2eutil.Options{
		Queues: []string{queueName},
		CapabilityResource: map[string]v1.ResourceList{
			queueName: {v1.ResourceCPU: resource.MustParse("1")},
		},
		NodesNumLimit: 1,
		NodesResourceLimit: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
	})
	defer e2eutil.CleanupTestContext(ctx)

	job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
		Name:      jobName,
		Namespace: ctx.Namespace,
		Queue:     queueName,
		Min:       1,
		Tasks: []e2eutil.TaskSpec{{
			Name: "worker",
			Img:  e2eutil.DefaultBusyBoxImage,
			Rep:  1,
			Min:  1,
			Req: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("600m"),
				v1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Command: "sleep 300",
		}},
	})
	waitForJobPodGroupPhase(ctx, job, schedulingv1beta1.PodGroupInqueue)
	expectJobUnbound(ctx, job)

	// Bind a standalone Pod directly so it contributes 600m to the Queue while
	// enqueue-only mode keeps the target Task unbound. Together they need 1.2
	// CPUs, exceeding the Queue's 1 CPU effective capacity/deserved share.
	blockerRequest := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("600m"),
		v1.ResourceMemory: resource.MustParse("16Mi"),
	}
	targetNode, slots := e2eutil.ComputeNode(ctx, blockerRequest)
	Expect(targetNode).NotTo(BeEmpty())
	Expect(slots).To(BeNumerically(">=", 1))
	blockerGroupName := jobName + "-blocker"
	blockerMinResources := blockerRequest.DeepCopy()
	_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: blockerGroupName, Namespace: ctx.Namespace},
		Spec: schedulingv1beta1.PodGroupSpec{
			MinMember:    1,
			MinResources: &blockerMinResources,
			Queue:        queueName,
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	blocker := e2eutil.CreatePod(ctx, e2eutil.PodSpec{
		Name:          jobName + "-blocker",
		Node:          targetNode,
		Req:           blockerRequest,
		SchedulerName: "volcano",
		RestartPolicy: v1.RestartPolicyNever,
		Annotations: map[string]string{
			batchv1alpha1.QueueNameKey:                   queueName,
			schedulingv1beta1.KubeGroupNameAnnotationKey: blockerGroupName,
		},
	})
	Expect(e2eutil.WaitPodReady(ctx, blocker)).To(Succeed())

	skipLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"stage":         "allocate",
	}
	wakeupLabels := map[string]string{
		"job_namespace": ctx.Namespace,
		"job_name":      jobName,
		"resource":      string(fwk.Pod),
	}
	skipBaseline := schedulerMetricValue(ctx, skipMetricName, skipLabels)
	wakeupBaseline := schedulerMetricValue(ctx, wakeupMetricName, wakeupLabels)

	configureScheduler(pluginName, "allocate")
	By(fmt.Sprintf("waiting for %s to cache the Task that exceeds remaining Queue quota", pluginName))
	waitForMetricIncrease(ctx, skipMetricName, skipLabels, skipBaseline)
	expectJobUnbound(ctx, job)

	By("deleting the Pod that consumes the Task's Queue quota")
	e2eutil.DeletePod(ctx, blocker)

	By(fmt.Sprintf("verifying %s's Pod hint wakes and schedules the Task", pluginName))
	waitForMetricIncrease(ctx, wakeupMetricName, wakeupLabels, wakeupBaseline)
	waitForJobReady(ctx, job)
}

func updateQueueCPUCapability(ctx *e2eutil.TestContext, queueName, cpu string) {
	Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		queue.Spec.Capability[v1.ResourceCPU] = resource.MustParse(cpu)
		_, err = ctx.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

// configureQuotaPlugin selects exactly one quota plugin for this case and
// restores the suite's scheduler configuration after the case completes.
func configureQuotaPlugin(pluginName string) {
	configureScheduler(pluginName, "")
}

// configureScheduler selects one quota plugin and, when non-empty, replaces
// the action list. Each invocation restores its own preceding configuration,
// allowing a case to move from enqueue-only to allocate-only safely.
func configureScheduler(pluginName, actions string) {
	cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
	quotaPluginFound := false
	Expect(cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
		return e2eutil.ModifySchedulerConfig(data, func(config *e2eutil.SchedulerConfiguration) bool {
			changed := false
			if actions != "" && config.Actions != actions {
				config.Actions = actions
				changed = true
			}
			for tierIndex := range config.Tiers {
				for pluginIndex := range config.Tiers[tierIndex].Plugins {
					plugin := &config.Tiers[tierIndex].Plugins[pluginIndex]
					if plugin.Name != "proportion" && plugin.Name != "capacity" {
						continue
					}
					quotaPluginFound = true
					if plugin.Name != pluginName {
						*plugin = e2eutil.PluginOption{Name: pluginName}
						changed = true
					}
					return changed
				}
			}
			return changed
		})
	})).To(Succeed())
	Expect(quotaPluginFound).To(BeTrue(), "scheduler config should contain a proportion or capacity plugin")
	DeferCleanup(func() {
		Expect(cmc.UndoChanged()).To(Succeed())
	})
}

func waitForJobPodGroupPhase(ctx *e2eutil.TestContext, job *batchv1alpha1.Job, phase schedulingv1beta1.PodGroupPhase) {
	podGroupName := job.Name + "-" + string(job.UID)
	Eventually(func() schedulingv1beta1.PodGroupPhase {
		podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
			context.TODO(), podGroupName, metav1.GetOptions{})
		if err != nil {
			return ""
		}
		return podGroup.Status.Phase
	}, eventWakeupTimeout, 500*time.Millisecond).Should(Equal(phase))
}

func smallRequest() v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("10m"),
		v1.ResourceMemory: resource.MustParse("16Mi"),
	}
}

func waitForJobReady(ctx *e2eutil.TestContext, job *batchv1alpha1.Job) {
	Eventually(func() int32 {
		var ready int32
		for _, task := range e2eutil.GetTasksOfJob(ctx, job) {
			if task.Status.Phase == v1.PodRunning || task.Status.Phase == v1.PodSucceeded {
				ready++
			}
		}
		return ready
	}, eventWakeupTimeout, 500*time.Millisecond).Should(BeNumerically(">=", job.Spec.MinAvailable),
		"the Job should satisfy minAvailable after the matching event")
}

func waitForMetricIncrease(ctx *e2eutil.TestContext, name string, labels map[string]string, baseline float64) {
	Expect(e2eutil.WaitSchedulerCounterIncrease(context.TODO(), ctx.Kubeclient, name, labels, baseline, eventWakeupTimeout)).To(Succeed())
}

func schedulerMetricValue(ctx *e2eutil.TestContext, name string, labels map[string]string) float64 {
	value, err := e2eutil.SchedulerCounterValue(context.TODO(), ctx.Kubeclient, name, labels)
	Expect(err).NotTo(HaveOccurred())
	return value
}

func expectJobUnbound(ctx *e2eutil.TestContext, job *batchv1alpha1.Job) {
	tasks := e2eutil.GetTasksOfJob(ctx, job)
	Expect(tasks).NotTo(BeEmpty())
	for _, task := range tasks {
		Expect(task.Spec.NodeName).To(BeEmpty())
	}
}
