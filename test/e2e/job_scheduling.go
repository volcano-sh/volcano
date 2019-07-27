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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbapi "volcano.sh/volcano/pkg/scheduler/api"
)

var _ = Describe("Job E2E Test", func() {
	It("Schedule Job", func() {
		context := initTestContext()
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
		context := initTestContext()
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
		context := initTestContext()
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
		err = waitJobStateInqueue(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobUnschedulable(context, job)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling: Full Occupied", func() {
		context := initTestContext()
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

	It("Preemption", func() {
		context := initTestContext()
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
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor-qj"
		job2 := createJob(context, job)
		err = waitTasksReady(context, job1, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		context := initTestContext()
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
		job1 := createJob(context, job)
		err := waitTasksReady(context, job1, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "multipreemptor-qj1"
		job2 := createJob(context, job)
		Expect(err).NotTo(HaveOccurred())

		job.name = "multipreemptor-qj2"
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
		context := initTestContext()
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
		context := initTestContext()
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
		context := initTestContext()
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

		alloc := kbapi.NewResource(node.Status.Allocatable)
		for _, pod := range clusterPods.Items {
			nodeName := pod.Spec.NodeName
			if nodeName != baseNodeName || len(nodeName) == 0 || pod.DeletionTimestamp != nil {
				continue
			}

			if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
				continue
			}

			for _, c := range pod.Spec.Containers {
				req := kbapi.NewResource(c.Resources.Requests)
				alloc.Sub(req)
			}
		}

		need := kbapi.NewResource(v1.ResourceList{"cpu": resource.MustParse("500m")})
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
		context := initTestContext()
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
		context := initTestContext()
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
})
