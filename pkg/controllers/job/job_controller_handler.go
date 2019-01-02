/*
Copyright 2017 The Vulcan Authors.

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
	"reflect"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	vkapi "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/apis/helpers"
)

func (cc *Controller) addJob(obj interface{}) {
	job, ok := obj.(*vkapi.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	cc.enqueue(job)
}

func (cc *Controller) updateJob(oldObj, newObj interface{}) {
	newJob, ok := newObj.(*vkapi.Job)
	if !ok {
		glog.Errorf("newObj is not Job")
		return
	}

	oldJob, ok := oldObj.(*vkapi.Job)
	if !ok {
		glog.Errorf("oldObj is not Job")
		return
	}

	if !reflect.DeepEqual(oldJob.Spec, newJob.Spec) {
		cc.enqueue(newJob)
	}
}

func (cc *Controller) deleteJob(obj interface{}) {
	job, ok := obj.(*vkapi.Job)
	if !ok {
		glog.Errorf("obj is not Job")
		return
	}

	cc.enqueue(job)
}

func (cc *Controller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	cc.enqueue(pod)
}

func (cc *Controller) updatePod(oldObj, newObj interface{}) {
	pod, ok := newObj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", newObj)
		return
	}

	cc.enqueue(pod)
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

	jobs, err := cc.jobLister.List(labels.Everything())
	if err != nil {
		glog.Errorf("Failed to list Jobs for Pod %v/%v", pod.Namespace, pod.Name)
	}

	ctl := helpers.GetController(pod)
	for _, job := range jobs {
		if job.UID == ctl {
			cc.enqueue(job)
			break
		}
	}
}

func (cc *Controller) enqueue(obj interface{}) {
	err := cc.eventQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue Job to update queue, err %#v", err)
	}
}
