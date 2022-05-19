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

package jobp

import (
	"context"
	"encoding/json"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Job E2E Test: Test Admission service", func() {

	ginkgo.It("Default queue would be added", func() {
		jobName := "job-default-queue"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		_, err := e2eutil.CreateJobInner(ctx, &e2eutil.JobSpec{
			Min:       1,
			Namespace: ctx.Namespace,
			Name:      jobName,
			Tasks: []e2eutil.TaskSpec{
				{
					Img:  e2eutil.DefaultNginxImage,
					Req:  e2eutil.OneCPU,
					Min:  1,
					Rep:  1,
					Name: "taskname",
				},
			},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		createdJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Get(context.TODO(), jobName, v1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdJob.Spec.Queue).Should(gomega.Equal("default"),
			"Job queue attribute would default to 'default' ")
	})

	ginkgo.It("Invalid CPU unit", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())

	})

	ginkgo.It("Invalid memory unit", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())

	})

	ginkgo.It("Create default-scheduler pod", func() {
		podName := "pod-default-scheduler"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      podName,
			},
			Spec: corev1.PodSpec{
				Containers: e2eutil.CreateContainers(e2eutil.DefaultNginxImage, "", "", e2eutil.OneCPU, e2eutil.OneCPU, 0),
			},
		}

		_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = e2eutil.WaitPodPhase(ctx, pod, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Create volcano-scheduler pod", func() {
		podName := "pod-volcano-scheduler"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      podName,
			},
			Spec: corev1.PodSpec{
				Containers:    e2eutil.CreateContainers(e2eutil.DefaultNginxImage, "", "", e2eutil.OneCPU, e2eutil.OneCPU, 0),
				SchedulerName: "volcano",
			},
		}

		_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = e2eutil.WaitPodPhase(ctx, pod, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Create volcano pod with volcano scheduler", func() {
		podName := "volcano-pod"
		pgName := "running-pg"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &e2eutil.OneCPU,
			},
		}

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace:   ctx.Namespace,
				Name:        podName,
				Annotations: map[string]string{vcschedulingv1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				Containers:    e2eutil.CreateContainers(e2eutil.DefaultNginxImage, "", "", e2eutil.HalfCPU, e2eutil.HalfCPU, 0),
				SchedulerName: "volcano",
			},
		}

		podGroup, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), pg, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = e2eutil.WaitPodGroupPhase(ctx, podGroup, schedulingv1beta1.PodGroupInqueue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = e2eutil.WaitPodPhase(ctx, pod, []corev1.PodPhase{corev1.PodRunning})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Allow to create pod when podgroup is Pending", func() {
		podName := "pod-volcano"
		pgName := "pending-pg"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &e2eutil.ThirtyCPU,
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
				Namespace:   ctx.Namespace,
				Name:        podName,
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName: "volcano",
				Containers:    e2eutil.CreateContainers(e2eutil.DefaultNginxImage, "", "", e2eutil.OneCPU, e2eutil.OneCPU, 0),
			},
		}

		_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), pg, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Job mutate check", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
   			"apiVersion": "batch.volcano.sh/v1alpha1",
   			"kind": "Job",
			"metadata": {
				"name": "test-job"
			},
			"spec": {
				"minAvailable": 1,
				"tasks": [
					{
						"replicas": 1,
						"template": {
							"spec": {
								"containers": [
									{
										"image": "nginx",
										"imagePullPolicy": "IfNotPresent",
										"name": "nginx",
										"resources": {
											"requests": {
												"cpu": "1"
											}
										}
									}
								],
								"restartPolicy": "Never"
							}
						}
					},
					{
						"replicas": 1,
						"template": {
							"spec": {
								"containers": [
									{
										"image": "busybox:1.24",
										"imagePullPolicy": "IfNotPresent",
										"name": "busybox",
										"resources": {
											"requests": {
												"cpu": "1"
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
		testJob, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(testJob.Spec.Queue).Should(gomega.Equal("default"), "Job queue attribute would default to 'default' ")
		gomega.Expect(testJob.Spec.SchedulerName).Should(gomega.Equal("volcano"), "Job scheduler wolud default to 'volcano'")
		gomega.Expect(testJob.Spec.Tasks[0].Name).Should(gomega.Equal("default0"), "task[0].name wolud default to 'default0'")
		gomega.Expect(testJob.Spec.Tasks[1].Name).Should(gomega.Equal("default1"), "task[1].name wolud default to 'default1'")
	})

	ginkgo.It("job validate check: duplicate task name check when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
   			"apiVersion": "batch.volcano.sh/v1alpha1",
   			"kind": "Job",
			"metadata": {
				"name": "test-job"
			},
			"spec": {
				"minAvailable": 1,
				"tasks": [
					{
						"name": "test",
						"replicas": 1,
						"template": {
							"spec": {
								"containers": [
									{
										"image": "nginx",
										"imagePullPolicy": "IfNotPresent",
										"name": "nginx",
										"resources": {
											"requests": {
												"cpu": "1"
											}
										}
									}
								],
								"restartPolicy": "Never"
							}
						}
					},
					{
						"name": "test",
						"replicas": 1,
						"template": {
							"spec": {
								"containers": [
									{
										"image": "busybox:1.24",
										"imagePullPolicy": "IfNotPresent",
										"name": "busybox",
										"resources": {
											"requests": {
												"cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: duplicate job policy event when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 1,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "policies": [
				 {
					 "event": "PodFailed",
					 "action": "AbortJob"
				 },
				 {
					"event": "PodFailed",
					"action": "RestartJob"
				}
			 ]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: minAvailable larger than replicas when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 2,
			 "tasks": [
				 {
					 "replicas": 1,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal plugin when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "plugins": {
				 "big_plugin": []
			 }
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal ttl when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "ttlSecondsAfterFinished": -1
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal minAvailable when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": -1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal maxRetry when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "maxRetry": -1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: no task spec when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks":[]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal replicas when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": -1,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: illegal task name when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "name": "Task-1",
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: policy event with exit code when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"event": "PodFailed",
					"action": "AbortJob",
					"exitCode": -1
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: policy event and exit code both nil when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"action": "AbortJob"
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: invalid policy event when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"event": "fakeEvent",
					"action": "AbortJob"
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: invalid action when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"event": "PodEvicted",
					"action": "fakeAction"
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: zero exitCode when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"action": "AbortJob",
					"exitCode": 0
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: both any event and other events exist when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			"policies": [
				{
					"event": "*",
					"action": "AbortJob"
				},
				{
					"event": "PodFailed",
					"action": "RestartJob"
				}
			]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: invalid volume mount when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "volumes": [
				 {
					 "mountPath": ""
				 }
			 ]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: duplicate mount volume when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "volumes": [
				 {
					 "mountPath": "/var",
					 "volumeClaimName": "pvc1"
				 },
				 {
					"mountPath": "/var",
					"volumeClaimName": "pvc2"
				}
			 ]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: vloume without volumeClaimName and volumeClaim when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "volumes": [
				 {
					 "mountPath": "/var"
				 }
			 ]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: vloume without volumeClaimName and volumeClaim when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 }
								 }
							 ],
							 "restartPolicy": "Never"
						 }
					 }
				 }
			 ],
			 "volumes": [
				 {
					 "mountPath": "/var"
				 }
			 ]
		 }
	 }`)
		err := json.Unmarshal(jsonData, &job)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: invalid queue when create", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "queue": "fakeQueue",
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("job validate check: create job with priviledged container", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		var job v1alpha1.Job
		jsonData := []byte(`{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind": "Job",
		 	"metadata": {
			"name": "test-job"
		 },
		 "spec": {
			 "minAvailable": 1,
			 "tasks": [
				 {
					 "replicas": 2,
					 "template": {
						 "spec": {
							 "containers": [
								 {
									 "image": "nginx",
									 "imagePullPolicy": "IfNotPresent",
									 "name": "nginx",
									 "resources": {
										 "requests": {
											 "cpu": "1"
										 }
									 },
									 "securityContext": {
										 "privileged": true
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
		_, err = ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).Create(context.TODO(), &job, v1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("queue check: open queue can be deleted", func() {
		queueName := "deleted-open-queue"
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: queueName,
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
			Status: schedulingv1beta1.QueueStatus{
				State: schedulingv1beta1.QueueStateOpen,
			},
		}

		_, err := ctx.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = ctx.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), queue.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
