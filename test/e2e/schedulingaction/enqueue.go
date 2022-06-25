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
	"k8s.io/apimachinery/pkg/api/resource"

	"gopkg.in/yaml.v2"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Enqueue E2E Test", func() {
	ginkgo.It("allocate work even not config enqueue action", func() {
		ginkgo.By("remove action enqueue from configmap")
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			vcScheConfStr, ok := data["volcano-scheduler-ci.conf"]
			gomega.Expect(ok).To(gomega.BeTrue())

			schedulerConf := &e2eutil.SchedulerConfiguration{}
			err := yaml.Unmarshal([]byte(vcScheConfStr), schedulerConf)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			actstring := ""
			if strings.Contains(schedulerConf.Actions, "enqueue") {
				acts := strings.Split(schedulerConf.Actions, ",")
				for i, act := range acts {
					acts[i] = strings.TrimSpace(act)
					if acts[i] != "enqueue" {
						actstring += acts[i] + ","
					}
				}
				actstring = strings.TrimRight(actstring, ",")
				schedulerConf.Actions = actstring
			}

			newVCScheConfBytes, err := yaml.Marshal(schedulerConf)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			changed = true
			changedBefore = make(map[string]string)
			changedBefore["volcano-scheduler-ci.conf"] = vcScheConfStr
			data["volcano-scheduler-ci.conf"] = string(newVCScheConfBytes)
			return
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
})
