/*
Copyright 2018 The Volcano Authors.

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

package admission

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"

	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8score "k8s.io/kubernetes/pkg/apis/core"
	k8scorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	k8scorevalid "k8s.io/kubernetes/pkg/apis/core/validation"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/plugins"
)

//KubeBatchClientSet is kube-batch clientset
var KubeBatchClientSet versioned.Interface

// job admit.
func AdmitJobs(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {

	glog.V(3).Infof("admitting jobs -- %s", ar.Request.Operation)

	job, err := DecodeJob(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return ToAdmissionResponse(err)
	}
	var msg string
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch ar.Request.Operation {
	case v1beta1.Create:
		msg = validateJob(job, &reviewResponse)
		break
	case v1beta1.Update:
		_, err := DecodeJob(ar.Request.OldObject, ar.Request.Resource)
		if err != nil {
			return ToAdmissionResponse(err)
		}
		break
	default:
		err := fmt.Errorf("expect operation to be 'CREATE' or 'UPDATE'")
		return ToAdmissionResponse(err)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

func validateJob(job v1alpha1.Job, reviewResponse *v1beta1.AdmissionResponse) string {

	var msg string
	taskNames := map[string]string{}
	var totalReplicas int32

	if job.Spec.MinAvailable < 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("'minAvailable' cannot be less than zero.")
	}

	if job.Spec.MaxRetry < 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("'maxRetry' cannot be less than zero.")
	}

	if job.Spec.TTLSecondsAfterFinished != nil && *job.Spec.TTLSecondsAfterFinished < 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("'ttlSecondsAfterFinished' cannot be less than zero.")
	}

	if len(job.Spec.Tasks) == 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("No task specified in job spec")
	}

	for index, task := range job.Spec.Tasks {
		if task.Replicas <= 0 {
			msg = msg + fmt.Sprintf(" 'replicas' is not set positive in task: %s;", task.Name)
		}

		// count replicas
		totalReplicas = totalReplicas + task.Replicas

		// validate task name
		if errMsgs := validation.IsDNS1123Label(task.Name); len(errMsgs) > 0 {
			msg = msg + fmt.Sprintf(" %v;", errMsgs)
		}

		// duplicate task name
		if _, found := taskNames[task.Name]; found {
			msg = msg + fmt.Sprintf(" duplicated task name %s;", task.Name)
			break
		} else {
			taskNames[task.Name] = task.Name
		}

		if err := validatePolicies(task.Policies, field.NewPath("spec.tasks.policies")); err != nil {
			msg = msg + err.Error() + fmt.Sprintf(" valid events are %v, valid actions are %v",
				getValidEvents(), getValidActions())
		}

		msg += validateTaskTemplate(task, job, index)
	}

	if totalReplicas < job.Spec.MinAvailable {
		msg = msg + " 'minAvailable' should not be greater than total replicas in tasks;"
	}

	if err := validatePolicies(job.Spec.Policies, field.NewPath("spec.policies")); err != nil {
		msg = msg + err.Error() + fmt.Sprintf(" valid events are %v, valid actions are %v;",
			getValidEvents(), getValidActions())
	}

	// invalid job plugins
	if len(job.Spec.Plugins) != 0 {
		for name := range job.Spec.Plugins {
			if _, found := plugins.GetPluginBuilder(name); !found {
				msg = msg + fmt.Sprintf(" unable to find job plugin: %s", name)
			}
		}
	}

	if validateInfo, ok := ValidateIO(job.Spec.Volumes); ok {
		msg = msg + validateInfo
	}

	// Check whether Queue already present or not
	if _, err := KubeBatchClientSet.SchedulingV1alpha1().Queues().Get(job.Spec.Queue, metav1.GetOptions{}); err != nil {
		msg = msg + fmt.Sprintf("Job not created with error: %v", err)
	}

	if msg != "" {
		reviewResponse.Allowed = false
	}

	return msg
}

func validateTaskTemplate(task v1alpha1.TaskSpec, job v1alpha1.Job, index int) string {
	var v1PodTemplate v1.PodTemplate
	v1PodTemplate.Template = *task.Template.DeepCopy()
	k8scorev1.SetObjectDefaults_PodTemplate(&v1PodTemplate)

	var coreTemplateSpec k8score.PodTemplateSpec
	k8scorev1.Convert_v1_PodTemplateSpec_To_core_PodTemplateSpec(&v1PodTemplate.Template, &coreTemplateSpec, nil)

	corePodTemplate := k8score.PodTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      task.Name,
			Namespace: job.Namespace,
		},
		Template: coreTemplateSpec,
	}

	if allErrs := k8scorevalid.ValidatePodTemplate(&corePodTemplate); len(allErrs) > 0 {
		msg := fmt.Sprintf("spec.task[%d].", index)
		for index := range allErrs {
			msg += allErrs[index].Error() + ". "
		}
		return msg
	}

	return ""
}
