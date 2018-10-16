/*
Copyright 2018 The Kubernetes Authors.

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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
)

var _ = Describe("Predicates E2E Test", func() {
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

		job := &jobSpec{
			name: "na-job",
			tasks: []taskSpec{
				{
					img:      "nginx",
					req:      slot,
					min:      1,
					rep:      1,
					affinity: affinity,
				},
			},
		}

		_, pg := createJobEx(context, job)
		err := waitPodGroupReady(context, pg)
		Expect(err).NotTo(HaveOccurred())

		pods := getPodOfPodGroup(context, pg)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Hostport", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nn := clusterNodeNumber(context)

		job := &jobSpec{
			name: "hp-job",
			tasks: []taskSpec{
				{
					img:      "nginx",
					min:      int32(nn),
					req:      oneCPU,
					rep:      int32(nn * 2),
					hostport: 28080,
				},
			},
		}

		_, pg := createJobEx(context, job)

		err := waitTasksReadyEx(context, pg, nn)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksPendingEx(context, pg, nn)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Pod Affinity", func() {
		context := initTestContext()
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

		job := &jobSpec{
			name: "pa-job",
			tasks: []taskSpec{
				{
					img:      "nginx",
					req:      slot,
					min:      rep,
					rep:      rep,
					affinity: affinity,
					labels:   labels,
				},
			},
		}

		_, pg := createJobEx(context, job)
		err := waitPodGroupReady(context, pg)
		Expect(err).NotTo(HaveOccurred())

		pods := getPodOfPodGroup(context, pg)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Taints/Tolerations", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}

		err := taintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		job := &jobSpec{
			name: "tt-job",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: oneCPU,
					min: 1,
					rep: 1,
				},
			},
		}

		_, pg := createJobEx(context, job)
		err = waitPodGroupPending(context, pg)
		Expect(err).NotTo(HaveOccurred())

		err = removeTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodGroupReady(context, pg)
		Expect(err).NotTo(HaveOccurred())
	})

})
