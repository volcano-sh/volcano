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

package job

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	cv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/job/plugins/env"
	"volcano.sh/volcano/pkg/controllers/job/plugins/svc"
)

var _ = Describe("Job E2E Test: Test Job Plugins", func() {
	It("Test SVC Plugin with Node Affinity", func() {
		jobName := "job-with-svc-plugin"
		taskName := "task"
		foundVolume := false
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		nodeName, rep := computeNode(ctx, oneCPU)
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

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			plugins: map[string][]string{
				"svc": {},
			},
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					req:      oneCPU,
					min:      1,
					rep:      1,
					name:     taskName,
					affinity: affinity,
				},
			},
		})

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-svc", jobName)
		_, err = ctx.kubeclient.CoreV1().ConfigMaps(ctx.namespace).Get(context.TODO(),
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Get(context.TODO(),
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := getTasksOfJob(ctx, job)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Test SSh Plugin with Pod Affinity", func() {
		jobName := "job-with-ssh-plugin"
		taskName := "task"
		foundVolume := false
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		_, rep := computeNode(ctx, oneCPU)
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

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			plugins: map[string][]string{
				"ssh": {"--no-root"},
			},
			tasks: []taskSpec{
				{
					img:      defaultNginxImage,
					req:      oneCPU,
					min:      rep,
					rep:      rep,
					affinity: affinity,
					labels:   labels,
					name:     taskName,
				},
			},
		})

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		secretName := genSSHSecretName(job)
		_, err = ctx.kubeclient.CoreV1().Secrets(ctx.namespace).Get(context.TODO(),
			secretName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Get(context.TODO(),
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == secretName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := getTasksOfJob(ctx, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Test SVC Plugin with disableNetworkPolicy", func() {
		jobName := "svc-with-disable-network-policy"
		taskName := "task"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		_, rep := computeNode(ctx, oneCPU)
		Expect(rep).NotTo(Equal(0))

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			plugins: map[string][]string{
				"svc": {"--disable-network-policy=true"},
			},
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  rep,
					name: taskName,
				},
			},
		})

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// Check whether network policy is created with job name
		networkPolicyName := jobName
		_, err = ctx.kubeclient.NetworkingV1().NetworkPolicies(ctx.namespace).Get(context.TODO(), networkPolicyName, v1.GetOptions{})
		// Error will occur because there is no policy should be created
		Expect(err).To(HaveOccurred())
	})

	It("Check Functionality of all plugins", func() {
		jobName := "job-with-all-plugin"
		taskName := "task"
		foundVolume := false
		foundEnv := false
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		_, rep := computeNode(ctx, oneCPU)
		Expect(rep).NotTo(Equal(0))

		job := createJob(ctx, &jobSpec{
			namespace: ctx.namespace,
			name:      jobName,
			plugins: map[string][]string{
				"ssh": {"--no-root"},
				"env": {},
				"svc": {},
			},
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  rep,
					name: taskName,
				},
			},
		})

		err := waitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		secretName := genSSHSecretName(job)
		_, err = ctx.kubeclient.CoreV1().Secrets(ctx.namespace).Get(context.TODO(),
			secretName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Get(context.TODO(),
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
		_, err = ctx.kubeclient.CoreV1().Services(job.Namespace).Get(context.TODO(), job.Name, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})

func genSSHSecretName(job *batch.Job) string {
	return job.Name + "-ssh"
}
