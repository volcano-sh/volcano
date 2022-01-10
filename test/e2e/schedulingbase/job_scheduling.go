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

package schedulingbase

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: "qj-1",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultBusyBoxImage,
					Req: e2eutil.OneCPU,
					Min: 2,
					Rep: rep,
				},
			},
		})

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultBusyBoxImage,
					Req: e2eutil.OneCPU,
					Min: 2,
					Rep: rep,
				},
			},
		}

		job.Name = "mqj-1"
		job1 := e2eutil.CreateJob(ctx, job)
		job.Name = "mqj-2"
		job2 := e2eutil.CreateJob(ctx, job)
		job.Name = "mqj-3"
		job3 := e2eutil.CreateJob(ctx, job)

		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobReady(ctx, job3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)/2 + 1

		replicaset := e2eutil.CreateReplicaSet(ctx, "rs-1", rep, e2eutil.DefaultNginxImage, e2eutil.OneCPU)
		err := e2eutil.WaitReplicaSetReady(ctx, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &e2eutil.JobSpec{
			Name:      "gang-qj",
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Img:     e2eutil.DefaultBusyBoxImage,
					Req:     e2eutil.OneCPU,
					Min:     rep,
					Rep:     rep,
					Command: "sleep 10s",
				},
			},
		}

		job := e2eutil.CreateJob(ctx, jobSpec)
		err = e2eutil.WaitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobUnschedulable(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.DeleteReplicaSet(ctx, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		job := &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.OneCPU,
					Min: rep,
					Rep: rep,
				},
			},
		}

		job.Name = "gang-fq-qj1"
		job1 := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "gang-fq-qj2"
		job2 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobStatePending(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Contains both best-effort pod and non-best-effort pod", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		rep := e2eutil.ClusterSize(ctx, e2eutil.OneCPU)

		if rep < 2 {
			fmt.Println("Skip e2e test for insufficient resources.")
			return
		}

		jobSpec := &e2eutil.JobSpec{
			Name:      "gang-both-best-effort-non-best-effort-pods",
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "best-effort",
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  rep / 2,
					Rep:  rep / 2,
				},
				{
					Name: "non-best-effort",
					Img:  e2eutil.DefaultNginxImage,
					Min:  rep - rep/2,
					Rep:  rep - rep/2,
				},
			},
		}

		job := e2eutil.CreateJob(ctx, jobSpec)
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule BestEffort Job", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		spec := &e2eutil.JobSpec{
			Name: "test",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 2,
					Rep: rep,
				},
				{
					Img: e2eutil.DefaultNginxImage,
					Min: 2,
					Rep: rep / 2,
				},
			},
		}

		job := e2eutil.CreateJob(ctx, spec)

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Statement", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		spec := &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep,
					Rep: rep,
				},
			},
		}

		spec.Name = "st-qj-1"
		job1 := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		now := time.Now()

		spec.Name = "st-qj-2"
		job2 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobUnschedulable(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		// No preemption event
		evicted, err := e2eutil.JobEvicted(ctx, job1, now)()
		Expect(err).NotTo(HaveOccurred())
		Expect(evicted).NotTo(BeTrue())
	})

	It("support binpack policy", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU

		By("create base job")
		spec := &e2eutil.JobSpec{
			Name:      "binpack-base-1",
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 1,
					Rep: 1,
				},
			},
		}

		baseJob := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, baseJob)
		Expect(err).NotTo(HaveOccurred())

		basePods := e2eutil.GetTasksOfJob(ctx, baseJob)
		basePod := basePods[0]
		baseNodeName := basePod.Spec.NodeName

		node, err := ctx.Kubeclient.CoreV1().Nodes().Get(context.TODO(), baseNodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		clusterPods, err := ctx.Kubeclient.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		alloc := schedulingapi.NewResource(node.Status.Allocatable)
		for _, pod := range clusterPods.Items {
			nodeName := pod.Spec.NodeName
			if nodeName != baseNodeName || len(nodeName) == 0 || pod.DeletionTimestamp != nil {
				continue
			}

			if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
				continue
			}

			for _, c := range pod.Spec.Containers {
				req := schedulingapi.NewResource(c.Resources.Requests)
				alloc.Sub(req)
			}
		}

		need := schedulingapi.NewResource(v1.ResourceList{"cpu": resource.MustParse("500m")})
		var count int32
		for need.LessEqual(alloc, schedulingapi.Zero) {
			count++
			alloc.Sub(need)
		}

		By(fmt.Sprintf("create test job with %d pods", count))
		spec = &e2eutil.JobSpec{
			Name:      "binpack-test-1",
			Namespace: ctx.Namespace,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: count,
					Rep: count,
				},
			},
		}
		job := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pods := e2eutil.GetTasksOfJob(ctx, baseJob)
		for _, pod := range pods {
			nodeName := pod.Spec.NodeName
			Expect(nodeName).Should(Equal(baseNodeName),
				fmt.Sprintf("Pod %s/%s should assign to node %s, but not %s", pod.Namespace, pod.Name, baseNodeName, nodeName))
		}
	})

	It("Schedule v1.Job type using Volcano scheduler", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		parallel := int32(2)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: ctx.Namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						RestartPolicy: v1.RestartPolicyNever,
						SchedulerName: e2eutil.SchedulerName,
						Containers: []v1.Container{
							{
								Name:  "test-container",
								Image: "nginx",
							},
						},
					},
				},
			},
		}

		//create job
		job, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobPhaseReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule v1.Job type using Volcano scheduler with error case", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		parallel := int32(2)

		errorJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: ctx.Namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						SchedulerName: e2eutil.SchedulerName,
						Containers: []v1.Container{
							{
								Name:  "test-container",
								Image: "nginx",
							},
						},
					},
				},
			},
		}

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: ctx.Namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						RestartPolicy: v1.RestartPolicyNever,
						SchedulerName: e2eutil.SchedulerName,
						Containers: []v1.Container{
							{
								Name:  "test-container",
								Image: "nginx",
							},
						},
					},
				},
			},
		}

		//create error job
		_, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), errorJob, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())

		//create job
		job, err = ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobPhaseReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Namespace Fair Share", func() {
		Skip("Failed when add yaml and test case may fail in some condition")
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)
		const fairShareNamespace = "fairshare"
		_, err := ctx.Kubeclient.CoreV1().Namespaces().Create(context.TODO(),
			&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fairShareNamespace,
				},
			},
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			deleteForeground := metav1.DeletePropagationForeground
			err := ctx.Kubeclient.CoreV1().Namespaces().Delete(context.TODO(),
				fairShareNamespace,
				metav1.DeleteOptions{
					PropagationPolicy: &deleteForeground,
				})
			Expect(err).NotTo(HaveOccurred())

			err = wait.Poll(100*time.Millisecond, e2eutil.FiveMinute, e2eutil.NamespaceNotExistWithName(ctx, fairShareNamespace))
			Expect(err).NotTo(HaveOccurred())
		}()

		slot := e2eutil.HalfCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		createJobToNamespace := func(namespace string, index int, replica int32) *vcbatch.Job {
			spec := &e2eutil.JobSpec{
				Name:      fmt.Sprintf("namespace-fair-share-%s-%d", namespace, index),
				Namespace: namespace,
				Tasks: []e2eutil.TaskSpec{
					{
						Img:     e2eutil.DefaultNginxImage,
						Command: "sleep 10000",
						Req:     slot,
						Min:     2,
						Rep:     replica,
					},
				},
			}
			job := e2eutil.CreateJob(ctx, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToNamespace("default", 123, rep*2)
		err = e2eutil.WaitJobReady(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToNamespace(ctx.Namespace, i, 2)
			createJobToNamespace(fairShareNamespace, i, 2)
		}

		By("release occupied cluster resources")
		deleteBackground := metav1.DeletePropagationBackground
		err = ctx.Vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(context.TODO(),
			occupiedJob.Name,
			metav1.DeleteOptions{
				PropagationPolicy: &deleteBackground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = e2eutil.WaitJobCleanedUp(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in fs/test namespace scheduled")
		fsScheduledPod := 0
		testScheduledPod := 0
		expectPod := int(rep)
		if expectPod%1 == 1 {
			expectPod--
		}
		err = wait.Poll(100*time.Millisecond, e2eutil.FiveMinute, func() (bool, error) {
			fsScheduledPod = 0
			testScheduledPod = 0

			pods, err := ctx.Kubeclient.CoreV1().Pods(fairShareNamespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if e2eutil.IsPodScheduled(&pod) {
					fsScheduledPod++
				}
			}

			pods, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if e2eutil.IsPodScheduled(&pod) {
					testScheduledPod++
				}
			}

			// gang scheduling
			if testScheduledPod+fsScheduledPod == expectPod {
				return true, nil
			}

			return false, nil
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(testScheduledPod).Should(BeNumerically(">=", expectPod/2-1), fmt.Sprintf("expectPod %d, fsScheduledPod %d, testScheduledPod %d", expectPod, fsScheduledPod, testScheduledPod))
		Expect(testScheduledPod).Should(BeNumerically("<=", expectPod/2+1), fmt.Sprintf("expectPod %d, fsScheduledPod %d, testScheduledPod %d", expectPod, fsScheduledPod, testScheduledPod))
	})

	It("Queue Fair Share", func() {
		Skip("Failed when add yaml, test case may fail in some condition")
		q1, q2 := "q1", "q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1, q2},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.HalfCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		createJobToQueue := func(queue string, index int, replica int32) *vcbatch.Job {
			spec := &e2eutil.JobSpec{
				Name:      fmt.Sprintf("queue-fair-share-%s-%d", queue, index),
				Namespace: ctx.Namespace,
				Queue:     queue,
				Tasks: []e2eutil.TaskSpec{
					{
						Img:     e2eutil.DefaultNginxImage,
						Command: "sleep 10000",
						Req:     slot,
						Min:     2,
						Rep:     replica,
					},
				},
			}
			job := e2eutil.CreateJob(ctx, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToQueue("default", 123, rep*2)
		err := e2eutil.WaitJobReady(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToQueue(q1, i, 2)
			createJobToQueue(q2, i, 2)
		}

		By(fmt.Sprintf("release occupied cluster resources, %s/%s", occupiedJob.Namespace, occupiedJob.Name))
		deleteForeground := metav1.DeletePropagationBackground
		err = ctx.Vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(context.TODO(),
			occupiedJob.Name,
			metav1.DeleteOptions{
				PropagationPolicy: &deleteForeground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = e2eutil.WaitJobCleanedUp(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in queue q1/q2 scheduled")
		q1ScheduledPod := 0
		q2ScheduledPod := 0
		expectPod := int(rep)
		if expectPod%1 == 1 {
			expectPod--
		}
		err = wait.Poll(100*time.Millisecond, e2eutil.FiveMinute, func() (bool, error) {
			q1ScheduledPod = 0
			q2ScheduledPod = 0

			pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if !e2eutil.IsPodScheduled(&pod) {
					continue
				}
				jobName := pod.Annotations[vcbatch.JobNameKey]
				if strings.Contains(jobName, "queue-fair-share-"+q1) {
					q1ScheduledPod++
				}
				if strings.Contains(jobName, "queue-fair-share-"+q2) {
					q2ScheduledPod++
				}
			}

			if q2ScheduledPod+q1ScheduledPod == expectPod {
				return true, nil
			}

			return false, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(q2ScheduledPod).Should(BeNumerically(">=", expectPod/2-1),
			fmt.Sprintf("expectPod %d, q1ScheduledPod %d, q2ScheduledPod %d", expectPod, q1ScheduledPod, q2ScheduledPod))

		Expect(q2ScheduledPod).Should(BeNumerically("<=", expectPod/2+1),
			fmt.Sprintf("expectPod %d, q1ScheduledPod %d, q2ScheduledPod %d", expectPod, q1ScheduledPod, q2ScheduledPod))
	})
})
