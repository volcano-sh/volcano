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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("TensorFlow Plugin E2E Test", func() {
	var testCtx *e2eutil.TestContext

	JustAfterEach(func() {
		e2eutil.DumpTestContextIfFailed(testCtx, CurrentSpecReport())
	})

	It("Will Start in pending state and goes through other phases to get complete phase", func() {
		testCtx = e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(testCtx)

		jobName := "tensorflow-dist-mnist"

		job := &vcbatch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: jobName,
			},
			Spec: vcbatch.JobSpec{
				MinAvailable:  int32(3),
				SchedulerName: e2eutil.SchedulerName,
				Plugins: map[string][]string{
					"tensorflow": {"--ps=ps", "--worker=worker", "--port=2222"},
				},
				Policies: []vcbatch.LifecyclePolicy{
					{
						Event:  vcbus.PodEvictedEvent,
						Action: vcbus.RestartJobAction,
					},
				},
				Tasks: []vcbatch.TaskSpec{
					{
						Replicas: int32(1),
						Name:     "ps",
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								RestartPolicy: v1.RestartPolicyNever,
								Containers: []v1.Container{
									{
										Command: []string{
											"sh",
											"-c",
											"python /var/tf_dist_mnist/dist_mnist.py --train_steps 1000",
										},
										Image: e2eutil.DefaultTFImage,
										Name:  "tensorflow",
										Ports: []v1.ContainerPort{
											{
												Name:          "tfjob-port",
												ContainerPort: int32(2222),
											},
										},
									},
								},
							},
						},
					},
					{
						Replicas: int32(2),
						Name:     "worker",
						Policies: []vcbatch.LifecyclePolicy{
							{
								Event:  vcbus.TaskCompletedEvent,
								Action: vcbus.CompleteJobAction,
							},
						},
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								RestartPolicy: v1.RestartPolicyNever,
								Containers: []v1.Container{
									{
										Command: []string{
											"sh",
											"-c",
											"python /var/tf_dist_mnist/dist_mnist.py --train_steps 1000",
										},
										Image: e2eutil.DefaultTFImage,
										Name:  "tensorflow",
										Ports: []v1.ContainerPort{
											{
												Name:          "tfjob-port",
												ContainerPort: int32(2222),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		created, err := testCtx.Vcclient.BatchV1alpha1().Jobs(testCtx.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobStates(testCtx, created, []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running, vcbatch.Completed}, e2eutil.FiveMinute)
		Expect(err).NotTo(HaveOccurred())
	})

})
