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
)

var _ = Describe("PG E2E Test: Test PG controller", func() {
	It("Create volcano rc, pg controller process", func() {
		rcName := "rc-volcano"
		podName := "pod-volcano"
		namespace := "test"
		label := map[string]string{"schedulerName": "volcano"}
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		rc := &corev1.ReplicationController{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ReplicationController",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:      rcName,
				Namespace: namespace,
			},
			Spec: corev1.ReplicationControllerSpec{
				Selector: label,
				Template: &corev1.PodTemplateSpec{
					ObjectMeta: v1.ObjectMeta{
						Name:   podName,
						Labels: label,
					},
					Spec: corev1.PodSpec{
						SchedulerName: "volcano",
						Containers: []corev1.Container{
							{
								Name:            podName,
								Image:           defaultNginxImage,
								ImagePullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
			},
		}

		_, err := context.kubeclient.CoreV1().ReplicationControllers(namespace).Create(rc)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodPhase(context, pod, []corev1.PodPhase{corev1.PodRunning})
		Expect(err).NotTo(HaveOccurred())

		ready, err := pgIsReady(context, namespace)
		Expect(ready).Should(Equal(true))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Create default-scheduler rc, pg controller don't process", func() {
		rcName := "rc-default-scheduler"
		podName := "pod-default-scheduler"
		namespace := "test"
		label := map[string]string{"a": "b"}
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		rc := &corev1.ReplicationController{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ReplicationController",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:      rcName,
				Namespace: namespace,
			},
			Spec: corev1.ReplicationControllerSpec{
				Selector: label,
				Template: &corev1.PodTemplateSpec{
					ObjectMeta: v1.ObjectMeta{
						Name:   podName,
						Labels: label,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            podName,
								Image:           defaultNginxImage,
								ImagePullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Namespace: namespace,
			},
		}

		_, err := context.kubeclient.CoreV1().ReplicationControllers(namespace).Create(rc)
		Expect(err).NotTo(HaveOccurred())

		err = waitPodPhase(context, pod, []corev1.PodPhase{corev1.PodRunning})
		Expect(err).NotTo(HaveOccurred())

		ready, err := pgIsReady(context, namespace)
		Expect(ready).Should(Equal(false))
		Expect(err.Error()).Should(Equal("podgroup is not found"))
	})
})
