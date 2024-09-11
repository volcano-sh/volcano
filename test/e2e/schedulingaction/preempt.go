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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	highPriority        = "high-priority"
	middlePriority      = "middle-priority"
	lowPriority         = "low-priority"
	highPriorityValue   = 100
	middlePriorityValue = 50
	lowPriorityValue    = 10
)

var _ = Describe("Job E2E Test", func() {

	var ctx *e2eutil.TestContext
	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	It("schedule high priority job without preemption when resource is enough", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU

		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    slot,
					Min:    1,
					Rep:    1,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		job.Name = "preemptee-0"
		job.Pri = lowPriority
		preempteeJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "preemptor-0"
		job.Pri = highPriority
		preemptorJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, 1)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(ctx, preemptorJob, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	It("schedule high priority job with preemption when idle resource is NOT enough but preemptee resource is enough", func() {
		// Remove enqueue action first because it conflicts with preempt.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			vcScheConfStr, ok := data["volcano-scheduler-ci.conf"]
			Expect(ok).To(BeTrue())

			schedulerConf := &e2eutil.SchedulerConfiguration{}
			err := yaml.Unmarshal([]byte(vcScheConfStr), schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changed = true
			newActions := strings.TrimPrefix(schedulerConf.Actions, "enqueue, ")
			if newActions == schedulerConf.Actions {
				changed = false
				klog.Warning("There is already no enqueue action")
				return
			}

			schedulerConf.Actions = newActions
			newVCScheConfBytes, err := yaml.Marshal(schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changedBefore = make(map[string]string)
			changedBefore["volcano-scheduler-ci.conf"] = vcScheConfStr
			data["volcano-scheduler-ci.conf"] = string(newVCScheConfBytes)
			return
		})
		defer cmc.UndoChanged()

		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		job := &e2eutil.JobSpec{
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

		job.Name = "preemptee-1"
		job.Pri = lowPriority
		preempteeJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.Name = "preemptor-1"
		job.Pri = highPriority
		job.Min = rep / 2
		preemptorJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		err = e2eutil.WaitTasksReady(ctx, preemptorJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("preemption doesn't work when podgroup is pending due to insufficient resource", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		pgName := "pending-pg"
		pg := &schedulingv1beta1.PodGroup{
			ObjectMeta: v1.ObjectMeta{
				Namespace: ctx.Namespace,
				Name:      pgName,
			},
			Spec: schedulingv1beta1.PodGroupSpec{
				MinMember:    1,
				MinResources: &e2eutil.ThirtyCPU,
			},
		}
		_, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).Create(context.TODO(), pg, v1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img: e2eutil.DefaultNginxImage,
					Req: slot,
					Min: 1,
					Rep: rep,
				},
			},
		}
		job.Name = "preemptee-2"
		job.Pri = lowPriority
		preempteeJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{
			TypeMeta: v1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: v1.ObjectMeta{
				Namespace:   ctx.Namespace,
				Name:        "preemptor-pod",
				Annotations: map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName},
			},
			Spec: corev1.PodSpec{
				SchedulerName:     "volcano",
				Containers:        e2eutil.CreateContainers(e2eutil.DefaultNginxImage, "", "", e2eutil.OneCPU, e2eutil.OneCPU, 0),
				PriorityClassName: highPriority,
			},
		}
		// Pod is allowed to be created, preemption does not happen due to PodGroup is in pending state
		_, err = ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		// Make sure preempteeJob is not preempted as expected
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
	})

	It("preemption only works in the same queue", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{"q1-preemption", "q2-reference"},
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    slot,
					Min:    1,
					Rep:    rep / 2,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		job.Name = "j1-q1"
		job.Pri = lowPriority
		job.Queue = "q1-preemption"
		queue1Job := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, queue1Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "j2-q2"
		job.Pri = lowPriority
		job.Queue = "q2-reference"
		queue2Job := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "j3-q1"
		job.Pri = highPriority
		job.Queue = "q1-preemption"
		job.Tasks[0].Rep = rep
		queue1Job3 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, queue1Job3, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, queue1Job, 0)
		Expect(err).NotTo(HaveOccurred())
	})

	It("preemption doesn't work when total resource of idle resource and preemptee is NOT enough", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{"q1-preemption", "q2-reference"},
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)
		job := &e2eutil.JobSpec{
			Tasks: []e2eutil.TaskSpec{
				{
					Img:    e2eutil.DefaultNginxImage,
					Req:    slot,
					Min:    1,
					Rep:    1,
					Labels: map[string]string{schedulingv1beta1.PodPreemptable: "true"},
				},
			},
		}

		job.Name = "j1-q1"
		job.Pri = lowPriority
		job.Queue = "q1-preemption"
		queue1Job := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, queue1Job, 1)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "j2-q2"
		job.Pri = lowPriority
		job.Queue = "q2-reference"
		job.Tasks[0].Min = rep / 2
		job.Tasks[0].Rep = rep / 2
		queue2Job := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "j3-q1"
		job.Pri = highPriority
		job.Queue = "q1-preemption"
		job.Tasks[0].Min = rep
		job.Tasks[0].Rep = rep
		queue1Job3 := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, queue1Job3, int(rep))
		Expect(err).To(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, queue1Job, 1)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, queue2Job, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

	It("multi-preemptor-jobs who are in different priority", func() {
		Skip("https://github.com/volcano-sh/volcano/issues/911")
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{"q1-preemption"},
			PriorityClasses: map[string]int32{
				highPriority:   highPriorityValue,
				middlePriority: middlePriorityValue,
				lowPriority:    lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)
		job := &e2eutil.JobSpec{
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

		job.Name = "low-priority-job"
		job.Pri = lowPriority
		job.Queue = "q1-preemption"
		lowPriorityJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, lowPriorityJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.Name = "middle-prority-job"
		job.Pri = middlePriority
		job.Queue = "q1-preemption"
		job.Tasks[0].Rep = rep / 2
		job.Tasks[0].Min = rep / 2
		middlePriorityJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, middlePriorityJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, lowPriorityJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())

		job.Name = "high-priority-job"
		job.Pri = highPriority
		job.Queue = "q1-preemption"
		job.Tasks[0].Rep = rep
		job.Tasks[0].Min = rep
		highPriorityJob := e2eutil.CreateJob(ctx, job)
		err = e2eutil.WaitTasksReady(ctx, highPriorityJob, int(rep))
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, lowPriorityJob, 0)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, middlePriorityJob, 0)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Jobs unschedulable due to scheduling gates will not preempt other jobs despite sufficient preemptor", func() {
		// Remove enqueue action first because it conflicts with preempt.
		cmc := e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(func(data map[string]string) (changed bool, changedBefore map[string]string) {
			vcScheConfStr, ok := data["volcano-scheduler-ci.conf"]
			Expect(ok).To(BeTrue())

			schedulerConf := &e2eutil.SchedulerConfiguration{}
			err := yaml.Unmarshal([]byte(vcScheConfStr), schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changed = true
			newActions := strings.TrimPrefix(schedulerConf.Actions, "enqueue, ")
			if newActions == schedulerConf.Actions {
				changed = false
				klog.Warning("There is already no enqueue action")
				return
			}

			schedulerConf.Actions = newActions
			newVCScheConfBytes, err := yaml.Marshal(schedulerConf)
			Expect(err).NotTo(HaveOccurred())

			changedBefore = make(map[string]string)
			changedBefore["volcano-scheduler-ci.conf"] = vcScheConfStr
			data["volcano-scheduler-ci.conf"] = string(newVCScheConfBytes)
			return
		})
		defer cmc.UndoChanged()

		ctx = e2eutil.InitTestContext(e2eutil.Options{
			PriorityClasses: map[string]int32{
				highPriority: highPriorityValue,
				lowPriority:  lowPriorityValue,
			},
		})

		slot := e2eutil.OneCPU
		rep := e2eutil.ClusterSize(ctx, slot)

		job := &e2eutil.JobSpec{
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

		job.Name = "preemptee-1"
		job.Pri = lowPriority
		preempteeJob := e2eutil.CreateJob(ctx, job)
		err := e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		job.Name = "preemptor-1"
		job.Pri = highPriority
		job.Min = rep / 2
		preemptorJob := e2eutil.CreateJob(ctx, job)
		// All pods of preemptorJob will be created and remain in pending state
		err = e2eutil.WaitTasksPending(ctx, preemptorJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		// None of the tasks of preemptee should be evicted
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep))
		Expect(err).NotTo(HaveOccurred())

		// remove gate
		err = e2eutil.RemovePodSchGates(ctx, preemptorJob)
		Expect(err).NotTo(HaveOccurred())

		// half jobs of preemptee will be evicted to make space for preemptorJob
		err = e2eutil.WaitTasksReady(ctx, preempteeJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, preemptorJob, int(rep)/2)
		Expect(err).NotTo(HaveOccurred())
	})

})
