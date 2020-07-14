/*
Copyright 2020 The Volcano Authors.

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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

const (
	highPriority        = "high-priority"
	middlePriority      = "middle-priority"
	lowPriority         = "low-priority"
	highPriorityValue   = 100
	middlePriorityValue = 50
	lowPriorityValue    = 10
)

var _ = Describe("Job E2E Test", func() {
	It("schedule high priority job without preemption when resource is enough", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "preemptee"
		job.pri = lowPriority
		preempteeJob := createJob(ctx, job)
		err := waitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor"
		job.pri = highPriority
		preemptorJob := createJob(ctx, job)
		err = waitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, preemptorJob, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("schedule high priority job with preemption when idle resource is NOT enough but preemptee resource is enough", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

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

		job.name = "preemptee"
		job.pri = lowPriority
		preempteeJob := createJob(ctx, job)
		err := waitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "preemptor"
		job.pri = highPriority
		job.min = rep / 2
		preemptorJob := createJob(ctx, job)
		err = waitTasksReady(ctx, preempteeJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, preemptorJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("preemption doesn't work when podgroup is pending", func() {
		ctx := initTestContext(options{
			priorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &thirtyCPU,
			},
		}
		_, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Create(context.TODO(), pg, v1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		slot := oneCPU
		rep := clusterSize(ctx, slot)
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
		job.name = "preemptee"
		job.pri = lowPriority
		preempteeJob := createJob(ctx, job)
		err = waitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace:   ctx.namespace,
				Name:        "preemptor-pod",
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName:     "volcano",
				Containers:        createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
				PriorityClassName: highPriority,
			},
		}
		_, err = ctx.kubeclient.CoreV1().Pods(ctx.namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		Expect(err).To(HaveOccurred())
	})

	It("preemption only works in the same queue", func() {
		ctx := initTestContext(options{
			queues: []string{"q1-preemption", "q2-reference"},
			priorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep / 2,
				},
			},
		}

		job.name = "j1-q1"
		job.pri = lowPriority
		job.queue = "q1-preemption"
		queue1Job := createJob(ctx, job)
		err := waitTasksReady(ctx, queue1Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-q2"
		job.pri = lowPriority
		job.queue = "q2-reference"
		queue2Job := createJob(ctx, job)
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j3-q1"
		job.pri = highPriority
		job.queue = "q1-preemption"
		job.tasks[0].rep = rep
		queue1Job3 := createJob(ctx, job)
		err = waitTasksReady(ctx, queue1Job3, 1)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, queue1Job, 0)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("preemption doesn't work when total resource of idle resource and preemptee is NOT enough", func() {
		ctx := initTestContext(options{
			queues: []string{"q1-preemption", "q2-reference"},
			priorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1-q1"
		job.pri = lowPriority
		job.queue = "q1-preemption"
		queue1Job := createJob(ctx, job)
		err := waitTasksReady(ctx, queue1Job, 1)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j2-q2"
		job.pri = lowPriority
		job.queue = "q2-reference"
		job.tasks[0].min = rep / 2
		job.tasks[0].rep = rep / 2
		queue2Job := createJob(ctx, job)
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "j3-q1"
		job.pri = highPriority
		job.queue = "q1-preemption"
		job.tasks[0].min = rep
		job.tasks[0].rep = rep
		queue1Job3 := createJob(ctx, job)
		err = waitTasksReady(ctx, queue1Job3, int(rep))
		Expect(err).To(HaveOccurred())
		err = waitTasksReady(ctx, queue1Job, 1)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("multi-preemptor-jobs who are in different priority", func() {
		Skip("https://github.com/volcano-sh/volcano/issues/911")
		ctx := initTestContext(options{
			queues: []string{"q1-preemption"},
			priorityClasses: map[string]int32{
				highPriority:   highPriorityValue,
				middlePriority: middlePriorityValue,
				lowPriority:    lowPriorityValue,
			},
		})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)
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

		job.name = "low-priority-job"
		job.pri = lowPriority
		job.queue = "q1-preemption"
		lowPriorityJob := createJob(ctx, job)
		err := waitTasksReady(ctx, lowPriorityJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.name = "middle-prority-job"
		job.pri = middlePriority
		job.queue = "q1-preemption"
		job.tasks[0].rep = rep / 2
		job.tasks[0].min = rep / 2
		middlePriorityJob := createJob(ctx, job)
		err = waitTasksReady(ctx, middlePriorityJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, lowPriorityJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.name = "high-priority-job"
		job.pri = highPriority
		job.queue = "q1-preemption"
		job.tasks[0].rep = rep
		job.tasks[0].min = rep
		highPriorityJob := createJob(ctx, job)
		err = waitTasksReady(ctx, highPriorityJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, lowPriorityJob, 0)
		Expect(err).NotTo(HaveOccurred())
		err = waitTasksReady(ctx, middlePriorityJob, 0)
		Expect(err).NotTo(HaveOccurred())
	})
})
