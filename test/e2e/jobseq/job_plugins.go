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

package jobseq

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	cv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/job/plugins/env"
	"volcano.sh/volcano/pkg/controllers/job/plugins/svc"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Job E2E Test: Test Job Plugins", func() {
	It("Test SVC Plugin with Node Affinity", func() {
		jobName := "job-with-svc-plugin"
		taskName := "task"
		foundVolume := false
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		nodeName, rep := e2eutil.ComputeNode(ctx, e2eutil.OneCPU)
		Expect(rep).NotTo(Equal(0))

		affinity := &cv1.Affinity{
			NodeAffinity: &cv1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &cv1.NodeSelector{
					NodeSelectorTerms: []cv1.NodeSelectorTerm{
						{
							MatchFields: []cv1.NodeSelectorRequirement{
								{
									Key:      "metadata.name",
									Operator: cv1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Plugins: map[string][]string{
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      e2eutil.OneCPU,
					Min:      1,
					Rep:      1,
					Name:     taskName,
					Affinity: affinity,
				},
			},
		})

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-svc", jobName)
		_, err = ctx.Kubeclient.CoreV1().ConfigMaps(ctx.Namespace).Get(context.TODO(),
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(),
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := e2eutil.GetTasksOfJob(ctx, job)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Test SSh Plugin with Pod Affinity", func() {
		jobName := "job-with-ssh-plugin"
		taskName := "task"
		foundVolume := false
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		_, rep := e2eutil.ComputeNode(ctx, e2eutil.OneCPU)
		Expect(rep).NotTo(Equal(0))

		labels := map[string]string{"foo": "bar"}

		affinity := &cv1.Affinity{
			PodAffinity: &cv1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []cv1.PodAffinityTerm{
					{
						LabelSelector: &v1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Plugins: map[string][]string{
				"ssh": {"--no-root"},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      e2eutil.HalfCPU,
					Min:      rep,
					Rep:      rep,
					Affinity: affinity,
					Labels:   labels,
					Name:     taskName,
				},
			},
		})

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		secretName := genSSHSecretName(job)
		_, err = ctx.Kubeclient.CoreV1().Secrets(ctx.Namespace).Get(context.TODO(),
			secretName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(),
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == secretName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := e2eutil.GetTasksOfJob(ctx, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Test SVC Plugin with disableNetworkPolicy", func() {
		jobName := "svc-with-disable-network-policy"
		taskName := "task"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		_, rep := e2eutil.ComputeNode(ctx, e2eutil.OneCPU)
		Expect(rep).NotTo(Equal(0))

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Plugins: map[string][]string{
				"svc": {"--disable-network-policy=true"},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  1,
					Rep:  rep,
					Name: taskName,
				},
			},
		})

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Check whether network policy is created with job name
		networkPolicyName := jobName
		_, err = ctx.Kubeclient.NetworkingV1().NetworkPolicies(ctx.Namespace).Get(context.TODO(), networkPolicyName, v1.GetOptions{})
		// Error will occur because there is no policy should be created
		Expect(err).To(HaveOccurred())
	})

	It("Check Functionality of all plugins", func() {
		jobName := "job-with-all-plugin"
		taskName := "task"
		foundVolume := false
		foundEnv := false
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		_, rep := e2eutil.ComputeNode(ctx, e2eutil.OneCPU)
		Expect(rep).NotTo(Equal(0))

		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Namespace: ctx.Namespace,
			Name:      jobName,
			Plugins: map[string][]string{
				"ssh": {"--no-root"},
				"env": {},
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  1,
					Rep:  rep,
					Name: taskName,
				},
			},
		})

		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		secretName := genSSHSecretName(job)
		_, err = ctx.Kubeclient.CoreV1().Secrets(ctx.Namespace).Get(context.TODO(),
			secretName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Get(context.TODO(),
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == secretName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		// Check whether env exists in the containers and initContainers
		containers := pod.Spec.Containers
		containers = append(containers, pod.Spec.InitContainers...)
		envNames := []string{
			env.TaskVkIndex,
			env.TaskIndex,
			fmt.Sprintf(svc.EnvTaskHostFmt, strings.ToUpper(taskName)),
			fmt.Sprintf(svc.EnvHostNumFmt, strings.ToUpper(taskName)),
		}

		for _, container := range containers {
			for _, name := range envNames {
				foundEnv = false
				for _, envi := range container.Env {
					if envi.Name == name {
						foundEnv = true
						break
					}
				}

				Expect(foundEnv).To(BeTrue(),
					fmt.Sprintf("container: %s, env name: %s", container.Name, name))
			}
		}

		// Check whether service is created with job name
		_, err = ctx.Kubeclient.CoreV1().Services(job.Namespace).Get(context.TODO(), job.Name, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})

func genSSHSecretName(job *batch.Job) string {
	return job.Name + "-ssh"
}
