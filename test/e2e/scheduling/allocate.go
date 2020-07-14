/*
Copyright 2019 The Volcano Authors.

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

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = ginkgo.Describe("Job E2E Test", func() {
	ginkgo.It("allocate work when resource is enough", func() {
		ctx := initTestContext(options{
			nodesNumLimit: 2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "low"
		lowReqJob := createJob(ctx, job)
		err := waitJobReady(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "high"
		job.tasks[0].req = slot2
		highReqJob := createJob(ctx, job)
		err = waitJobReady(ctx, highReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when resource is NOT enough", func() {
		ctx := initTestContext(options{
			nodesNumLimit: 1,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}
		slot3 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("3000m"),
			corev1.ResourceMemory: resource.MustParse("3072Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot2,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "middle"
		midReqJob := createJob(ctx, job)
		err := waitJobReady(ctx, midReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "low"
		job.tasks[0].req = slot1
		lowReqJob := createJob(ctx, job)
		err = waitJobReady(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "high"
		job.tasks[0].req = slot3
		highReqJob := createJob(ctx, job)
		err = waitJobPending(ctx, highReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when podgroup is pending", func() {
		ctx := initTestContext(options{
			nodesNumLimit: 2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ctx.namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &thirtyCPU,
			},
		}

		_, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1"
		existJob := createJob(ctx, job)
		err = waitJobReady(ctx, existJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "j2"
		allocateJob := createJobWithPodGroup(ctx, job, pgName)
		err = waitJobPending(ctx, allocateJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when job NOT satisify predicates", func() {
		ctx := initTestContext(options{
			nodesNumLimit: 1,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer cleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
					labels: map[string]string{
						"job": "for-test-predicate",
					},
				},
			},
		}

		job.name = "j1"
		existJob := createJob(ctx, job)
		err := waitJobReady(ctx, existJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "j2"
		job.tasks[0].affinity = &corev1.Affinity{
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
		allocateJob := createJob(ctx, job)
		err = waitJobPending(ctx, allocateJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("allocate don't work when queue is overused", func() {
		q1 := "q1"
		q2 := "q2"
		ctx := initTestContext(options{
			queues:        []string{q1, q2},
			nodesNumLimit: 2,
			nodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})

		defer cleanupTestContext(ctx)

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

		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot1,
					min: 1,
					rep: 1,
				},
			},
		}

		job.name = "j1-q1"
		job.queue = q1
		queue1Job1 := createJob(ctx, job)
		err := waitJobStateReady(ctx, queue1Job1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "j2-q1"
		job.queue = q1
		job.tasks[0].req = slot2
		queue1Job2 := createJob(ctx, job)
		err = waitJobStateReady(ctx, queue1Job2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "j3-q2"
		job.queue = q2
		job.tasks[0].req = slot3
		queue2Job3 := createJob(ctx, job)
		err = waitJobStateReady(ctx, queue2Job3)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.name = "j4-q1"
		job.queue = q1
		job.tasks[0].req = slot4
		queue1Job4 := createJob(ctx, job)
		err = waitJobPending(ctx, queue1Job4)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
