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

package schedulingaction

import (
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	//v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Enqueue E2E Test", func() {
	ginkgo.It("allocate work even not config enqueue action", func() {
		ginkgo.By("remove action enqueue from configmap")
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
			actstring := ""
			if strings.Contains(sc.Actions, "enqueue") {
				acts := strings.Split(sc.Actions, ",")
				for i, act := range acts {
					acts[i] = strings.TrimSpace(act)
					if acts[i] != "enqueue" {
						actstring += acts[i] + ","
					}
				}
				actstring = strings.TrimRight(actstring, ",")
				sc.Actions = actstring
				return true
			}
			return false
		}
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, modifier)
		})
		defer cmc.UndoChanged()

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi")},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi")}
		slot2 := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1024Mi")}

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: 1,
					Rep: 1,
				},
			},
		}

		job.Name = "low"
		lowReqJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitJobReady(ctx, lowReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		job.Name = "high"
		job.Tasks[0].Req = slot2
		highReqJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitJobReady(ctx, highReqJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Scheduling gated task will not consume inqueue resources", func() {

		ns := "test-namespace"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Namespace:     ns,
			NodesNumLimit: 1,
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot1 := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot1)
		// j-gated needs all CPU resources in the cluster
		jobGated := &e2eutil.JobSpec{
			Namespace: ns,
			Name:      "j-gated",
			Tasks: []e2eutil.TaskSpec{
				{
					Img:      e2eutil.DefaultNginxImage,
					Req:      slot1,
					Rep:      rep,
					Min:      rep,
					SchGates: []corev1.PodSchedulingGate{{Name: "g1"}},
				},
			},
		}

		// job1 and job 2 each need half CPU resources of the cluster
		// beware of overcommit gates
		job := &e2eutil.JobSpec{
			Namespace: ns,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot1,
					Min: rep / 2,
					Rep: rep / 2,
				},
			},
		}

		jGated := e2eutil.CreateJob(ctx, jobGated)
		job.Name = "job1"
		j1 := e2eutil.CreateJob(ctx, job)
		job.Name = "job2"
		j2 := e2eutil.CreateJob(ctx, job)

		err := e2eutil.WaitTasksPending(ctx, jGated, int(rep))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		// Since all pods of j-gated are scheduling gated and does not take up resources
		// j1 and j2 can run normally
		err = e2eutil.WaitJobReady(ctx, j1)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = e2eutil.WaitJobReady(ctx, j2)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

	})

})
