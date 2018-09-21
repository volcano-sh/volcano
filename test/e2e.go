/*
Copyright 2017 The Kubernetes Authors.

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

package test

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
)

var _ = Describe("E2E Test", func() {
	It("Schedule Job", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)
		job := createJob(context, "qj-1", 2, rep, "busybox", oneCPU, nil)
		err := waitJobReady(context, job.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Schedule Multiple Jobs", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		rep := clusterSize(context, oneCPU)

		job1 := createJob(context, "qj-1", 2, rep, "busybox", oneCPU, nil)
		qj2 := createJob(context, "qj-2", 2, rep, "busybox", oneCPU, nil)
		qj3 := createJob(context, "qj-3", 2, rep, "busybox", oneCPU, nil)

		err := waitJobReady(context, job1.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, qj2.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, qj3.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Gang scheduling", func() {
		context := initTestContext()
		defer cleanupTestContext(context)
		rep := clusterSize(context, oneCPU)/2 + 1

		replicaset := createReplicaSet(context, "rs-1", rep, "nginx", oneCPU)
		err := waitReplicaSetReady(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		job := createJob(context, "gang-qj", rep, rep, "busybox", oneCPU, nil)
		err = waitJobNotReady(context, job.Name)
		Expect(err).NotTo(HaveOccurred())

		err = deleteReplicaSet(context, replicaset.Name)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job.Name)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job1 := createJob(context, "preemptee-qj", 1, rep, "nginx", slot, nil)
		err := waitTasksReady(context, job1.Name, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job2 := createJob(context, "preemptor-qj", 1, rep, "nginx", slot, nil)
		err = waitTasksReady(context, job2.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1.Name, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Multiple Preemption", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		job1 := createJob(context, "preemptee-qj", 1, rep, "nginx", slot, nil)
		err := waitTasksReady(context, job1.Name, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job2 := createJob(context, "preemptor-qj", 1, rep, "nginx", slot, nil)
		Expect(err).NotTo(HaveOccurred())

		job3 := createJob(context, "preemptor-qj2", 1, rep, "nginx", slot, nil)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job3.Name, int(rep)/3)
		Expect(err).NotTo(HaveOccurred())
	})

	It("NodeAffinity", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU
		nodeName, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		affinity := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchFields: []v1.NodeSelectorRequirement{
								{
									Key:      algorithm.NodeFieldSelectorKeyNodeName,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}

		job := createJob(context, "na-job", 1, 1, "nginx", slot, affinity)
		err := waitJobReady(context, job.Name)
		Expect(err).NotTo(HaveOccurred())

		pods := getPodOfJob(context, "na-job")
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Reclaim", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		jobName1 := "n1/qj-1"
		jobName2 := "n2/qj-2"

		slot := oneCPU
		rep := clusterSize(context, slot)

		createJob(context, jobName1, 1, rep, "nginx", slot, nil)
		err := waitJobReady(context, jobName1)
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		createJob(context, jobName2, 1, rep, "nginx", slot, nil)
		err = waitTasksReady(context, jobName2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, jobName1, expected)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Hostport", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nn := clusterNodeNumber(context)

		containers := createContainers("nginx", oneCPU, 28080)
		job := createJobWithOptions(context, "kube-batchd", "qj-1", int32(nn), int32(nn*2), nil, containers)

		err := waitTasksReady(context, job.Name, nn)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksNotReady(context, job.Name, nn)
		Expect(err).NotTo(HaveOccurred())
	})

})
