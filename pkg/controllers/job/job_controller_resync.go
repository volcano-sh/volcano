/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
)

func newRateLimitingQueue() workqueue.TypedRateLimitingInterface[any] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedMaxOfRateLimiter[any](
		workqueue.NewTypedItemExponentialFailureRateLimiter[any](5*time.Millisecond, 180*time.Second),
		// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.TypedBucketRateLimiter[any]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	))
}

func (cc *jobcontroller) processResyncTask() {
	obj, shutdown := cc.errTasks.Get()
	if shutdown {
		return
	}

	defer cc.errTasks.Done(obj)

	// one task only resync 10 times
	if cc.errTasks.NumRequeues(obj) > 10 {
		cc.errTasks.Forget(obj)
		return
	}

	task, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("failed to convert %v to *v1.Pod", obj)
		return
	}

	if err := cc.syncTask(task); err != nil {
		klog.Errorf("Failed to sync pod <%v/%v>, retry it, err %v", task.Namespace, task.Name, err)
		cc.resyncTask(task)
	} else {
		cc.errTasks.Forget(obj)
	}
}

func (cc *jobcontroller) syncTask(oldTask *v1.Pod) error {
	owner := metav1.GetControllerOf(oldTask)
	if owner == nil {
		return nil
	}
	req := apis.Request{Namespace: oldTask.Namespace, JobName: oldTask.Annotations[batch.JobNameKey], JobUid: owner.UID, Event: bus.OutOfSyncEvent}
	cc.getWorkerQueue(jobhelpers.GetJobKeyByReq(&req)).AddAfter(req, 2*time.Second)
	return nil
}

func (cc *jobcontroller) resyncTask(task *v1.Pod) {
	cc.errTasks.AddRateLimited(task)
}
