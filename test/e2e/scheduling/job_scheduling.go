/*
Copyright 2017 The Volcano Authors.

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

package scheduling

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		job := createJob(ctx, &jobSpec{
			name: "qj-1",
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		})

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		rep := clusterSize(ctx, oneCPU)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultBusyBoxImage,
					req: oneCPU,
					min: 2,
					rep: rep,
				},
			},
		}

		job.name = "mqj-1"
		job1 := createJob(ctx, job)
		job.name = "mqj-2"
		job2 := createJob(ctx, job)
		job.name = "mqj-3"
		job3 := createJob(ctx, job)

		err := waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(ctx, job3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)/2 + 1

		replicaset := createReplicaSet(ctx, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(ctx, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "gang-qj",
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img:     defaultBusyBoxImage,
					req:     oneCPU,
					min:     rep,
					rep:     rep,
					command: "sleep 10s",
				},
			},
		}

		job := createJob(ctx, jobSpec)
		err = waitJobStatePending(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobUnschedulable(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(ctx, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		job := &jobSpec{
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep,
					rep: rep,
				},
			},
		}

		job.name = "gang-fq-qj1"
		job1 := createJob(ctx, job)
		err := waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "gang-fq-qj2"
		job2 := createJob(ctx, job)
		err = waitJobStatePending(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Contains both best-effort pod and non-best-effort pod", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		rep := clusterSize(ctx, oneCPU)

		if rep < 2 {
			fmt.Println("Skip e2e test for insufficient resources.")
			return
		}

		jobSpec := &jobSpec{
			name:      "gang-both-best-effort-non-best-effort-pods",
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: rep / 2,
					rep: rep / 2,
				},
				{
					img: defaultNginxImage,
					min: rep - rep/2,
					rep: rep - rep/2,
				},
			},
		}

		job := createJob(ctx, jobSpec)
		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule BestEffort Job", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

		spec := &jobSpec{
			name: "test",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 2,
					rep: rep,
				},
				{
					img: defaultNginxImage,
					min: 2,
					rep: rep / 2,
				},
			},
		}

		job := createJob(ctx, spec)

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Statement", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

		spec := &jobSpec{
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}

		spec.name = "st-qj-1"
		job1 := createJob(ctx, spec)
		err := waitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		now := time.Now()

		spec.name = "st-qj-2"
		job2 := createJob(ctx, spec)
		err = waitJobUnschedulable(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		// No preemption event
		evicted, err := jobEvicted(ctx, job1, now)()
		Expect(err).NotTo(HaveOccurred())
		Expect(evicted).NotTo(BeTrue())
	})

	It("support binpack policy", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU

		By("create base job")
		spec := &jobSpec{
			name:      "binpack-base-1",
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: 1,
				},
			},
		}

		baseJob := createJob(ctx, spec)
		err := waitJobReady(ctx, baseJob)
		Expect(err).NotTo(HaveOccurred())

		basePods := getTasksOfJob(ctx, baseJob)
		basePod := basePods[0]
		baseNodeName := basePod.Spec.NodeName

		node, err := ctx.kubeclient.CoreV1().Nodes().Get(context.TODO(), baseNodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		clusterPods, err := ctx.kubeclient.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
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
		for need.LessEqual(alloc) {
			count++
			alloc.Sub(need)
		}

		By(fmt.Sprintf("create test job with %d pods", count))
		spec = &jobSpec{
			name:      "binpack-test-1",
			namespace: ctx.namespace,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: count,
					rep: count,
				},
			},
		}
		job := createJob(ctx, spec)
		err = waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pods := getTasksOfJob(ctx, baseJob)
		for _, pod := range pods {
			nodeName := pod.Spec.NodeName
			Expect(nodeName).Should(Equal(baseNodeName),
				fmt.Sprintf("Pod %s/%s should assign to node %s, but not %s", pod.Namespace, pod.Name, baseNodeName, nodeName))
		}
	})

	It("Schedule v1.Job type using Volcano scheduler", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		parallel := int32(2)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: ctx.namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						RestartPolicy: v1.RestartPolicyNever,
						SchedulerName: schedulerName,
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
		job, err := ctx.kubeclient.BatchV1().Jobs(ctx.namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobPhaseReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule v1.Job type using Volcano scheduler with error case", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		parallel := int32(2)

		errorJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: ctx.namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						SchedulerName: schedulerName,
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
				Namespace: ctx.namespace,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &parallel,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						RestartPolicy: v1.RestartPolicyNever,
						SchedulerName: schedulerName,
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
		_, err := ctx.kubeclient.BatchV1().Jobs(ctx.namespace).Create(context.TODO(), errorJob, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())

		//create job
		job, err = ctx.kubeclient.BatchV1().Jobs(ctx.namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitJobPhaseReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Namespace Fair Share", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)
		const fairShareNamespace = "fairshare"
		_, err := ctx.kubeclient.CoreV1().Namespaces().Create(context.TODO(),
			&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fairShareNamespace,
				},
			},
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			deleteForeground := metav1.DeletePropagationForeground
			err := ctx.kubeclient.CoreV1().Namespaces().Delete(context.TODO(),
				fairShareNamespace,
				metav1.DeleteOptions{
					PropagationPolicy: &deleteForeground,
				})
			Expect(err).NotTo(HaveOccurred())

			err = wait.Poll(100*time.Millisecond, twoMinute, namespaceNotExistWithName(ctx, fairShareNamespace))
			Expect(err).NotTo(HaveOccurred())
		}()

		slot := halfCPU
		rep := clusterSize(ctx, slot)

		createJobToNamespace := func(namespace string, index int, replica int32) *vcbatch.Job {
			spec := &jobSpec{
				name:      fmt.Sprintf("namespace-fair-share-%s-%d", namespace, index),
				namespace: namespace,
				tasks: []taskSpec{
					{
						img:     defaultNginxImage,
						command: "sleep 10000",
						req:     slot,
						min:     2,
						rep:     replica,
					},
				},
			}
			job := createJob(ctx, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToNamespace("default", 123, rep*2)
		err = waitJobReady(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToNamespace(ctx.namespace, i, 2)
			createJobToNamespace(fairShareNamespace, i, 2)
		}

		By("release occupied cluster resources")
		deleteBackground := metav1.DeletePropagationBackground
		err = ctx.vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(context.TODO(),
			occupiedJob.Name,
			metav1.DeleteOptions{
				PropagationPolicy: &deleteBackground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = waitJobCleanedUp(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in fs/test namespace scheduled")
		fsScheduledPod := 0
		testScheduledPod := 0
		expectPod := int(rep)
		if expectPod%1 == 1 {
			expectPod--
		}
		err = wait.Poll(100*time.Millisecond, twoMinute, func() (bool, error) {
			fsScheduledPod = 0
			testScheduledPod = 0

			pods, err := ctx.kubeclient.CoreV1().Pods(fairShareNamespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if isPodScheduled(&pod) {
					fsScheduledPod++
				}
			}

			pods, err = ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if isPodScheduled(&pod) {
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
		q1, q2 := "q1", "q2"
		ctx := initTestContext(options{
			queues: []string{q1, q2},
		})
		defer cleanupTestContext(ctx)

		slot := halfCPU
		rep := clusterSize(ctx, slot)

		createJobToQueue := func(queue string, index int, replica int32) *vcbatch.Job {
			spec := &jobSpec{
				name:      fmt.Sprintf("queue-fair-share-%s-%d", queue, index),
				namespace: ctx.namespace,
				queue:     queue,
				tasks: []taskSpec{
					{
						img:     defaultNginxImage,
						command: "sleep 10000",
						req:     slot,
						min:     2,
						rep:     replica,
					},
				},
			}
			job := createJob(ctx, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToQueue("default", 123, rep*2)
		err := waitJobReady(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToQueue(q1, i, 2)
			createJobToQueue(q2, i, 2)
		}

		By(fmt.Sprintf("release occupied cluster resources, %s/%s", occupiedJob.Namespace, occupiedJob.Name))
		deleteForeground := metav1.DeletePropagationBackground
		err = ctx.vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(context.TODO(),
			occupiedJob.Name,
			metav1.DeleteOptions{
				PropagationPolicy: &deleteForeground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = waitJobCleanedUp(ctx, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in queue q1/q2 scheduled")
		q1ScheduledPod := 0
		q2ScheduledPod := 0
		expectPod := int(rep)
		if expectPod%1 == 1 {
			expectPod--
		}
		err = wait.Poll(100*time.Millisecond, twoMinute, func() (bool, error) {
			q1ScheduledPod = 0
			q2ScheduledPod = 0

			pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if !isPodScheduled(&pod) {
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
