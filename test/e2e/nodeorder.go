/*
Copyright 2019 The Kubernetes Authors.

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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletapi "k8s.io/kubernetes/pkg/kubelet/apis"
)

var _ = Describe("NodeOrder E2E Test", func() {
	It("Node Affinity Test", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nodeNames := getAllWorkerNodes(context)
		var preferredSchedulingTermSlice []v1.PreferredSchedulingTerm
		nodeSelectorRequirement := v1.NodeSelectorRequirement{Key: kubeletapi.LabelHostname, Operator: v1.NodeSelectorOpIn, Values: []string{nodeNames[0]}}
		nodeSelectorTerm := v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{nodeSelectorRequirement}}
		schedulingTerm := v1.PreferredSchedulingTerm{Weight: 100, Preference: nodeSelectorTerm}
		preferredSchedulingTermSlice = append(preferredSchedulingTermSlice, schedulingTerm)

		slot := oneCPU
		_, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		affinity := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: preferredSchedulingTermSlice,
			},
		}

		job := &jobSpec{
			name: "pa-job",
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

		_, pg := createJob(context, job)
		err := waitPodGroupReady(context, pg)
		checkError(context, err)

		pods := getPodOfPodGroup(context, pg)
		//All pods should be scheduled in particular node
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeNames[0]))
		}
	})

	It("Pod Affinity Test", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		var preferredSchedulingTermSlice []v1.WeightedPodAffinityTerm
		labelSelectorRequirement := metav1.LabelSelectorRequirement{Key: "test", Operator: metav1.LabelSelectorOpIn, Values: []string{"e2e"}}
		labelSelector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{labelSelectorRequirement}}
		podAffinityTerm := v1.PodAffinityTerm{LabelSelector: labelSelector, TopologyKey: "kubernetes.io/hostname"}
		weightedPodAffinityTerm := v1.WeightedPodAffinityTerm{Weight: 100, PodAffinityTerm: podAffinityTerm}
		preferredSchedulingTermSlice = append(preferredSchedulingTermSlice, weightedPodAffinityTerm)

		labels := make(map[string]string)
		labels["test"] = "e2e"

		job1 := &jobSpec{
			name: "pa-job1",
			tasks: []taskSpec{
				{
					img:    "nginx",
					req:    halfCPU,
					min:    1,
					rep:    1,
					labels: labels,
				},
			},
		}

		_, pg1 := createJob(context, job1)
		err := waitPodGroupReady(context, pg1)
		checkError(context, err)

		pods := getPodOfPodGroup(context, pg1)
		nodeName := pods[0].Spec.NodeName

		affinity := &v1.Affinity{
			PodAffinity: &v1.PodAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: preferredSchedulingTermSlice,
			},
		}

		job2 := &jobSpec{
			name: "pa-job2",
			tasks: []taskSpec{
				{
					img:      "nginx",
					req:      halfCPU,
					min:      1,
					rep:      1,
					affinity: affinity,
				},
			},
		}

		_, pg2 := createJob(context, job2)
		err = waitPodGroupReady(context, pg2)
		checkError(context, err)

		podsWithAffinity := getPodOfPodGroup(context, pg2)
		// All Pods Should be Scheduled in same node
		nodeNameWithAffinity := podsWithAffinity[0].Spec.NodeName
		Expect(nodeNameWithAffinity).To(Equal(nodeName))

	})

	It("Least Requested Resource Test", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		nodeNames := getAllWorkerNodes(context)
		affinityNodeOne := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      kubeletapi.LabelHostname,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeNames[0]},
								},
							},
						},
					},
				},
			},
		}

		job1 := &jobSpec{
			name: "pa-job",
			tasks: []taskSpec{
				{
					img:      "nginx",
					req:      halfCPU,
					min:      3,
					rep:      3,
					affinity: affinityNodeOne,
				},
			},
		}

		//Schedule Job in first Node
		_, pg1 := createJob(context, job1)
		err := waitPodGroupReady(context, pg1)
		checkError(context, err)

		affinityNodeTwo := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      kubeletapi.LabelHostname,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeNames[1]},
								},
							},
						},
					},
				},
			},
		}

		job2 := &jobSpec{
			name: "pa-job1",
			tasks: []taskSpec{
				{
					img:      "nginx",
					req:      halfCPU,
					min:      3,
					rep:      3,
					affinity: affinityNodeTwo,
				},
			},
		}

		//Schedule Job in Second Node
		_, pg2 := createJob(context, job2)
		err = waitPodGroupReady(context, pg2)
		checkError(context, err)

		testJob := &jobSpec{
			name: "pa-test-job",
			tasks: []taskSpec{
				{
					img: "nginx",
					req: oneCPU,
					min: 1,
					rep: 1,
				},
			},
		}

		//This job should be scheduled in third node
		_, pg3 := createJob(context, testJob)
		err = waitPodGroupReady(context, pg3)
		checkError(context, err)

		pods := getPodOfPodGroup(context, pg3)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).NotTo(Equal(nodeNames[0]))
			Expect(pod.Spec.NodeName).NotTo(Equal(nodeNames[1]))
		}
	})
})
