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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	kbv1 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
)

var _ = Describe("Job E2E Test: Test Admission service", func() {

	It("Default queue would be added", func() {
		jobName := "job-default-queue"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		_, err := createJobInner(context, &jobSpec{
			min:       1,
			namespace: namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: "taskname",
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		createdJob, err := context.vkclient.BatchV1alpha1().Jobs(namespace).Get(jobName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(createdJob.Spec.Queue).Should(Equal("default"),
			"Job queue attribute would default to 'default' ")
	})

	It("Create default-scheduler pod", func() {
		podName := "pod-default-scheduler"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
				Name:      podName,
			},
			Spec: corev1.PodSpec{
				Containers: createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
			},
		}

		_, err := context.kubeclient.CoreV1().Pods(namespace).Create(pod)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodPhase(context, pod, []corev1.PodPhase{corev1.PodRunning})
		Expect(err).NotTo(HaveOccurred())
	})

	It("Can't create volcano pod when podgroup is Pending", func() {
		podName := "pod-volcano"
		pgName := "pending-pg"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		pg := &kbv1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
				Name:      pgName,
			},
			Spec: kbv1.PodGroupSpec{
				MinMember:    1,
				MinResources: &thirtyCPU,
			},
			Status: kbv1.PodGroupStatus{
				Phase: kbv1.PodGroupPending,
			},
		}

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace:   namespace,
				Name:        podName,
				Annotations: map[string]string{kbv1.GroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
			},
		}

		_, err := context.kbclient.SchedulingV1alpha1().PodGroups(namespace).Create(pg)
		Expect(err).NotTo(HaveOccurred())

		_, err = context.kubeclient.CoreV1().Pods(namespace).Create(pod)
		Expect(err.Error()).Should(ContainSubstring(`Failed to create pod for pod <test/pending-pg>, because the podgroup phase is Pending`))
	})
})
