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

package e2e

import (
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/job/plugins/env"
)

var _ = Describe("Job E2E Test: Test Job Plugins", func() {
	It("SVC Plugin with Node Affinity", func() {
		jobName := "job-with-svc-plugin"
		namespace := "test"
		taskName := "task"
		foundVolume := false
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		nodeName, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		affinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchFields: []corev1.NodeSelectorRequirement{
								{
									Key:      api.NodeFieldSelectorKeyNodeName,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}

		job := createJob(context, &jobSpec{
			namespace: namespace,
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

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-svc", jobName)
		_, err = context.kubeclient.CoreV1().ConfigMaps(namespace).Get(
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := context.kubeclient.CoreV1().Pods(namespace).Get(
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := getTasksOfJob(context, job)
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("SSh Plugin with Pod Affinity", func() {
		jobName := "job-with-ssh-plugin"
		namespace := "test"
		taskName := "task"
		foundVolume := false
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		_, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		labels := map[string]string{"foo": "bar"}

		affinity := &corev1.Affinity{
			PodAffinity: &corev1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &v1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}

		job := createJob(context, &jobSpec{
			namespace: namespace,
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

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-ssh", jobName)
		_, err = context.kubeclient.CoreV1().ConfigMaps(namespace).Get(
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := context.kubeclient.CoreV1().Pods(namespace).Get(
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		pods := getTasksOfJob(context, job)
		// All pods should be scheduled to the same node.
		nodeName := pods[0].Spec.NodeName
		for _, pod := range pods {
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
		}
	})

	It("Check Functionality of all plugins", func() {
		jobName := "job-with-all-plugin"
		namespace := "test"
		taskName := "task"
		foundVolume := false
		foundEnv := false
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		_, rep := computeNode(context, oneCPU)
		Expect(rep).NotTo(Equal(0))

		job := createJob(context, &jobSpec{
			namespace: namespace,
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

		err := waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		pluginName := fmt.Sprintf("%s-ssh", jobName)
		_, err = context.kubeclient.CoreV1().ConfigMaps(namespace).Get(
			pluginName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pod, err := context.kubeclient.CoreV1().Pods(namespace).Get(
			fmt.Sprintf(helpers.PodNameFmt, jobName, taskName, 0), v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == pluginName {
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue())

		// Check whether env exists in the pod
		for _, container := range pod.Spec.Containers {
			for _, envi := range container.Env {
				if envi.Name == env.TaskVkIndex {
					foundEnv = true
					break
				}
			}
		}
		Expect(foundEnv).To(BeTrue())

		// Check whether service is created with job name
		_, err = context.kubeclient.CoreV1().Services(job.Namespace).Get(job.Name, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Check network access while set networkpolicy", func() {
		jobName := "np-test"
		namespace := "test"
		taskName := "task"
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		job := createJob(context, &jobSpec{
			namespace: namespace,
			name:      jobName,
			plugins: map[string][]string{
				"svc": {},
			},
			tasks: []taskSpec{
				{
					img:  defaultNginxImage, // serves on 80
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: taskName,
				},
			},
		})

		pod := &corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
				Name:      "test",
			},
			Spec: corev1.PodSpec{
				Containers: createContainers(curlImage, "/bin/sleep 3650d", "", nil, nil, 0),
			},
		}

		pod, err := context.kubeclient.CoreV1().Pods(pod.Namespace).Create(pod)
		Expect(err).NotTo(HaveOccurred())

		err = waitJobReady(context, job)
		Expect(err).NotTo(HaveOccurred())

		waitPodReady(context, pod.Name, pod.Namespace)
		Expect(err).NotTo(HaveOccurred())

		url := fmt.Sprintf("%s-%s-0.%s", jobName, taskName, jobName)
		cmd := []string{"/bin/sh", "-c", fmt.Sprintf("curl --connect-timeout 5 http://%s:80 -o /dev/null -s -w '%s'", url, "%{http_code}")}

		// Test reachability from test to nginx <job name>-<task name>-0.<jobname>
		_, err = ExecCommandInContainer(context, pod.Namespace, pod.Name, pod.Spec.Containers[0].Name, cmd...)
		Expect(err.Error()).Should(ContainSubstring("terminated with exit code 28"))
	})
})
