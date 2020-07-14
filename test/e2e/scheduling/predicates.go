/*
Copyright 2018 The Volcano Authors.

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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Predicates E2E Test", func() {

	It("Hostport", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		nn := clusterNodeNumber(context)

		spec := &jobSpec{
			name: "hp-spec",
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					min:      int32(nn),
					req:      oneCPU,
					rep:      int32(nn * 2),
					hostport: 28080,
				},
			},
		}

		job := createJob(context, spec)

		err := waitTasksReady(context, job, nn)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksPending(context, job, nn)
		Expect(err).NotTo(HaveOccurred())
	})

	It("NodeAffinity", func() {
		context := initTestContext(options{})
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
									Key:      nodeFieldSelectorKeyNodeName,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}

		spec := &jobSpec{
			name: "na-job",
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					req:      slot,
					min:      1,
					rep:      rep,
					affinity: affinity,
				},
			},
		}

		job := createJob(context, spec)
		err := waitTasksReady(context, job, int(rep))
		Expect(err).NotTo(HaveOccurred())

		pods := getTasksOfJob(context, job)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Pod Affinity", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU
		_, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		labels := map[string]string{"foo": "bar"}

		affinity := &v1.Affinity{
			PodAffinity: &v1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}

		spec := &jobSpec{
			name: "pa-job",
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					req:      slot,
					min:      rep,
					rep:      rep,
					affinity: affinity,
					labels:   labels,
				},
			},
		}

		job := createJob(context, spec)
		err := waitTasksReady(context, job, int(rep))
		Expect(err).NotTo(HaveOccurred())

		pods := getTasksOfJob(context, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Pod Anti-Affinity", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU

		labels := map[string]string{"foo": "bar"}

		affinity := &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}

		spec := &jobSpec{
			name: "pa-job",
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					req:      slot,
					min:      2,
					rep:      2,
					affinity: affinity,
					labels:   labels,
				},
			},
		}

		job := createJob(context, spec)
		err := waitTasksReady(context, job, 2)
		Expect(err).NotTo(HaveOccurred())

		pods := getTasksOfJob(context, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName

		for index, pod := range pods {
			if index != 0 {
				Expect(pod.Spec.NodeName).NotTo(Equal(nodeName))
			}
		}
	})

	It("Taints", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		defer removeTaintsFromAllNodes(context, taints)

		err := taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		spec := &jobSpec{
			name: "tt-job",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: 1,
					rep: 1,
				},
			},
		}

		job := createJob(context, spec)
		err = waitTasksPending(context, job, 1)
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Taints and Tolerations", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		defer removeTaintsFromAllNodes(context, taints)

		tolerations := []v1.Toleration{
			{
				Key:      "test-taint-key",
				Value:    "test-taint-val",
				Operator: v1.TolerationOpEqual,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}

		err := taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		spec1 := &jobSpec{
			name: "tt-job",
			tasks: []taskSpec{
				{
					img:         defaultNginxImage,
					req:         oneCPU,
					min:         1,
					rep:         1,
					tolerations: tolerations,
				},
			},
		}

		spec2 := &jobSpec{
			name: "tt-job-no-toleration",
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: oneCPU,
					min: 1,
					rep: 1,
				},
			},
		}

		job1 := createJob(context, spec1)
		err = waitTasksReady(context, job1, 1)
		Expect(err).NotTo(HaveOccurred())

		job2 := createJob(context, spec2)
		err = waitTasksPending(context, job2, 1)
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job2, 1)
		Expect(err).NotTo(HaveOccurred())
	})
})
