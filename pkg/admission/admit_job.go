/*
Copyright 2019 The Kubernetes Authors.

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
	"reflect"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	v1alpha1 "github.com/kubernetes-sigs/volcano/pkg/apis/batch/v1alpha1"
	"github.com/kubernetes-sigs/volcano/pkg/controllers/job/plugins"
)

// AdmitJobs function is used to admit jobs
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
		msg = validateJobSpec(job.Spec, &reviewResponse)
		break
	case v1beta1.Update:
		oldJob, err := DecodeJob(ar.Request.OldObject, ar.Request.Resource)
		if err != nil {
			return ToAdmissionResponse(err)
		}
		msg = specDeepEqual(job, oldJob, &reviewResponse)
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

func validateJobSpec(jobSpec v1alpha1.JobSpec, reviewResponse *v1beta1.AdmissionResponse) string {

	var msg string
	taskNames := map[string]string{}
	var totalReplicas int32

	if len(jobSpec.Tasks) == 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("No task specified in job spec")
	}

	for _, task := range jobSpec.Tasks {
		if task.Replicas == 0 {
			msg = msg + fmt.Sprintf(" 'replicas' is set '0' in task: %s;", task.Name)
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

		// validate task event policies
		if err := ValidatePolicies(task.Policies); err != nil {
			msg = msg + err.Error()
		}

	}

	if totalReplicas < jobSpec.MinAvailable {
		msg = msg + " 'minAvailable' should not be greater than total replicas in tasks;"
	}

	// validate job event policies
	if err := ValidatePolicies(jobSpec.Policies); err != nil {
		msg = msg + err.Error()
	}

	// invalid job plugins
	if len(jobSpec.Plugins) != 0 {
		for name := range jobSpec.Plugins {
			if _, found := plugins.GetPluginBuilder(name); !found {
				msg = msg + fmt.Sprintf(" unable to find job plugin: %s", name)
			}
		}
	}

	if msg != "" {
		reviewResponse.Allowed = false
	}

	return msg
}

func specDeepEqual(newJob v1alpha1.Job, oldJob v1alpha1.Job, reviewResponse *v1beta1.AdmissionResponse) string {
	var msg string
	if !reflect.DeepEqual(newJob.Spec, oldJob.Spec) {
		reviewResponse.Allowed = false
		msg = "job.spec is not allowed to modify when update jobs;"
	}

	return msg
}
