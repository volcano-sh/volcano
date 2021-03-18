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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	jobWaitingTime = "sla-waiting-time"
)

var _ = Describe("SLA Test", func() {
	It("sla permits job to be inqueue", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

		// job requests resources more than the whole cluster, but after sla-waiting-time, job can be inqueue
		// and create Pending pods
		job := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep * 2,
					rep: rep * 2,
				},
			},
		}

		annotations := map[string]string{jobWaitingTime: "3s"}

		job.name = "j1-overlarge"
		overlargeJob := createJobWithPodGroup(ctx, job, "", annotations)
		err := waitTaskPhase(ctx, overlargeJob, []v1.PodPhase{v1.PodPending}, int(rep*2))
		Expect(err).NotTo(HaveOccurred())
	})

	It("sla adjusts job order", func() {
		ctx := initTestContext(options{})
		defer cleanupTestContext(ctx)

		slot := oneCPU
		rep := clusterSize(ctx, slot)

		// job requests resources more than the whole cluster, but after sla-waiting-time, job can be inqueue
		// and create Pending pods
		job1 := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep * 2,
					rep: rep * 2,
				},
			},
		}
		job2 := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep,
					rep: rep,
				},
			},
		}

		job1.name = "j1-overlarge"
		overlargeJob := createJobWithPodGroup(ctx, job1, "", map[string]string{jobWaitingTime: "3s"})
		err := waitTaskPhase(ctx, overlargeJob, []v1.PodPhase{v1.PodPending}, int(rep*2))
		Expect(err).NotTo(HaveOccurred())

		job2.name = "j2-slow-sla"
		slowSlaJob := createJobWithPodGroup(ctx, job2, "", map[string]string{jobWaitingTime: "1h"})
		err = waitTaskPhase(ctx, slowSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		job2.name = "j3-fast-sla"
		fastSlaJob := createJobWithPodGroup(ctx, job2, "", map[string]string{jobWaitingTime: "30m"})
		err = waitTaskPhase(ctx, fastSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		err = ctx.vcclient.BatchV1alpha1().Jobs(getNS(ctx, job1)).Delete(context.TODO(), job1.name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = waitTaskPhase(ctx, slowSlaJob, []v1.PodPhase{v1.PodPending}, 0)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(ctx, fastSlaJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})
})
