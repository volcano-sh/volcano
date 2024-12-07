/*
Copyright 2022 The Volcano Authors.

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

package jobtemplate

import (
	"strings"

	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func (jt *jobtemplatecontroller) enqueue(req apis.FlowRequest) {
	jt.queue.Add(req)
}

func (jt *jobtemplatecontroller) addJobTemplate(obj interface{}) {
	jobTemplate, ok := obj.(*v1alpha1.JobTemplate)
	if !ok {
		klog.Errorf("Failed to convert %v to jobTemplate", obj)
		return
	}

	req := apis.FlowRequest{
		Namespace:       jobTemplate.Namespace,
		JobTemplateName: jobTemplate.Name,
	}

	jt.enqueueJobTemplate(req)
}

func (jt *jobtemplatecontroller) addJob(obj interface{}) {
	job, ok := obj.(*batch.Job)
	if !ok {
		klog.Errorf("Failed to convert %v to vcjob", obj)
		return
	}

	if job.Labels[CreatedByJobTemplate] == "" {
		return
	}

	//Filter vcjobs created by JobFlow
	namespaceName := strings.Split(job.Labels[CreatedByJobTemplate], ".")
	if len(namespaceName) != CreateByJobTemplateValueNum {
		return
	}
	namespace, name := namespaceName[0], namespaceName[1]

	req := apis.FlowRequest{
		Namespace:       namespace,
		JobTemplateName: name,
	}
	jt.enqueueJobTemplate(req)
}
