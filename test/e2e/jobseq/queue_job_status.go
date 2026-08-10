/*
Copyright 2022 The Volcano Authors.

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

package jobseq

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Queue Job Status Transition", func() {

	var testCtx *e2eutil.TestContext
	var q1 string
	var rep int32
	var podNamespace string

	BeforeEach(func() {
		q1 = "queue-jobs-status-transition"
		testCtx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1},
		})
		DeferCleanup(e2eutil.CleanupTestContext, testCtx)

		slot := e2eutil.HalfCPU
		rep = e2eutil.ClusterSize(testCtx, slot)
	})

	JustAfterEach(func() {
		e2eutil.DumpTestContextIfFailed(testCtx, CurrentSpecReport())
	})

	XIt("Transform from inqueque to running should succeed", func() {
		Skip("Prepare 2 job")

		if rep < 4 {
			err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
			Expect(err).NotTo(HaveOccurred())
		}

		for i := 0; i < 2; i++ {
			spec := &e2eutil.JobSpec{
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "queue-job",
						Img:  e2eutil.DefaultNginxImage,
						Req:  e2eutil.HalfCPU,
						Min:  rep,
						Rep:  rep,
					},
				},
			}
			spec.Name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
			spec.Queue = q1
			e2eutil.CreateJob(testCtx, spec)
		}

		By("Verify queue have pod groups inqueue")
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(testCtx, testCtx.Namespace, q1)
			return pgStats.Inqueue > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue inqueue")

		By("Verify queue have pod groups running")
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(testCtx, testCtx.Namespace, q1)
			return pgStats.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
	})

	It("Transform from running to pending should succeed", func() {
		By("Prepare 2 job")
		var firstJobName string

		podNamespace = testCtx.Namespace
		slot := e2eutil.HalfCPU

		if rep < 4 {
			err := fmt.Errorf("You need at least 2 logical cpu for this test case, please skip 'Queue Job Status Transition' when you see this message")
			Expect(err).NotTo(HaveOccurred())
		}

		for i := 0; i < 2; i++ {
			spec := &e2eutil.JobSpec{
				Tasks: []e2eutil.TaskSpec{
					{
						Name: "queue-job",
						Img:  e2eutil.DefaultNginxImage,
						Req:  slot,
						Min:  rep,
						Rep:  rep,
					},
				},
			}
			spec.Name = "queue-job-status-transition-test-job-" + strconv.Itoa(i)
			if i == 0 {
				firstJobName = spec.Name
			}
			spec.Queue = q1
			e2eutil.CreateJob(testCtx, spec)
		}

		By("Verify queue have pod groups running")
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(testCtx, testCtx.Namespace, q1)
			return pgStats.Running > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		clusterPods, err := testCtx.Kubeclient.CoreV1().Pods(podNamespace).List(context.TODO(), metav1.ListOptions{})
		for _, pod := range clusterPods.Items {
			if pod.Labels["volcano.sh/job-name"] == firstJobName {
				err = testCtx.Kubeclient.CoreV1().Pods(podNamespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
			}
		}

		By("Verify queue have pod groups Pending")
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(testCtx, testCtx.Namespace, q1)
			return pgStats.Pending > 0, nil
		})
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Pending")
	})

	It("Transform from running to unknown after a member is gone should succeed", func() {
		const pgName = "queue-podgroup-status-transition-unknown"

		By("Creating a PodGroup with two standalone members")
		pg, err := testCtx.Vcclient.SchedulingV1beta1().PodGroups(testCtx.Namespace).Create(context.TODO(), &v1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pgName,
				Namespace: testCtx.Namespace,
			},
			Spec: v1beta1.PodGroupSpec{
				MinMember: 2,
				Queue:     q1,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods := make([]*corev1.Pod, 0, 2)
		for i := 0; i < 2; i++ {
			pods = append(pods, e2eutil.CreatePod(testCtx, e2eutil.PodSpec{
				Name:          fmt.Sprintf("queue-podgroup-status-transition-%d", i),
				SchedulerName: "volcano",
				RestartPolicy: corev1.RestartPolicyNever,
				Req:           e2eutil.HalfCPU,
				Annotations: map[string]string{
					v1beta1.KubeGroupNameAnnotationKey: pgName,
				},
			}))
		}

		By("Waiting for both members and the PodGroup to be Running")
		for _, pod := range pods {
			err = e2eutil.WaitPodReady(testCtx, pod)
			Expect(err).NotTo(HaveOccurred())
		}
		err = e2eutil.WaitPodGroupPhase(testCtx, pg, v1beta1.PodGroupRunning)
		Expect(err).NotTo(HaveOccurred())

		By("Force deleting one member so it is gone rather than Releasing")
		zero := int64(0)
		err = testCtx.Kubeclient.CoreV1().Pods(testCtx.Namespace).Delete(context.TODO(), pods[0].Name, metav1.DeleteOptions{
			GracePeriodSeconds: &zero,
		})
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitPodGone(testCtx, pods[0].Name, testCtx.Namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying the PodGroup becomes Unknown because a required member is missing")
		err = e2eutil.WaitPodGroupPhase(testCtx, pg, v1beta1.PodGroupUnknown)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for PodGroup Unknown")
	})
})
