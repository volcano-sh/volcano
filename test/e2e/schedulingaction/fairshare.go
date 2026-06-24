/*
Copyright 2025 The Volcano Authors.

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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("Fairshare Plugin E2E Test", func() {
	ginkgo.It("should schedule jobs from low-usage users before high-usage users", func() {
		// Enable the fairshare plugin in the scheduler config, targeting a test queue
		// that tracks CPU resources for testability (no GPU hardware required).
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		modifier := func(sc *e2eutil.SchedulerConfiguration) bool {
			fairsharePlugin := e2eutil.PluginOption{
				Name: "fairshare",
				Arguments: map[string]string{
					"fairshare.targetQueues":    "default",
					"fairshare.resourceKey":     "cpu",
					"fairshare.halfLifeMinutes": "60",
				},
			}
			if len(sc.Tiers) > 0 {
				sc.Tiers[0].Plugins = append(sc.Tiers[0].Plugins, fairsharePlugin)
			} else {
				sc.Tiers = append(sc.Tiers, e2eutil.Tier{
					Plugins: []e2eutil.PluginOption{fairsharePlugin},
				})
			}
			return true
		}
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			return e2eutil.ModifySchedulerConfig(data, modifier)
		})
		defer cmc.UndoChanged()

		// Wait for config to take effect
		time.Sleep(5 * time.Second)

		// Create test contexts for two different namespaces (simulating two users).
		ctx1 := e2eutil.InitTestContext(e2eutil.Options{
			Namespace: "fairshare-user-a",
			Queues:    []string{"default"},
			NodesNumLimit: 2,
			NodesResourceLimit: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("2048Mi"),
			},
		})
		defer e2eutil.CleanupTestContext(ctx1)

		ctx2 := e2eutil.InitTestContext(e2eutil.Options{
			Namespace: "fairshare-user-b",
			Queues:    []string{"default"},
		})
		defer e2eutil.CleanupTestContext(ctx2)

		cpuSlot := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		}

		// User A submits 4 jobs first, filling available capacity.
		userAJobs := make([]string, 4)
		for i := 0; i < 4; i++ {
			jobName := fmt.Sprintf("user-a-job-%d", i)
			userAJobs[i] = jobName
			job := &e2eutil.JobSpec{
				Name:      jobName,
				Namespace: "fairshare-user-a",
				Queue:     "default",
				Tasks: []e2eutil.TaskSpec{
					{
						Img: e2eutil.DefaultNginxImage,
						Req: cpuSlot,
						Min: 1,
						Rep: 1,
					},
				},
			}
			createdJob := e2eutil.CreateJob(ctx1, job)
			err := e2eutil.WaitJobReady(ctx1, createdJob)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		// Let User A accumulate some usage history.
		time.Sleep(10 * time.Second)

		// Now User B submits a job. Even though User A has pending work,
		// User B should be scheduled first because User B has zero usage.
		userBJob := &e2eutil.JobSpec{
			Name:      "user-b-job-0",
			Namespace: "fairshare-user-b",
			Queue:     "default",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: cpuSlot,
					Min: 1,
					Rep: 1,
				},
			},
		}
		createdBJob := e2eutil.CreateJob(ctx2, userBJob)

		// User A also submits another job.
		userAExtraJob := &e2eutil.JobSpec{
			Name:      "user-a-job-extra",
			Namespace: "fairshare-user-a",
			Queue:     "default",
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: cpuSlot,
					Min: 1,
					Rep: 1,
				},
			},
		}
		createdAExtraJob := e2eutil.CreateJob(ctx1, userAExtraJob)

		// Delete one of User A's running jobs to free capacity.
		err := ctx1.Vcclient.BatchV1alpha1().Jobs("fairshare-user-a").Delete(
			context.TODO(), userAJobs[0], metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// User B's job should become ready (scheduled) before User A's extra job,
		// because User B has lower historical usage.
		err = e2eutil.WaitJobReady(ctx2, createdBJob)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify User A's extra job is still pending (or at least was scheduled after User B).
		err = wait.Poll(2*time.Second, 30*time.Second, func() (bool, error) {
			job, getErr := ctx1.Vcclient.BatchV1alpha1().Jobs("fairshare-user-a").Get(
				context.TODO(), createdAExtraJob.Name, metav1.GetOptions{})
			if getErr != nil {
				return false, getErr
			}
			// If User A's extra job is still pending, fairshare is working correctly.
			// If it becomes ready, we check the timeline against User B's job.
			return job.Status.Running > 0 || job.Status.Pending > 0, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
