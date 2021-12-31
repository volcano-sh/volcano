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

package schedulingbase

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	jobWaitingTime = "sla-waiting-time"
)

var _ = Describe("SLA Test", func() {
	It("sla permits job to be inqueue", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		// job requests resources more than the whole cluster, but after sla-waiting-time, job can be inqueue
		// and create Pending pods
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep * 2,
					Rep: rep * 2,
				},
			},
		}

		annotations := map[string]string{jobWaitingTime: "3s"}

		job.Name = "j1-overlarge"
		overlargeJob := e2eutil.CreateJobWithPodGroup(ctx, job, "", annotations)
		err := e2eutil.WaitTaskPhase(ctx, overlargeJob, []v1.PodPhase{v1.PodPending}, int(rep*2))
		Expect(err).NotTo(HaveOccurred())
	})

	It("sla adjusts job order", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		// job requests resources more than the whole cluster, but after sla-waiting-time, job can be inqueue
		// and create Pending pods
		job1 := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep * 2,
					Rep: rep * 2,
				},
			},
		}
		job2 := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep,
					Rep: rep,
				},
			},
		}

		job1.Name = "j1-overlarge"
		overlargeJob := e2eutil.CreateJobWithPodGroup(ctx, job1, "", map[string]string{jobWaitingTime: "3s"})
		err := e2eutil.WaitTaskPhase(ctx, overlargeJob, []v1.PodPhase{v1.PodPending}, int(rep*2))
		Expect(err).NotTo(HaveOccurred())

		job2.Name = "j2-slow-sla"
		slowSlaJob := e2eutil.CreateJobWithPodGroup(ctx, job2, "", map[string]string{jobWaitingTime: "1h"})
		err = e2eutil.WaitTaskPhase(ctx, slowSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		job2.Name = "j3-fast-sla"
		fastSlaJob := e2eutil.CreateJobWithPodGroup(ctx, job2, "", map[string]string{jobWaitingTime: "30m"})
		err = e2eutil.WaitTaskPhase(ctx, fastSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		err = ctx.Vcclient.BatchV1alpha1().Jobs(e2eutil.Namespace(ctx, job1)).Delete(context.TODO(), job1.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTaskPhase(ctx, slowSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(ctx, fastSlaJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})
})
