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
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = Describe("Job E2E Test: Test Admission service", func() {

	It("Default queue would be added", func() {
		jobName := "job-default-queue"
		namespace := "test"
		context := initTestContext(options{})
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
		createdJob, err := context.vcclient.BatchV1alpha1().Jobs(namespace).Get(jobName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(createdJob.Spec.Queue).Should(Equal("default"),
			"Job queue attribute would default to 'default' ")
	})

	It("Invalid CPU unit", func() {

		context := initTestContext(options{})
		defer cleanupTestContext(context)
		namespace := "test"

		var job v1alpha1.Job
		jsonData := []byte(`{
   "apiVersion": "batch.volcano.sh/v1alpha1",
   "kind": "Job",
   "metadata": {
      "name": "test-job"
   },
   "spec": {
      "minAvailable": 3,
      "schedulerName": "volcano",
      "queue": "default",
      "tasks": [
         {
            "replicas": 3,
            "name": "default-nginx",
            "template": {
               "spec": {
                  "containers": [
                     {
                        "image": "nginx",
                        "imagePullPolicy": "IfNotPresent",
                        "name": "nginx",
                        "resources": {
                           "requests": {
                              "cpu": "-1"
                           }
                        }
                     }
                  ],
                  "restartPolicy": "Never"
               }
            }
         }
      ]
   }
}`)
		err := json.Unmarshal(jsonData, &job)
		Expect(err).NotTo(HaveOccurred())
		_, err = context.vcclient.BatchV1alpha1().Jobs(namespace).Create(&job)
		Expect(err).To(HaveOccurred())

	})

	It("Invalid memory unit", func() {

		context := initTestContext(options{})
		defer cleanupTestContext(context)
		namespace := "test"

		var job v1alpha1.Job
		jsonData := []byte(`{
   "apiVersion": "batch.volcano.sh/v1alpha1",
   "kind": "Job",
   "metadata": {
      "name": "test-job"
   },
   "spec": {
      "minAvailable": 3,
      "schedulerName": "volcano",
      "queue": "default",
      "tasks": [
         {
            "replicas": 3,
            "name": "default-nginx",
            "template": {
               "spec": {
                  "containers": [
                     {
                        "image": "nginx",
                        "imagePullPolicy": "IfNotPresent",
                        "name": "nginx",
                        "resources": {
                           "requests": {
                              "memory": "-1"
                           }
                        }
                     }
                  ],
                  "restartPolicy": "Never"
               }
            }
         }
      ]
   }
}`)

		err := json.Unmarshal(jsonData, &job)
		Expect(err).NotTo(HaveOccurred())
		_, err = context.vcclient.BatchV1alpha1().Jobs(namespace).Create(&job)
		Expect(err).To(HaveOccurred())

	})

	It("Create default-scheduler pod", func() {
		podName := "pod-default-scheduler"
		namespace := "test"
		context := initTestContext(options{})
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
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &thirtyCPU,
			},
			Status: schedulingv1beta1.PodGroupStatus{
				Phase: schedulingv1beta1.PodGroupPending,
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
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
			},
		}

		_, err := context.vcclient.SchedulingV1beta1().PodGroups(namespace).Create(pg)
		Expect(err).NotTo(HaveOccurred())

		_, err = context.kubeclient.CoreV1().Pods(namespace).Create(pod)
		Expect(err.Error()).Should(ContainSubstring(`failed to create pod <test/pod-volcano> as the podgroup phase is Pending`))
	})
})
