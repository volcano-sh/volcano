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
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

func newRateLimitingQueue() workqueue.RateLimitingInterface {
	return workqueue.NewRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 180*time.Second),
		// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	))
}

func (cc *jobcontroller) processResyncTask() {
	obj, shutdown := cc.errTasks.Get()
	if shutdown {
		return
	}

	// one task only resync 10 times
	if cc.errTasks.NumRequeues(obj) > 10 {
		cc.errTasks.Forget(obj)
		return
	}

	defer cc.errTasks.Done(obj)

	task, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("failed to convert %v to *v1.Pod", obj)
		return
	}

	if err := cc.syncTask(task); err != nil {
		klog.Errorf("Failed to sync pod <%v/%v>, retry it, err %v", task.Namespace, task.Name, err)
		cc.resyncTask(task)
	}
}

func (cc *jobcontroller) syncTask(oldTask *v1.Pod) error {
	newPod, err := cc.kubeClient.CoreV1().Pods(oldTask.Namespace).Get(context.TODO(), oldTask.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			if err := cc.cache.DeletePod(oldTask); err != nil {
				klog.Errorf("failed to delete cache pod <%v/%v>, err %v.", oldTask.Namespace, oldTask.Name, err)
				return err
			}
			klog.V(3).Infof("Pod <%v/%v> was deleted, removed from cache.", oldTask.Namespace, oldTask.Name)

			return nil
		}
		return fmt.Errorf("failed to get Pod <%v/%v>: err %v", oldTask.Namespace, oldTask.Name, err)
	}

	return cc.cache.UpdatePod(newPod)
}

func (cc *jobcontroller) resyncTask(task *v1.Pod) {
	cc.errTasks.AddRateLimited(task)
}
