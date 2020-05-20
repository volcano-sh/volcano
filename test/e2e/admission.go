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

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

var _ = ginkgo.Describe("Job E2E Test: Test Admission service", func() {

	ginkgo.It("Default queue would be added", func() {
		jobName := "job-default-queue"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		_, err := createJobInner(ctx, &jobSpec{
			min:       1,
			namespace: ctx.namespace,
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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		createdJob, err := ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Get(jobName, v1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdJob.Spec.Queue).Should(gomega.Equal("default"),
			"Job queue attribute would default to 'default' ")
	})

	ginkgo.It("Invalid CPU unit", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Create(&job)
		gomega.Expect(err).To(gomega.HaveOccurred())

	})

	ginkgo.It("Invalid memory unit", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.vcclient.BatchV1alpha1().Jobs(ctx.namespace).Create(&job)
		gomega.Expect(err).To(gomega.HaveOccurred())

	})

	ginkgo.It("Create default-scheduler pod", func() {
		podName := "pod-default-scheduler"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.namespace,
				Name:      podName,
			},
			Spec: corev1.PodSpec{
				Containers: createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
			},
		}

		_, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Create(pod)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitPodPhase(ctx, pod, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Can't create volcano pod when podgroup is Pending", func() {
		podName := "pod-volcano"
		pgName := "pending-pg"
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.namespace,
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
				Namespace:   ctx.namespace,
				Name:        podName,
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    createContainers(defaultNginxImage, "", "", oneCPU, oneCPU, 0),
			},
		}

		_, err := ctx.vcclient.SchedulingV1beta1().PodGroups(ctx.namespace).Create(pg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.kubeclient.CoreV1().Pods(ctx.namespace).Create(pod)
		gomega.Expect(err.Error()).Should(gomega.ContainSubstring(`the podgroup phase is Pending`))
	})
})
