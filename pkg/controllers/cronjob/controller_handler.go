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
	"fmt"
	"github.com/golang/glog"
	"github.com/pborman/uuid"
	"github.com/robfig/cron"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/apis/helpers"

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
		glog.Errorf("Failed to get key for CronJob: %s, %s", cronJob, err)
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
		glog.Errorf("Failed to get key for CronJob: %s, %s", newCronJob, err)
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
		glog.Errorf("Failed to get key for CronJob: %s, %s", cronJob, err)
	}
	cc.jobStore.Delete(jobKey)
	cc.queue.Forget(jobKey)
	cc.queue.Done(jobKey)
}

func (cc *Controller) syncCronJob(key string) error {
	cronJob, err := cc.jobStore.Get(key)
	if err != nil {
		glog.Errorf("Unable to find CronJob %s in cache : %s", key, err)
		return err
	}

	newStatus, err := cc.processCronJobSync(cronJob)
	if err != nil {
		glog.Errorf("Unable to process CronJob %s sync operation : %s", key, err)
		return err
	}

	return cc.updateCronJobStatus(cronJob, newStatus)

}

func (cc *Controller) processCronJobSync(cronJob *vkbatchv1.CronJob) (vkbatchv1.CronJobStatus, error) {
	glog.Infof("Starting to synchronize CronJob: %s/%s", cronJob.Namespace, cronJob.Name)
	newCJob := cronJob.DeepCopy()

	schedule, err := cron.ParseStandard(newCJob.Spec.Schedule)
	if err != nil {
		glog.Errorf("failed to parse schedule %s of CronJob %s/%s: %v", newCJob.Spec.Schedule, newCJob.Namespace, newCJob.Name, err)
		newCJob.Status.State = vkbatchv1.Stopped
		newCJob.Status.Reason = err.Error()
		return newCJob.Status, nil
	}

	newCJob.Status.State = vkbatchv1.Scheduled
	now := cc.clock.Now()
	nextRunTime := newCJob.Status.NextRun.Time
	if nextRunTime.IsZero() {
		nextRunTime = schedule.Next(now)
		newCJob.Status.NextRun = v1.NewTime(nextRunTime)
	}
	if nextRunTime.Before(now) {
		glog.Infof("Start to create new job for CronJob: <%s/%s>", newCJob.Namespace, newCJob.Name)
		name, err := cc.createNewJob(newCJob)
		if err != nil {
			return vkbatchv1.CronJobStatus{}, err
		}
		newCJob.Status.LastRun = v1.NewTime(now)
		newCJob.Status.NextRun = v1.NewTime(schedule.Next(newCJob.Status.LastRun.Time))
		newCJob.Status.LastRunName = name
		//Record event
		cc.recorder.Event(newCJob, core.EventTypeNormal, string(vkbatchv1.JobTriggered),
			fmt.Sprintf("New job %s started at %s", name, now))
	}
	return newCJob.Status, nil
}

func (cc *Controller) createNewJob(cronJob *vkbatchv1.CronJob) (string, error) {
	newJob := &vkbatchv1.Job{}
	newJob.Name = fmt.Sprintf("%s-%s", cronJob.Name, uuid.NewUUID())
	newJob.Spec = cronJob.Spec.Template
	newJob.OwnerReferences = []v1.OwnerReference{
		*v1.NewControllerRef(cronJob, helpers.CronJobKind),
	}
	newJob.Annotations = make(map[string]string)
	newJob.Annotations[vkbatchv1.CronJobNameKey] = cronJob.Name
	job, err := cc.vkClients.BatchV1alpha1().Jobs(cronJob.Namespace).Create(newJob)
	if err != nil {
		glog.Errorf("Failed to create job %s for CronJob %s, error: %s", job.Name, cronJob.Name, err.Error())
		return "", err
	}
	return job.Name, nil
}

func (cc *Controller) updateCronJobStatus(job *vkbatchv1.CronJob, newStatus vkbatchv1.CronJobStatus) error {
	if StatusEqual(job.Status, newStatus) {
		return nil
	}
	newJob := job.DeepCopy()
	newJob.Status = newStatus
	_, err := cc.vkClients.BatchV1alpha1().CronJobs(job.Namespace).Update(newJob)
	if err != nil {
		glog.Errorf("Failed to update CronJob's status <%s/%s>", job.Namespace, job.Name)
		return err
	}
	return nil
}

func StatusEqual(oldStatus, newStatus vkbatchv1.CronJobStatus) bool {
	return oldStatus.NextRun == newStatus.NextRun &&
		oldStatus.LastRun == newStatus.LastRun &&
		oldStatus.LastRunName == newStatus.LastRunName &&
		oldStatus.State == newStatus.State &&
		oldStatus.Reason == newStatus.Reason
}
