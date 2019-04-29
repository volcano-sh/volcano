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

package job

import (
	"fmt"
	"strconv"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/scheduling/v1beta1"
	"k8s.io/client-go/tools/cache"

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkbusv1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	kbtype "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/apis"
	vkcache "volcano.sh/volcano/pkg/controllers/job/cache"
)

func (cc *Controller) addCommand(obj interface{}) {
	cmd, ok := obj.(*vkbusv1.Command)
	if !ok {
		glog.Errorf("obj is not Command")
		return
	}

	cc.commandQueue.Add(cmd)
}

func (cc *Controller) addJob(obj interface{}) {
	job, ok := obj.(*vkbatchv1.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	req := apis.Request{
		Namespace: job.Namespace,
		JobName:   job.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	// TODO(k82cn): if failed to add job, the cache should be refresh
	if err := cc.cache.Add(job); err != nil {
		glog.Errorf("Failed to add job <%s/%s>: %v in cache",
			job.Namespace, job.Name, err)
	}
	cc.queue.Add(req)
}

func (cc *Controller) updateJob(oldObj, newObj interface{}) {
	newJob, ok := newObj.(*vkbatchv1.Job)
	if !ok {
		glog.Errorf("newObj is not Job")
		return
	}

	if err := cc.cache.Update(newJob); err != nil {
		glog.Errorf("Failed to update job <%s/%s>: %v in cache",
			newJob.Namespace, newJob.Name, err)
	}

	req := apis.Request{
		Namespace: newJob.Namespace,
		JobName:   newJob.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	cc.queue.Add(req)
}

func (cc *Controller) deleteJob(obj interface{}) {
	var job *vkbatchv1.Job
	switch t := obj.(type) {
	case *vkbatchv1.Job:
		job = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		job, ok = t.Obj.(*vkbatchv1.Job)
		if !ok {
			glog.Errorf("Cannot convert to *vkbatchv1.Job in DeletedFinalState: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *vkbatchv1.Job: %v", t)
		return
	}

	if err := cc.cache.Delete(job); err != nil {
		glog.Errorf("Failed to delete job <%s/%s>: %v in cache",
			job.Namespace, job.Name, err)
	}
}

func (cc *Controller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	jobName, found := pod.Annotations[vkbatchv1.JobNameKey]
	if !found {
		glog.Infof("Failed to find jobName of Pod <%s/%s>, skipping",
			pod.Namespace, pod.Name)
		return
	}

	version, found := pod.Annotations[vkbatchv1.JobVersion]
	if !found {
		glog.Infof("Failed to find jobVersion of Pod <%s/%s>, skipping",
			pod.Namespace, pod.Name)
		return
	}

	dVersion, err := strconv.Atoi(version)
	if err != nil {
		glog.Infof("Failed to convert jobVersion of Pod <%s/%s> into number, skipping",
			pod.Namespace, pod.Name)
		return
	}

	req := apis.Request{
		Namespace: pod.Namespace,
		JobName:   jobName,

		Event:      vkbatchv1.OutOfSyncEvent,
		JobVersion: int32(dVersion),
	}

	if err := cc.cache.AddPod(pod); err != nil {
		glog.Errorf("Failed to add Pod <%s/%s>: %v to cache",
			pod.Namespace, pod.Name, err)
	}
	cc.queue.Add(req)
}

func (cc *Controller) updatePod(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", oldObj)
		return
	}

	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", newObj)
		return
	}

	taskName, found := newPod.Annotations[vkbatchv1.TaskSpecKey]
	if !found {
		glog.Infof("Failed to find taskName of Pod <%s/%s>, skipping",
			newPod.Namespace, newPod.Name)
		return
	}

	jobName, found := newPod.Annotations[vkbatchv1.JobNameKey]
	if !found {
		glog.Infof("Failed to find jobName of Pod <%s/%s>, skipping",
			newPod.Namespace, newPod.Name)
		return
	}

	version, found := newPod.Annotations[vkbatchv1.JobVersion]
	if !found {
		glog.Infof("Failed to find jobVersion of Pod <%s/%s>, skipping",
			newPod.Namespace, newPod.Name)
		return
	}

	dVersion, err := strconv.Atoi(version)
	if err != nil {
		glog.Infof("Failed to convert jobVersion of Pod into number <%s/%s>, skipping",
			newPod.Namespace, newPod.Name)
		return
	}

	if err := cc.cache.UpdatePod(newPod); err != nil {
		glog.Errorf("Failed to update Pod <%s/%s>: %v in cache",
			newPod.Namespace, newPod.Name, err)
	}

	event := vkbatchv1.OutOfSyncEvent
	if oldPod.Status.Phase != v1.PodFailed &&
		newPod.Status.Phase == v1.PodFailed {
		event = vkbatchv1.PodFailedEvent
	}

	if oldPod.Status.Phase != v1.PodSucceeded &&
		newPod.Status.Phase == v1.PodSucceeded {
		if cc.cache.TaskCompleted(vkcache.JobKeyByName(newPod.Namespace, jobName), taskName) {
			event = vkbatchv1.TaskCompletedEvent
		}
	}

	req := apis.Request{
		Namespace: newPod.Namespace,
		JobName:   jobName,
		TaskName:  taskName,

		Event:      event,
		JobVersion: int32(dVersion),
	}

	cc.queue.Add(req)
}

func (cc *Controller) deletePod(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	taskName, found := pod.Annotations[vkbatchv1.TaskSpecKey]
	if !found {
		glog.Infof("Failed to find taskName of Pod <%s/%s>, skipping",
			pod.Namespace, pod.Name)
		return
	}

	jobName, found := pod.Annotations[vkbatchv1.JobNameKey]
	if !found {
		glog.Infof("Failed to find jobName of Pod <%s/%s>, skipping",
			pod.Namespace, pod.Name)
		return
	}

	version, found := pod.Annotations[vkbatchv1.JobVersion]
	if !found {
		glog.Infof("Failed to find jobVersion of Pod <%s/%s>, skipping",
			pod.Namespace, pod.Name)
		return
	}

	dVersion, err := strconv.Atoi(version)
	if err != nil {
		glog.Infof("Failed to convert jobVersion of Pod <%s/%s> into number, skipping",
			pod.Namespace, pod.Name)
		return
	}

	req := apis.Request{
		Namespace: pod.Namespace,
		JobName:   jobName,
		TaskName:  taskName,

		Event:      vkbatchv1.PodEvictedEvent,
		JobVersion: int32(dVersion),
	}

	if err := cc.cache.DeletePod(pod); err != nil {
		glog.Errorf("Failed to delete Pod <%s/%s>: %v in cache",
			pod.Namespace, pod.Name, err)
	}

	cc.queue.Add(req)
}

func (cc *Controller) recordJobEvent(namespace, name string, event vkbatchv1.JobEvent, message string) {
	job, err := cc.cache.Get(vkcache.JobKeyByName(namespace, name))
	if err != nil {
		glog.Warningf("Failed to find job in cache when reporting job event <%s/%s>: %v",
			namespace, name, err)
		return
	}
	cc.recorder.Event(job.Job, v1.EventTypeNormal, string(event), message)

}

func (cc *Controller) handleCommands() {
	obj, shutdown := cc.commandQueue.Get()
	if shutdown {
		return
	}
	cmd := obj.(*vkbusv1.Command)
	defer cc.commandQueue.Done(cmd)

	if err := cc.vkClients.BusV1alpha1().Commands(cmd.Namespace).Delete(cmd.Name, nil); err != nil {
		glog.Errorf("Failed to delete Command <%s/%s>.", cmd.Namespace, cmd.Name)
		cc.commandQueue.AddRateLimited(cmd)
		return
	}
	cc.recordJobEvent(cmd.Namespace, cmd.TargetObject.Name,
		vkbatchv1.CommandIssued,
		fmt.Sprintf(
			"Start to execute command %s, and clean it up to make sure executed not more than once.", cmd.Action))
	req := apis.Request{
		Namespace: cmd.Namespace,
		JobName:   cmd.TargetObject.Name,
		Event:     vkbatchv1.CommandIssuedEvent,
		Action:    vkbatchv1.Action(cmd.Action),
	}

	cc.queue.Add(req)

}

func (cc *Controller) updatePodGroup(oldObj, newObj interface{}) {
	oldPG, ok := oldObj.(*kbtype.PodGroup)
	if !ok {
		glog.Errorf("Failed to convert %v to PodGroup", newObj)
		return
	}

	newPG, ok := newObj.(*kbtype.PodGroup)
	if !ok {
		glog.Errorf("Failed to convert %v to PodGroup", newObj)
		return
	}

	_, err := cc.cache.Get(vkcache.JobKeyByName(newPG.Namespace, newPG.Name))
	if err != nil {
		glog.Warningf(
			"Failed to find job in cache by PodGroup, this may not be a PodGroup for volcano job.")
	}

	if newPG.Status.Phase != oldPG.Status.Phase {
		req := apis.Request{
			Namespace: newPG.Namespace,
			JobName:   newPG.Name,
		}
		switch newPG.Status.Phase {
		case kbtype.PodGroupUnknown:
			req.Event = vkbatchv1.JobUnknownEvent
		case kbtype.PodGroupInqueue:
			req.Action = vkbatchv1.EnqueueAction
		}
		cc.queue.Add(req)
	}
}

// TODO(k82cn): add handler for PodGroup unschedulable event.

func (cc *Controller) addPriorityClass(obj interface{}) {
	pc := convert2PriorityClass(obj)
	if pc == nil {
		return
	}

	cc.priorityClasses[pc.Name] = pc
	return
}

func (cc *Controller) deletePriorityClass(obj interface{}) {
	pc := convert2PriorityClass(obj)
	if pc == nil {
		return
	}

	delete(cc.priorityClasses, pc.Name)
	return
}

func convert2PriorityClass(obj interface{}) *v1beta1.PriorityClass {
	var pc *v1beta1.PriorityClass
	switch t := obj.(type) {
	case *v1beta1.PriorityClass:
		pc = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pc, ok = t.Obj.(*v1beta1.PriorityClass)
		if !ok {
			glog.Errorf("Cannot convert to *v1beta1.PriorityClass: %v", t.Obj)
			return nil
		}
	default:
		glog.Errorf("Cannot convert to *v1beta1.PriorityClass: %v", t)
		return nil
	}

	return pc
}
