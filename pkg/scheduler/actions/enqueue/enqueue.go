/*
Copyright 2019 The Kubernetes Authors.

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

package enqueue

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (enqueue *Action) Name() string {
	return "enqueue"
}

func (enqueue *Action) Initialize() {}

func (enqueue *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Enqueue ...")
	defer klog.V(5).Infof("Leaving Enqueue ...")

	// 优先队列
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	// 存放本次调度 job 涉及到的队列，set 去重
	queueSet := sets.NewString()
	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	// 遍历所有作业，将作业加入到优先队列中
	for _, job := range ssn.Jobs {
		if job.ScheduleStartTimestamp.IsZero() {
			ssn.Jobs[job.UID].ScheduleStartTimestamp = metav1.Time{
				Time: time.Now(),
			}
		}
		if queue, found := ssn.Queues[job.Queue]; !found {
			klog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else if !queueSet.Has(string(queue.UID)) {
			// 新队列，加入队列集合
			klog.V(5).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)

			queueSet.Insert(string(queue.UID))
			queues.Push(queue)
		}

		// 含义：只有以下状态的作业才会被考虑入队：
		// 没有 PodGroup 的作业
		// PodGroup 状态为 Pending 的作业
		// PodGroup 状态为空的作业
		if job.IsPending() {
			if _, found := jobsMap[job.Queue]; !found {
				jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			klog.V(5).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
			jobsMap[job.Queue].Push(job)
		}
	}

	klog.V(3).Infof("Try to enqueue PodGroup to %d Queues", len(jobsMap))

	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		// skip the Queue that has no pending job
		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			continue // 该队列没有待处理作业
		}
		job := jobs.Pop().(*api.JobInfo)

		// 两个入队条件（满足其一即可）：
		// MinResources 为空：作业没有设置最小资源要求
		// 通常是 BestEffort 作业
		// 直接允许入队，不进行资源检查
		// 通过 JobEnqueueable 检查：调用插件注册的 ssn.jobEnqueueableFns 进行入队资格验证
		// 会触发各种插件的 jobEnqueueableFns
		// 例如：资源配额检查、容量检查、Gang 调度检查等
		if job.PodGroup.Spec.MinResources == nil || ssn.JobEnqueueable(job) {
			// 入队成功后的操作：
			// 调用 ssn.JobEnqueued(job)：通知插件作业已入队
			// 更新 PodGroup 状态为 PodGroupInqueue
			// 更新调度器缓存中的作业信息
			ssn.JobEnqueued(job)
			job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			ssn.Jobs[job.UID] = job
		}

		// Added Queue back until no job in Queue.
		// 每次只从队列 pop 1 个，所以如果队列还有剩余，需要重新放回去，直到队列空为止
		queues.Push(queue)
	}
}

func (enqueue *Action) UnInitialize() {}
