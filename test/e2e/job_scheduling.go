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

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := createJob(context, &jobSpec{
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

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)

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
		job1 := createJob(context, job)
		job.name = "mqj-2"
		job2 := createJob(context, job)
		job.name = "mqj-3"
		job3 := createJob(context, job)

		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, defaultNginxImage, oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		jobSpec := &jobSpec{
			name:      "gang-qj",
			namespace: "test",
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

		job := createJob(context, jobSpec)
		err = waitJobStatePending(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobUnschedulable(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		job := &jobSpec{
			namespace: "test",
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
		job1 := createJob(context, job)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "gang-fq-qj2"
		job2 := createJob(context, job)
		err = waitJobStatePending(context, job2)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Contains both best-effort pod and non-best-effort pod", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)

		if rep < 2 {
			fmt.Println("Skip e2e test for insufficient resources.")
			return
		}

		jobSpec := &jobSpec{
			name:      "gang-both-best-effort-non-best-effort-pods",
			namespace: "test",
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

		job := createJob(context, jobSpec)
		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		context := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "preemptee-qj"
		job.pri = workerPriority
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj"
		job.pri = masterPriority
		job.min = rep / 2
		job2 := createJob(context, job)
		err = waitTasksReady(context, job1, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		context := initTestContext(options{
			priorityClasses: map[string]int32{
				masterPriority: masterPriorityValue,
				workerPriority: workerPriorityValue,
			},
		})
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		job.name = "multipreemptee-qj"
		job.pri = workerPriority
		job1 := createJob(context, job)

		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "multipreemptor-qj1"
		job.pri = masterPriority
		job.min = rep / 3
		job2 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "multipreemptor-qj2"
		job.pri = masterPriority
		job3 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job3, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule BestEffort Job", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

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

		job := createJob(context, spec)

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Statement", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			namespace: "test",
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
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		now := time.Now()

		spec.name = "st-qj-2"
		job2 := createJob(context, spec)
		err = waitJobUnschedulable(context, job2)
		Expect(err).NotTo(HaveOccurred())

		// No preemption event
		evicted, err := jobEvicted(context, job1, now)()
		Expect(err).NotTo(HaveOccurred())
		Expect(evicted).NotTo(BeTrue())
	})

	It("support binpack policy", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU

		By("create base job")
		spec := &jobSpec{
			name:      "binpack-base-1",
			namespace: "test",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: 1,
				},
			},
		}

		baseJob := createJob(context, spec)
		err := waitJobReady(context, baseJob)
		Expect(err).NotTo(HaveOccurred())

		basePods := getTasksOfJob(context, baseJob)
		basePod := basePods[0]
		baseNodeName := basePod.Spec.NodeName

		node, err := context.kubeclient.CoreV1().Nodes().Get(baseNodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		clusterPods, err := context.kubeclient.CoreV1().Pods(v1.NamespaceAll).List(metav1.ListOptions{})
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
			namespace: "test",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: count,
					rep: count,
				},
			},
		}
		job := createJob(context, spec)
		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pods := getTasksOfJob(context, baseJob)
		for _, pod := range pods {
			nodeName := pod.Spec.NodeName
			Expect(nodeName).Should(Equal(baseNodeName),
				fmt.Sprintf("Pod %s/%s should assign to node %s, but not %s", pod.Namespace, pod.Name, baseNodeName, nodeName))
		}
	})

	It("Schedule v1.Job type using Volcano scheduler", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		namespace := "test"
		parallel := int32(2)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: namespace,
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
		job, err := context.kubeclient.BatchV1().Jobs(namespace).Create(job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobPhaseReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule v1.Job type using Volcano scheduler with error case", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)
		namespace := "test"
		parallel := int32(2)

		errorJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job1",
				Namespace: namespace,
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
				Namespace: namespace,
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
		errorJob, err := context.kubeclient.BatchV1().Jobs(namespace).Create(errorJob)
		Expect(err).To(HaveOccurred())

		//create job
		job, err = context.kubeclient.BatchV1().Jobs(namespace).Create(job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobPhaseReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Namespace Fair Share", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		const fairShareNamespace = "fairshare"

		_, err := context.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: fairShareNamespace,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			deleteForeground := metav1.DeletePropagationForeground
			err := context.kubeclient.CoreV1().Namespaces().Delete(fairShareNamespace, &metav1.DeleteOptions{
				PropagationPolicy: &deleteForeground,
			})
			Expect(err).NotTo(HaveOccurred())

			err = wait.Poll(100*time.Millisecond, twoMinute, namespaceNotExistWithName(context, fairShareNamespace))
			Expect(err).NotTo(HaveOccurred())
		}()

		slot := halfCPU
		rep := clusterSize(context, slot)

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
			job := createJob(context, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToNamespace("default", 123, rep*2)
		err = waitJobReady(context, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToNamespace("test", i, 2)
			createJobToNamespace(fairShareNamespace, i, 2)
		}

		By("release occupied cluster resources")
		deleteBackground := metav1.DeletePropagationBackground
		err = context.vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(occupiedJob.Name,
			&metav1.DeleteOptions{
				PropagationPolicy: &deleteBackground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = waitJobCleanedUp(context, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in fs/test namespace scheduled")
		fsScheduledPod := 0
		testScheduledPod := 0
		expectPod := int(rep)
		err = wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
			fsScheduledPod = 0
			testScheduledPod = 0

			pods, err := context.kubeclient.CoreV1().Pods(fairShareNamespace).List(metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if isPodScheduled(&pod) {
					fsScheduledPod++
				}
			}

			pods, err = context.kubeclient.CoreV1().Pods("test").List(metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if isPodScheduled(&pod) {
					testScheduledPod++
				}
			}

			if testScheduledPod+fsScheduledPod == expectPod {
				return true, nil
			}

			return false, nil
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(testScheduledPod).Should(Or(Equal(expectPod/2), Equal((expectPod+1)/2)),
			fmt.Sprintf("expectPod %d, fsScheduledPod %d, testScheduledPod %d", expectPod, fsScheduledPod, testScheduledPod))
	})

	It("Queue Fair Share", func() {
		context := initTestContext(options{
			queues: []string{defaultQueue1, defaultQueue2},
		})
		defer cleanupTestContext(context)

		slot := halfCPU
		rep := clusterSize(context, slot)

		createJobToQueue := func(queue string, index int, replica int32) *vcbatch.Job {
			spec := &jobSpec{
				name:      fmt.Sprintf("queue-fair-share-%s-%d", queue, index),
				namespace: "test",
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
			job := createJob(context, spec)
			return job
		}

		By("occupy all cluster resources")
		occupiedJob := createJobToQueue("default", 123, rep*2)
		err := waitJobReady(context, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < int(rep); i++ {
			createJobToQueue(defaultQueue1, i, 2)
			createJobToQueue(defaultQueue2, i, 2)
		}

		By(fmt.Sprintf("release occupied cluster resources, %s/%s", occupiedJob.Namespace, occupiedJob.Name))
		deleteForeground := metav1.DeletePropagationBackground
		err = context.vcclient.BatchV1alpha1().Jobs(occupiedJob.Namespace).Delete(occupiedJob.Name,
			&metav1.DeleteOptions{
				PropagationPolicy: &deleteForeground,
			})
		Expect(err).NotTo(HaveOccurred())

		By("wait occupied cluster resources releasing")
		err = waitJobCleanedUp(context, occupiedJob)
		Expect(err).NotTo(HaveOccurred())

		By("wait pod in queue q1/q2 scheduled")
		q1ScheduledPod := 0
		q2ScheduledPod := 0
		expectPod := int(rep)
		err = wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
			q1ScheduledPod = 0
			q2ScheduledPod = 0

			pods, err := context.kubeclient.CoreV1().Pods("test").List(metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if !isPodScheduled(&pod) {
					continue
				}
				jobName := pod.Annotations[vcbatch.JobNameKey]
				if strings.Contains(jobName, "queue-fair-share-"+defaultQueue1) {
					q1ScheduledPod++
				}
				if strings.Contains(jobName, "queue-fair-share-"+defaultQueue2) {
					q2ScheduledPod++
				}
			}

			if q2ScheduledPod+q1ScheduledPod == expectPod {
				return true, nil
			}

			return false, nil
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(q2ScheduledPod).Should(Or(Equal(expectPod/2), Equal((expectPod+1)/2)),
			fmt.Sprintf("expectPod %d, q1ScheduledPod %d, q2ScheduledPod %d", expectPod, q1ScheduledPod, q2ScheduledPod))
	})

})
