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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Reclaim E2E Test", func() {

	CreateReclaimJob := func(ctx *e2eutil.TestContext, req v1.ResourceList, name string, queue string, pri string, nodeName string, waitTaskReady bool) (*batchv1alpha1.Job, error) {
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    req,
					Min:    1,
					Rep:    1,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
			Name:     name,
			Queue:    queue,
			NodeName: nodeName,
		}
		if pri != "" {
			job.Pri = pri
		}
		batchJob, err := e2eutil.CreateJobInner(ctx, job)
		if err != nil {
			return nil, err
		}
		if waitTaskReady {
			err = e2eutil.WaitTasksReady(ctx, batchJob, 1)
		}
		return batchJob, err
	}

	WaitQueueStatus := func(ctx *e2eutil.TestContext, status string, num int32, queue string) error {
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queue, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Get queue %s failed", queue)
			switch status {
			case "Open":
				return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
			default:
				return false, nil
			}
		})
		return err
	}

	CheckPodGroupStatistics := func(ctx *e2eutil.TestContext, status string, num int, queue string) error {
		err := e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(ctx, ctx.Namespace, queue)
			switch status {
			case "Running":
				return pgStats.Running == num, nil
			case "Pending":
				return pgStats.Pending == num, nil
			case "Inqueue":
				return pgStats.Inqueue == num, nil
			default:
				return false, nil
			}
		})
		return err
	}

	It("Reclaim Case 1: New queue with job created no reclaim when resource is enough", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"
		ctx.Queues = append(ctx.Queues, q3)
		e2eutil.CreateQueues(ctx)

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

	})

	It("Reclaim Case 3: New queue with job created no reclaim when job.PodGroup.Status.Phase pending", func() {
		Skip("Occasionally Failed E2E Test Cases for Claim, See issue: #3562")
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		j1 := "reclaim-j1"
		j2 := "reclaim-j2"
		j3 := "reclaim-j3"

		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, j1, q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, j2, q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"
		ctx.Queues = append(ctx.Queues, q3)
		e2eutil.CreateQueues(ctx)

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, j3, q3, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		// delete pod of job3 to make sure reclaim-j3 podgroup is pending
		listOptions := metav1.ListOptions{
			LabelSelector: labels.Set(map[string]string{batchv1alpha1.JobNameKey: j3}).String(),
		}

		job3pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), listOptions)
		Expect(err).NotTo(HaveOccurred(), "Get %s pod failed", j3)

		By("Make sure q1 q2 with job running in it.")
		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		for _, pod := range job3pods.Items {
			err = ctx.Kubeclient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", pod.Name)
		}

		By("Q3 pending when we delete it.")
		err = CheckPodGroupStatistics(ctx, "Pending", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue pending")
	})

	It("Reclaim Case 4: New queue with job created no reclaim when new queue is not created", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming job")
		q3 := "reclaim-q3"

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "", "", true)
		Expect(err).Should(HaveOccurred(), "job3 create failed when queue3 is not created")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
	})

	// As we agreed, this is not intended behavior, actually, it is a bug.
	It("Reclaim Case 5: New queue with job created no reclaim when job or task is low-priority", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "low-priority", "", true)
		Expect(err).Should(HaveOccurred(), "job3 create failed when queue3 is not created")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")
	})

	It("Reclaim Case 6: New queue with job created no reclaim when overused", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		q3 := "reclaim-q3"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2, q3},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		By("Create job4 to testing overused cases.")
		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j4", q3, "", "", false)
		Expect(err).NotTo(HaveOccurred(), "Wait for job4 failed")

		time.Sleep(10 * time.Second)
		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Inqueue", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Inqueue")
	})

	It("Reclaim Case 7:  New queue with job created no reclaim when job not satisfied with predicates", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"
		ctx.Queues = append(ctx.Queues, q3)
		e2eutil.CreateQueues(ctx)

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "", "fake-node", false)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		time.Sleep(10 * time.Second)
		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		// TODO: it is a bug : the job status is pending but podgroup status is running
		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Running")

	})

	It("Reclaim Case 8: New queue with job created no reclaim when task resources less than reclaimable resource", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"
		ctx.Queues = append(ctx.Queues, q3)
		e2eutil.CreateQueues(ctx)

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: e2eutil.CPU4Mem4,
					Min: 1,
					Rep: 1,
				},
			},
			Name:  "reclaim-j4",
			Queue: q3,
		}
		e2eutil.CreateJob(ctx, job)

		time.Sleep(10 * time.Second)
		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Inqueue", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue Inqueue")
	})

	It("Reclaim Case 9:  New queue with job created, all queues.spec.reclaimable is false, no reclaim", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2},
			NodesNumLimit:      3,
			NodesResourceLimit: e2eutil.CPU1Mem1,
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		_, err := CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j1", q1, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job1 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j2", q2, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job2 failed")

		By("Create new coming queue and job")
		q3 := "reclaim-q3"
		ctx.Queues = append(ctx.Queues, q3)
		e2eutil.CreateQueues(ctx)

		e2eutil.SetQueueReclaimable(ctx, []string{q1, q2}, false)
		defer e2eutil.SetQueueReclaimable(ctx, []string{q1}, true)

		err = WaitQueueStatus(ctx, "Open", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue open")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue running")

	})

	// Reclaim rely on priority is a bug here.
	It("Reclaim Case 10: Multi reclaimed queue", func() {
		q1 := e2eutil.DefaultQueue
		q2 := "reclaim-q2"
		q3 := "reclaim-q3"
		q4 := "reclaim-q4"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q2, q3, q4},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		spec := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    e2eutil.CPU1Mem1,
					Min:    1,
					Rep:    2,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		spec.Name = "reclaim-j1"
		spec.Queue = q1
		spec.Pri = "low-priority"
		job1 := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		spec.Name = "reclaim-j2"
		spec.Queue = q2
		spec.Pri = "low-priority"
		job2 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue1 running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue2 running")

		By("Create coming jobs")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q3, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j4", q4, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job4 failed")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue3 running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q4)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue4 running")

	})

	// Reclaim for capacity plugin.
	It("Capacity Reclaim Case 11: Multi reclaimed queue", func() {
		// First replace proportion with capacity plugin.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			vcScheConfStr, ok := data["volcano-scheduler-ci.conf"]
			Expect(ok).To(BeTrue())

			schedulerConf := &e2eutil.SchedulerConfiguration{}
			err := yaml.Unmarshal([]byte(vcScheConfStr), schedulerConf)
			Expect(err).NotTo(HaveOccurred())
			for _, tier := range schedulerConf.Tiers {
				for i, plugin := range tier.Plugins {
					if plugin.Name == "proportion" {
						tier.Plugins[i].Name = "capacity"
						break
					}
				}
			}

			newVCScheConfBytes, err := yaml.Marshal(schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changed = true
			changedBefore = make(map[string]string)
			changedBefore["volcano-scheduler-ci.conf"] = vcScheConfStr
			data["volcano-scheduler-ci.conf"] = string(newVCScheConfBytes)
			return
		})
		defer cmc.UndoChanged()

		q1 := "reclaim-q1"
		q2 := "reclaim-q2"
		q3 := "reclaim-q3"
		q4 := "reclaim-q4"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{q1, q2, q3, q4},
			NodesNumLimit: 4,
			DeservedResource: map[string]v1.ResourceList{
				q1: {v1.ResourceCPU: *resource.NewQuantity(1, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(1024*1024*1024, resource.BinarySI)},
				q2: {v1.ResourceCPU: *resource.NewQuantity(1, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(1024*1024*1024, resource.BinarySI)},
				q3: {v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)},
				q4: {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI)},
			},
			NodesResourceLimit: e2eutil.CPU2Mem2,
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})

		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		spec := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    e2eutil.CPU1Mem1,
					Min:    1,
					Rep:    4,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		spec.Name = "reclaim-j1"
		spec.Queue = q1
		spec.Pri = "low-priority"
		job1 := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		spec.Name = "reclaim-j2"
		spec.Queue = q2
		spec.Pri = "low-priority"
		job2 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = CheckPodGroupStatistics(ctx, "Running", 1, q1)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue1 running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue2 running")

		By("Create coming jobs")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU2Mem2, "reclaim-j3", q3, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU2Mem2, "reclaim-j4", q4, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job4 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j5", q4, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job5 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j6", q4, "high-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job6 failed")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q3)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue3 running")

		err = CheckPodGroupStatistics(ctx, "Running", 3, q4)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue4 running")

	})

	// Reclaim for hierarchical capacity plugin.
	It("Capacity Reclaim Case 12: Multi reclaimed queue", func() {
		// First replace configmap with hierarchical capacity plugin.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			vcScheConfStr, ok := data["volcano-scheduler-ci.conf"]
			Expect(ok).To(BeTrue())

			schedulerConf := &e2eutil.SchedulerConfiguration{}
			err := yaml.Unmarshal([]byte(vcScheConfStr), schedulerConf)
			Expect(err).NotTo(HaveOccurred())
			trueValue := true
			schedulerConf.Tiers = []e2eutil.Tier{
				{
					Plugins: []e2eutil.PluginOption{
						{
							Name:               "capacity",
							EnabledHierarchy:   &trueValue,
							EnabledJobOrder:    &trueValue,
							EnabledReclaimable: &trueValue,
							EnabledQueueOrder:  &trueValue,
						},
					},
				},
			}
			newVCScheConfBytes, err := yaml.Marshal(schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changed = true
			changedBefore = make(map[string]string)
			changedBefore["volcano-scheduler-ci.conf"] = vcScheConfStr
			data["volcano-scheduler-ci.conf"] = string(newVCScheConfBytes)
			return
		})
		defer cmc.UndoChanged()

		q1 := "reclaim-q1"
		q2 := "reclaim-q2"
		q11 := "reclaim-q11"
		q12 := "reclaim-q12"
		rootQueue := "root"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues:        []string{q1, q2, q11, q12},
			NodesNumLimit: 4,
			DeservedResource: map[string]v1.ResourceList{
				q1:  {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI)},
				q2:  {v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI)},
				q11: {v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)},
				q12: {v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI), v1.ResourceMemory: *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)},
			},
			QueueParent: map[string]string{
				q1:  rootQueue,
				q2:  rootQueue,
				q11: q1,
				q12: q1,
			},
			NodesResourceLimit: e2eutil.CPU2Mem2,
			PriorityClasses: map[string]int32{
				"low-priority": 10,
			},
		})

		ctx.Queues = []string{q11, q12, q1, q2}
		defer e2eutil.CleanupTestContext(ctx)

		By("Setup initial jobs")

		spec := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    e2eutil.CPU2Mem2,
					Min:    1,
					Rep:    1,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		spec.Name = "reclaim-j1"
		spec.Queue = q2
		spec.Pri = "low-priority"
		job1 := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		spec.Name = "reclaim-j2"
		spec.Queue = q11
		spec.Pri = "low-priority"
		job2 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobReady(ctx, job2)
		Expect(err).NotTo(HaveOccurred())

		err = CheckPodGroupStatistics(ctx, "Running", 1, q2)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue11 running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q11)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue2 running")

		By("Create coming jobs")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j3", q12, "low-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job3 failed")

		_, err = CreateReclaimJob(ctx, e2eutil.CPU1Mem1, "reclaim-j4", q11, "low-priority", "", true)
		Expect(err).NotTo(HaveOccurred(), "Wait for job4 failed")

		By("Make sure all job running")

		err = CheckPodGroupStatistics(ctx, "Running", 1, q12)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue12 running")

		err = CheckPodGroupStatistics(ctx, "Running", 2, q11)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for queue11 running")
	})

	It("Reclaim", func() {
		Skip("skip: the case has some problem")
		q1, q2 := "reclaim-q1", "reclaim-q2"
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{q1, q2},
			PriorityClasses: map[string]int32{
				"low-priority":  10,
				"high-priority": 10000,
			},
		})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		spec := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    slot,
					Min:    1,
					Rep:    rep,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		spec.Name = "q1-qj-1"
		spec.Queue = q1
		spec.Pri = "low-priority"
		job1 := e2eutil.CreateJob(ctx, spec)
		err := e2eutil.WaitJobReady(ctx, job1)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(ctx, ctx.Namespace, q1)
			return pgStats.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		spec.Name = "q2-qj-2"
		spec.Queue = q2
		spec.Pri = "high-priority"
		job2 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitTasksReady(ctx, job2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(ctx, job1, expected)
		Expect(err).NotTo(HaveOccurred())

		// Test Queue status
		spec = &e2eutil.JobSpec{
			Name:  "q1-qj-2",
			Queue: q1,
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: rep * 2,
					Rep: rep * 2,
				},
			},
		}
		job3 := e2eutil.CreateJob(ctx, spec)
		err = e2eutil.WaitJobStatePending(ctx, job3)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitQueueStatus(func() (bool, error) {
			pgStats := e2eutil.GetPodGroupStatistics(ctx, ctx.Namespace, q1)
			return pgStats.Pending == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
