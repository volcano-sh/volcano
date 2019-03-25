/*
Copyright 2017 The Volcano Authors.

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

package cronjob

import (
	"github.com/golang/glog"
        "github.com/robfig/cron"

	"k8s.io/client-go/tools/cache"

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

func (cc *Controller) addCronJob(obj interface{}) {
	cronJob, ok := obj.(*vkbatchv1.CronJob)
	if !ok {
		glog.Errorf("obj is not a CronJob")
		return
	}

	jobKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(cronJob)

	if err != nil {
		glog.Errorf("Failed to get key for CronJob: %s, %s",cronJob, err)
	}
	cc.jobStore.AddOrUpdate(jobKey, cronJob)
	cc.queue.Add(jobKey)
}

func (cc *Controller) updateCronJob(oldObj, newObj interface{}) {
	newCronJob, ok := newObj.(*vkbatchv1.CronJob)
	if !ok {
		glog.Errorf("newObj is not CronJob")
		return
	}

	_, ok = oldObj.(*vkbatchv1.CronJob)
	if !ok {
		glog.Errorf("oldJob is not CronJob")
		return
	}

	jobKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newCronJob)

	if err != nil {
		glog.Errorf("Failed to get key for CronJob: %s, %s",newCronJob, err)
	}

	cc.jobStore.AddOrUpdate(jobKey, newCronJob)
	cc.queue.Add(jobKey)
}

func (cc *Controller) deleteCronJob(obj interface{}) {
	cronJob, ok := obj.(*vkbatchv1.CronJob)
	if !ok {
		glog.Errorf("obj is not a CronJob")
		return
	}

	jobKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(cronJob)
	if err != nil {
		glog.Errorf("Failed to get key for CronJob: %s, %s",cronJob, err)
	}
	cc.jobStore.Delete(jobKey)
	cc.queue.Forget(jobKey)
	cc.queue.Done(jobKey)
}


func (cc *Controller) syncCronJob(cronjobKey string) error {
	cronJob, err := cc.jobStore.Get(cronjobKey)
	if err != nil {
		glog.Errorf("Unable to find CronJob %s in cache : %s",cronjobKey, err)
		return err
	}
	glog.Infof("Starting to synchronize CronJob: %s/%s", cronJob.Namespace, cronJob.Name)
	//schedule, err := cron.ParseStandard(cronJob.Spec.Schedule)
	if err != nil {
		glog.Errorf("failed to parse schedule %s of CronJob %s/%s: %v", cronJob.Spec.Schedule, cronJob.Namespace, cronJob.Name, err)
		cronJob.Status.State = vkbatchv1.Stopped
		cronJob.Status.Reason = err.Error()
	} else {
		cronJob.Status.State = vkbatchv1.Scheduled
		now := cc.clock.Now()
		if cronJob.Status.NextRun.Time.IsZero() {
			cronJob.Status.NextRun = now
		}
	}


}
