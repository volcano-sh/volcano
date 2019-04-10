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
	"reflect"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

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

func validateJob(job v1alpha1.Job, reviewResponse *v1beta1.AdmissionResponse) string {

	var msg string
	taskNames := map[string]string{}
	var totalReplicas int32

	if job.Spec.MinAvailable < 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("'minAvailable' cannot be less than zero.")
	}

	if len(job.Spec.Tasks) == 0 {
		reviewResponse.Allowed = false
		return fmt.Sprintf("No task specified in job spec")
	}

	for _, task := range job.Spec.Tasks {
		if task.Replicas == 0 {
			msg = msg + fmt.Sprintf(" 'replicas' is set '0' in task: %s;", task.Name)
		}

		// count replicas
		totalReplicas = totalReplicas + task.Replicas

		// duplicate task name
		if _, found := taskNames[task.Name]; found {
			msg = msg + fmt.Sprintf(" duplicated task name %s;", task.Name)
			break
		} else {
			taskNames[task.Name] = task.Name
		}

		//duplicate task event policies
		if duplicateInfo, ok := CheckPolicyDuplicate(task.Policies); ok {
			msg = msg + fmt.Sprintf(" duplicated task event policies: %s;", duplicateInfo)
		}
	}

	if totalReplicas < job.Spec.MinAvailable {
		msg = msg + " 'minAvailable' should not be greater than total replicas in tasks;"
	}

	//duplicate job event policies
	if duplicateInfo, ok := CheckPolicyDuplicate(job.Spec.Policies); ok {
		msg = msg + fmt.Sprintf(" duplicated job event policies: %s;", duplicateInfo)
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
