/*
Copyright 2026 The Volcano Authors.

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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Dequeue E2E Test", func() {
	ginkgo.It("releases inqueue quota without immediately re-enqueuing the unschedulable job", func() {
		const queueName = "dequeue-queue"

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{queueName},
			CapabilityResource: map[string]corev1.ResourceList{queueName: e2eutil.CPU1Mem1},
			NodesNumLimit:      1,
			NodesResourceLimit: e2eutil.CPU2Mem2,
		})
		defer e2eutil.CleanupTestContext(ctx)

		impossibleAffinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchFields: []corev1.NodeSelectorRequirement{{
							Key:      e2eutil.NodeFieldSelectorKeyNodeName,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"dequeue-node-does-not-exist"},
						}},
					}},
				},
			},
		}

		stuckJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:  "dequeue-stuck",
			Queue: queueName,
			Tasks: []e2eutil.TaskSpec{{
				Img:      e2eutil.DefaultNginxImage,
				Req:      e2eutil.CPU1Mem1,
				Min:      1,
				Rep:      1,
				Affinity: impossibleAffinity,
			}},
		})
		stuckPodGroup := waitForJobPodGroup(ctx, stuckJob)
		gomega.Expect(e2eutil.WaitPodGroupPhase(
			ctx, stuckPodGroup, schedulingv1beta1.PodGroupInqueue,
		)).NotTo(gomega.HaveOccurred())

		eligibleJob := e2eutil.CreateJob(ctx, &e2eutil.JobSpec{
			Name:  "dequeue-eligible",
			Queue: queueName,
			Tasks: []e2eutil.TaskSpec{{
				Img: e2eutil.DefaultNginxImage,
				Req: e2eutil.CPU1Mem1,
				Min: 1,
				Rep: 1,
			}},
		})
		eligiblePodGroup := waitForJobPodGroup(ctx, eligibleJob)
		gomega.Expect(e2eutil.WaitPodGroupPhase(
			ctx, eligiblePodGroup, schedulingv1beta1.PodGroupPending,
		)).NotTo(gomega.HaveOccurred())
		gomega.Consistently(func() (schedulingv1beta1.PodGroupPhase, error) {
			pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(
				eligiblePodGroup.Namespace,
			).Get(context.TODO(), eligiblePodGroup.Name, metav1.GetOptions{})
			if err != nil {
				return "", err
			}
			return pg.Status.Phase, nil
		}, 3*time.Second, 200*time.Millisecond).Should(gomega.Equal(schedulingv1beta1.PodGroupPending))

		ginkgo.By("enabling dequeue as the final scheduler action")
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		err := cmc.ChangeBy(func(data map[string]string) (bool, map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, func(sc *e2eutil.SchedulerConfiguration) bool {
				for _, action := range strings.Split(sc.Actions, ",") {
					if strings.TrimSpace(action) == "dequeue" {
						return false
					}
				}
				sc.Actions = strings.TrimSpace(sc.Actions) + ", dequeue"
				return true
			})
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer cmc.UndoChanged()

		gomega.Expect(e2eutil.WaitPodGroupPhase(
			ctx, stuckPodGroup, schedulingv1beta1.PodGroupPending,
		)).NotTo(gomega.HaveOccurred())
		gomega.Expect(e2eutil.WaitJobReady(ctx, eligibleJob)).NotTo(gomega.HaveOccurred())

		stuckPodGroup, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(
			stuckPodGroup.Namespace,
		).Get(context.TODO(), stuckPodGroup.Name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(stuckPodGroup.Status.Phase).To(gomega.Equal(schedulingv1beta1.PodGroupPending))
	})
})

func waitForJobPodGroup(
	ctx *e2eutil.TestContext,
	job *batchv1alpha1.Job,
) *schedulingv1beta1.PodGroup {
	podGroupName := fmt.Sprintf("%s-%s", job.Name, job.UID)
	var podGroup *schedulingv1beta1.PodGroup
	gomega.Eventually(func() error {
		var err error
		podGroup, err = ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(
			context.TODO(), podGroupName, metav1.GetOptions{},
		)
		return err
	}, time.Minute, time.Second).Should(gomega.Succeed())
	return podGroup
}
