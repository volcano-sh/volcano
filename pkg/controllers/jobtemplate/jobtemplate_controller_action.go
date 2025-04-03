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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog/v2"

	v1alpha1flow "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func (jt *jobtemplatecontroller) syncJobTemplate(jobTemplate *v1alpha1flow.JobTemplate) error {
	// search the jobs created by JobTemplate
	selector := labels.NewSelector()
	r, err := labels.NewRequirement(CreatedByJobTemplate, selection.Equals, []string{GetTemplateString(jobTemplate.Namespace, jobTemplate.Name)})
	if err != nil {
		return err
	}
	selector = selector.Add(*r)
	jobList, err := jt.jobLister.Jobs(jobTemplate.Namespace).List(selector)
	if err != nil {
		klog.Errorf("Failed to list jobs of JobTemplate %v/%v: %v",
			jobTemplate.Namespace, jobTemplate.Name, err)
		return err
	}

	if len(jobList) == 0 {
		return nil
	}

	jobListName := make([]string, 0)
	for _, job := range jobList {
		jobListName = append(jobListName, job.Name)
	}
	jobTemplate.Status.JobDependsOnList = jobListName

	//update jobTemplate status
	_, err = jt.vcClient.FlowV1alpha1().JobTemplates(jobTemplate.Namespace).UpdateStatus(context.Background(), jobTemplate, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of JobTemplate %v/%v: %v",
			jobTemplate.Namespace, jobTemplate.Name, err)
		return err
	}
	return nil
}
