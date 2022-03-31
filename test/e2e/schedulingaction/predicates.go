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

package schedulingaction

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Predicates E2E Test", func() {

	It("Hostport", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		nn := e2eutil.ClusterNodeNumber(context)

		spec := &e2eutil.JobSpec{
			Name: "hp-spec",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Min:      int32(nn),
					Req:      e2eutil.OneCPU,
					Rep:      int32(nn * 2),
					Hostport: 28080,
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)

		err := e2eutil.WaitTasksReady(context, job, nn)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksPending(context, job, nn)
		Expect(err).NotTo(HaveOccurred())
	})

	It("NodeAffinity", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU
		nodeName, rep := e2eutil.ComputeNode(context, e2eutil.OneCPU)
		Expect(rep).NotTo(Equal(0))

		affinity := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchFields: []v1.NodeSelectorRequirement{
								{
									Key:      e2eutil.NodeFieldSelectorKeyNodeName,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}

		spec := &e2eutil.JobSpec{
			Name: "na-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      slot,
					Min:      1,
					Rep:      rep,
					Affinity: affinity,
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitTasksReady(context, job, int(rep))
		Expect(err).NotTo(HaveOccurred())

		pods := e2eutil.GetTasksOfJob(context, job)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Pod Affinity", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.HalfCPU
		_, rep := e2eutil.ComputeNode(context, e2eutil.HalfCPU)
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

		spec := &e2eutil.JobSpec{
			Name: "pa-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      slot,
					Min:      rep / 2,
					Rep:      rep / 2,
					Affinity: affinity,
					Labels:   labels,
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitTasksReady(context, job, int(rep/2))
		Expect(err).NotTo(HaveOccurred())

		pods := e2eutil.GetTasksOfJob(context, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Pod Anti-Affinity", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU

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

		spec := &e2eutil.JobSpec{
			Name: "pa-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      slot,
					Min:      2,
					Rep:      2,
					Affinity: affinity,
					Labels:   labels,
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitTasksReady(context, job, 2)
		Expect(err).NotTo(HaveOccurred())

		pods := e2eutil.GetTasksOfJob(context, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName

		for index, pod := range pods {
			if index != 0 {
				Expect(pod.Spec.NodeName).NotTo(Equal(nodeName))
			}
		}
	})

	It("Taints", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		defer e2eutil.RemoveTaintsFromAllNodes(context, taints)

		err := e2eutil.TaintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		spec := &e2eutil.JobSpec{
			Name: "tt-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.OneCPU,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err = e2eutil.WaitTasksPending(context, job, 1)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.RemoveTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(context, job, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Taints and Tolerations", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		taints := []v1.Taint{
			{
				Key:    "test-taint-key",
				Value:  "test-taint-val",
				Effect: v1.TaintEffectNoSchedule,
			},
		}
		defer e2eutil.RemoveTaintsFromAllNodes(context, taints)

		tolerations := []v1.Toleration{
			{
				Key:      "test-taint-key",
				Value:    "test-taint-val",
				Operator: v1.TolerationOpEqual,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}

		err := e2eutil.TaintAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		spec1 := &e2eutil.JobSpec{
			Name: "tt-job",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:         e2eutil.DefaultNginxImage,
					Req:         e2eutil.OneCPU,
					Min:         1,
					Rep:         1,
					Tolerations: tolerations,
				},
			},
		}

		spec2 := &e2eutil.JobSpec{
			Name: "tt-job-no-toleration",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.OneCPU,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job1 := e2eutil.CreateJob(context, spec1)
		err = e2eutil.WaitTasksReady(context, job1, 1)
		Expect(err).NotTo(HaveOccurred())

		job2 := e2eutil.CreateJob(context, spec2)
		err = e2eutil.WaitTasksPending(context, job2, 1)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.RemoveTaintsFromAllNodes(context, taints)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(context, job2, 1)
		Expect(err).NotTo(HaveOccurred())
	})
})
