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

package jobp

import (
	"context"
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job UID isolation", func() {
	It("Reconciles a VCJob recreated with the same name after stale cleanup retries", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		const replicas int32 = 8
		var terminationGracePeriodSeconds int64 = 1
		jobSpec := &e2eutil.JobSpec{
			Name:     "same-name-recreate-job",
			Plugins:  map[string][]string{"svc": {}},
			Policies: []vcbatch.LifecyclePolicy{{Events: []vcbus.Event{vcbus.PodEvictedEvent}, Action: vcbus.RestartJobAction}},
			Tasks: []e2eutil.TaskSpec{{
				Name:                  "worker",
				Img:                   e2eutil.DefaultBusyBoxImage,
				Min:                   replicas,
				Rep:                   replicas,
				Command:               `trap '' TERM; while true; do sleep 1; done`,
				RestartPolicy:         v1.RestartPolicyNever,
				DefaultGracefulPeriod: &terminationGracePeriodSeconds,
			}},
		}

		By("creating the first VCJob lifecycle")
		oldJob := e2eutil.CreateJob(ctx, jobSpec)
		Expect(e2eutil.WaitJobReady(ctx, oldJob)).To(Succeed())
		Expect(e2eutil.GetTasksOfJob(ctx, oldJob)).To(HaveLen(int(replicas)))
		for round := 0; round < 3; round++ {
			By(fmt.Sprintf("repeating same-name lifecycle replacement, round %d", round+1))

			// Hold a Pod name independently of kubelet termination timing.
			const holdFinalizer = "e2e.volcano.sh/pod-name-hold"
			victim := e2eutil.GetTasksOfJob(ctx, oldJob)[0]
			victim.Finalizers = append(victim.Finalizers, holdFinalizer)
			_, err := ctx.Kubeclient.CoreV1().Pods(oldJob.Namespace).Update(context.TODO(), victim, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			release := func() error {
				return retry.RetryOnConflict(retry.DefaultRetry, func() error {
					pod, err := ctx.Kubeclient.CoreV1().Pods(victim.Namespace).Get(context.TODO(), victim.Name, metav1.GetOptions{})
					if apierrors.IsNotFound(err) {
						return nil
					}
					if err != nil {
						return err
					}
					if pod.UID != victim.UID {
						return nil
					}
					pod.Finalizers = slices.DeleteFunc(pod.Finalizers, func(value string) bool { return value == holdFinalizer })
					_, err = ctx.Kubeclient.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
					return err
				})
			}
			defer func() { Expect(release()).To(Succeed()) }()

			By("deleting the first VCJob while keeping its Pods terminating")
			Expect(ctx.Vcclient.BatchV1alpha1().Jobs(oldJob.Namespace).Delete(
				context.TODO(), oldJob.Name, metav1.DeleteOptions{})).To(Succeed())
			Eventually(func() bool {
				_, err := ctx.Vcclient.BatchV1alpha1().Jobs(oldJob.Namespace).Get(
					context.TODO(), oldJob.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, e2eutil.OneMinute, 200*time.Millisecond).Should(BeTrue())
			Eventually(func() bool {
				pods, err := ctx.Kubeclient.CoreV1().Pods(oldJob.Namespace).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false
				}
				for i := range pods.Items {
					owner := metav1.GetControllerOf(&pods.Items[i])
					if owner != nil && owner.UID == oldJob.UID && pods.Items[i].DeletionTimestamp != nil {
						return true
					}
				}
				return false
			}, e2eutil.OneMinute, 200*time.Millisecond).Should(BeTrue())

			By("creating a new VCJob lifecycle with the same namespace and name")
			newJob := e2eutil.CreateJob(ctx, jobSpec)
			Expect(newJob.UID).NotTo(Equal(oldJob.UID))
			By("waiting through repeated name conflicts without failing or restarting the new Job")
			Consistently(func() error {
				current, err := ctx.Vcclient.BatchV1alpha1().Jobs(newJob.Namespace).Get(context.TODO(), newJob.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if current.UID != newJob.UID || current.Status.Version != 0 || current.Status.RetryCount != 0 || current.Status.State.Phase == vcbatch.Failed || current.Status.State.Phase == vcbatch.Terminating || current.Status.State.Phase == vcbatch.Terminated {
					return fmt.Errorf("name conflict changed the new lifecycle: %+v", current.Status)
				}
				return nil
			}, 20*time.Second, 500*time.Millisecond).Should(Succeed())
			By("releasing the old Pod name")
			Expect(release()).To(Succeed())
			Expect(e2eutil.WaitJobReady(ctx, newJob)).To(Succeed())

			currentPods := func() ([]v1.Pod, error) {
				podList, err := ctx.Kubeclient.CoreV1().Pods(newJob.Namespace).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return nil, err
				}
				pods := make([]v1.Pod, 0, replicas)
				for i := range podList.Items {
					owner := metav1.GetControllerOf(&podList.Items[i])
					if owner != nil && owner.UID == newJob.UID {
						pods = append(pods, podList.Items[i])
					}
				}
				return pods, nil
			}

			By("waiting for all Pods owned by the new lifecycle")
			Eventually(func() error {
				pods, err := currentPods()
				if err != nil {
					return err
				}
				if len(pods) != int(replicas) {
					return fmt.Errorf("got %d Pods owned by new Job UID %s, want %d", len(pods), newJob.UID, replicas)
				}
				return nil
			}, e2eutil.TwoMinute, 500*time.Millisecond).Should(Succeed())

			By("waiting beyond the old cleanup backoff and verifying the new lifecycle remains healthy")
			Consistently(func() error {
				job, err := ctx.Vcclient.BatchV1alpha1().Jobs(newJob.Namespace).Get(
					context.TODO(), newJob.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if job.UID != newJob.UID {
					return fmt.Errorf("current Job UID changed from %s to %s", newJob.UID, job.UID)
				}
				pods, err := currentPods()
				if err != nil {
					return err
				}
				if len(pods) != int(replicas) {
					return fmt.Errorf("got %d Pods after stale cleanup window, want %d", len(pods), replicas)
				}
				return nil
			}, 15*time.Second, time.Second).Should(Succeed())

			By("deleting one current Pod and verifying reconciliation still works")
			before, err := ctx.Vcclient.BatchV1alpha1().Jobs(newJob.Namespace).Get(context.TODO(), newJob.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(before.Status.Version).To(BeZero(), "old lifecycle events must not restart the new Job")
			Expect(before.Status.RetryCount).To(BeZero())
			pods, err := currentPods()
			Expect(err).NotTo(HaveOccurred())
			Expect(pods).To(HaveLen(int(replicas)))
			deletedPod := pods[0]
			zero := int64(0)
			Expect(ctx.Kubeclient.CoreV1().Pods(newJob.Namespace).Delete(context.TODO(), deletedPod.Name,
				metav1.DeleteOptions{GracePeriodSeconds: &zero})).To(Succeed())
			Eventually(func() types.UID {
				pod, err := ctx.Kubeclient.CoreV1().Pods(newJob.Namespace).Get(
					context.TODO(), deletedPod.Name, metav1.GetOptions{})
				if err != nil {
					return ""
				}
				owner := metav1.GetControllerOf(pod)
				if owner == nil || owner.UID != newJob.UID {
					return ""
				}
				return pod.UID
			}, e2eutil.TwoMinute, 500*time.Millisecond).Should(And(Not(BeEmpty()), Not(Equal(deletedPod.UID))))
			Expect(e2eutil.WaitJobReady(ctx, newJob)).To(Succeed())
			Eventually(func() bool {
				current, err := ctx.Vcclient.BatchV1alpha1().Jobs(newJob.Namespace).Get(context.TODO(), newJob.Name, metav1.GetOptions{})
				if err != nil || current.Status.Version != 1 || current.Status.RetryCount != 1 {
					return false
				}
				svc, err := ctx.Kubeclient.CoreV1().Services(newJob.Namespace).Get(context.TODO(), newJob.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				owner := metav1.GetControllerOf(svc)
				return owner != nil && owner.UID == newJob.UID
			}, e2eutil.TwoMinute, 500*time.Millisecond).Should(BeTrue(), "current eviction policy and owned Service must recover")
			oldJob = newJob
		}
	})

	DescribeTable("Preserves targeted lifecycle recovery", func(action vcbus.Action) {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		policy := vcbatch.LifecyclePolicy{Events: []vcbus.Event{vcbus.PodEvictedEvent}, Action: action}
		if action == vcbus.RestartPodAction {
			policy.Timeout = &metav1.Duration{Duration: time.Second}
		}
		worker := e2eutil.TaskSpec{Name: "worker", Img: e2eutil.DefaultBusyBoxImage, Min: 4, Rep: 4, Command: "sleep 3600", RestartPolicy: v1.RestartPolicyNever}
		if action == vcbus.RestartPartitionAction {
			worker.PartitionPolicy = &vcbatch.PartitionPolicySpec{TotalPartitions: 2, PartitionSize: 2}
		}
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{Name: "targeted-lifecycle", Policies: []vcbatch.LifecyclePolicy{policy}, Tasks: []e2eutil.TaskSpec{
			worker, {Name: "other", Img: e2eutil.DefaultBusyBoxImage, Min: 1, Rep: 1, Command: "sleep 3600", RestartPolicy: v1.RestartPolicyNever},
		}})
		Expect(e2eutil.WaitJobReady(ctx, job)).To(Succeed())
		original := e2eutil.GetTasksOfJob(ctx, job)
		Expect(original).To(HaveLen(5))
		victim, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).Get(context.TODO(), job.Name+"-worker-0", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		if action == vcbus.RestartPartitionAction {
			Expect(victim.Labels[vcbatch.TaskPartitionID]).NotTo(BeEmpty())
		}
		zero := int64(0)
		Expect(ctx.Kubeclient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), victim.Name,
			metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &victim.UID}})).To(Succeed())
		Eventually(func() error {
			for _, old := range original {
				current, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).Get(context.TODO(), old.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				owner := metav1.GetControllerOf(current)
				if owner == nil || owner.UID != job.UID || current.Status.Phase != v1.PodRunning || current.DeletionTimestamp != nil {
					return fmt.Errorf("Pod %s is not ready in current lifecycle", current.Name)
				}
				restarted := old.Name == victim.Name
				if action == vcbus.RestartTaskAction {
					restarted = old.Annotations[vcbatch.TaskSpecKey] == "worker"
				}
				if action == vcbus.RestartPartitionAction {
					restarted = old.Annotations[vcbatch.TaskSpecKey] == "worker" && old.Labels[vcbatch.TaskPartitionID] == victim.Labels[vcbatch.TaskPartitionID]
				}
				if (current.UID != old.UID) != restarted {
					return fmt.Errorf("Pod %s changed UID=%v, expected=%v", old.Name, current.UID != old.UID, restarted)
				}
			}
			return nil
		}, e2eutil.TwoMinute, 500*time.Millisecond).Should(Succeed())
		current, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(current.Status.Version).To(BeZero(), "targeted actions must not bump the Job version")
	},
		Entry("RestartPod with timeout after the target is deleted", vcbus.RestartPodAction),
		Entry("RestartTask retains Pods outside the task", vcbus.RestartTaskAction),
		Entry("RestartPartition retains Pods outside the partition", vcbus.RestartPartitionAction),
	)

})
