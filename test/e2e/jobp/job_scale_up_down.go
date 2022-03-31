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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/job/plugins/svc"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Dynamic Job scale up and down", func() {
	It("Scale up", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "scale-up-job"
		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: jobName,
			Plugins: map[string][]string{
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "default",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Req:  e2eutil.HalfCPU,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// scale up
		job.Spec.MinAvailable = 4
		job.Spec.Tasks[0].Replicas = 4
		err = e2eutil.UpdateJob(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		pluginName := fmt.Sprintf("%s-svc", jobName)
		cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(ctx.Namespace).Get(context.TODO(),
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts := svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// TODO: check others

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Scale down", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "scale-down-job"
		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: jobName,
			Plugins: map[string][]string{
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "default",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Req:  e2eutil.HalfCPU,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// scale down
		var taskMinAvailable int32 = 1
		job.Spec.MinAvailable = 1
		job.Spec.Tasks[0].Replicas = 1
		job.Spec.Tasks[0].MinAvailable = &taskMinAvailable
		err = e2eutil.UpdateJob(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		pluginName := fmt.Sprintf("%s-svc", jobName)
		cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(ctx.Namespace).Get(context.TODO(),
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts := svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// TODO: check others

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Scale down to zero and scale up", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		jobName := "scale-down-job"
		By("create job")
		job := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name: jobName,
			Plugins: map[string][]string{
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "default",
					Img:  e2eutil.DefaultNginxImage,
					Min:  2,
					Rep:  2,
					Req:  e2eutil.HalfCPU,
				},
			},
		})

		// job phase: pending -> running
		err := e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// scale down
		var taskMinAvailable int32 = 0
		job.Spec.MinAvailable = 0
		job.Spec.Tasks[0].Replicas = 0
		job.Spec.Tasks[0].MinAvailable = &taskMinAvailable
		err = e2eutil.UpdateJob(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		pluginName := fmt.Sprintf("%s-svc", jobName)
		cm, err := ctx.Kubeclient.CoreV1().ConfigMaps(ctx.Namespace).Get(context.TODO(),
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts := svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// scale up
		job.Spec.MinAvailable = 2
		job.Spec.Tasks[0].Replicas = 2
		err = e2eutil.UpdateJob(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// wait for tasks scaled up
		err = e2eutil.WaitJobReady(ctx, job)
		Expect(err).NotTo(HaveOccurred())

		// check configmap updated
		cm, err = ctx.Kubeclient.CoreV1().ConfigMaps(ctx.Namespace).Get(context.TODO(),
			pluginName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		hosts = svc.GenerateHosts(job)
		Expect(hosts).To(Equal(cm.Data))

		// TODO: check others

		By("delete job")
		err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitJobCleanedUp(ctx, job)
		Expect(err).NotTo(HaveOccurred())
	})
})
