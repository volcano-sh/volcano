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
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	vkbatchv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	vkbusv1 "hpw.cloud/volcano/pkg/apis/bus/v1alpha1"
)

func (cc *Controller) addCommand(obj interface{}) {
	cmd, ok := obj.(*vkbusv1.Command)
	if !ok {
		glog.Errorf("obj is not Command")
		return
	}

	req := &Request{
		Namespace: cmd.Namespace,
		JobName:   cmd.TargetObject.Name,

		Event:  vkbatchv1.CommandIssuedEvent,
		Action: vkbatchv1.Action(cmd.Action),
	}

	glog.V(3).Infof("Try to execute command <%v> on Job <%s/%s>",
		cmd.Action, req.Namespace, req.JobName)

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}

	go func() {
		if err := cc.vkClients.BusV1alpha1().Commands(cmd.Namespace).Delete(cmd.Name, nil); err != nil {
			glog.Errorf("Failed to delete Command <%s/%s> which maybe executed again.",
				cmd.Namespace, cmd.Name)
		}
	}()

}

func (cc *Controller) addJob(obj interface{}) {
	job, ok := obj.(*vkbatchv1.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	req := &Request{
		Namespace: job.Namespace,
		JobName:   job.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}
}

func (cc *Controller) updateJob(oldObj, newObj interface{}) {
	newJob, ok := newObj.(*vkbatchv1.Job)
	if !ok {
		glog.Errorf("newObj is not Job")
		return
	}

	req := &Request{
		Namespace: newJob.Namespace,
		JobName:   newJob.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}

}

func (cc *Controller) deleteJob(obj interface{}) {
	job, ok := obj.(*vkbatchv1.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	req := &Request{
		Namespace: job.Namespace,
		JobName:   job.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
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
		return
	}

	req := &Request{
		Namespace: pod.Namespace,
		JobName:   jobName,
		PodName:   pod.Name,

		Event: vkbatchv1.OutOfSyncEvent,
	}

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}
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

	jobName, found := newPod.Annotations[vkbatchv1.JobNameKey]
	if !found {
		return
	}

	event := vkbatchv1.OutOfSyncEvent
	if oldPod.Status.Phase != v1.PodFailed &&
		newPod.Status.Phase == v1.PodFailed {
		event = vkbatchv1.PodFailedEvent
	}

	req := &Request{
		Namespace: newPod.Namespace,
		JobName:   jobName,
		PodName:   newPod.Name,

		Event: event,
	}
	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}
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

	jobName, found := pod.Annotations[vkbatchv1.JobNameKey]
	if !found {
		return
	}

	req := &Request{
		Namespace: pod.Namespace,
		JobName:   jobName,
		PodName:   pod.Name,

		Event: vkbatchv1.PodEvictedEvent,
	}

	if err := cc.eventQueue.Add(req); err != nil {
		glog.Errorf("Failed to add request <%v> into queue: %v",
			req, err)
	}
}

// TODO(k82cn): add handler for PodGroup unschedulable event.
