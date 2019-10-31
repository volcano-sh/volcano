/*
Copyright 2019 The Volcano Authors.

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

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/golang/glog"
)

// AdmitQueues is to admit queues
func AdmitQueues(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(3).Infof("admitting %s queue %s", ar.Request.Operation, ar.Request.Name)

	queue, err := DecodeQueue(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return ToAdmissionResponse(err)
	}

	switch ar.Request.Operation {
	case v1beta1.Create, v1beta1.Update:
		err = validateQueue(queue)

		break
	case v1beta1.Delete:
		err = validateQueueDeleting(ar.Request.Name)

		break
	default:
		return ToAdmissionResponse(fmt.Errorf("invalid operation %s, "+
			"expect operation to be `CREATE`, `UPDATE` or `DELETE`", ar.Request.Operation))
	}

	if err != nil {
		return &v1beta1.AdmissionResponse{
			Allowed: false,
			Result:  &metav1.Status{Message: err.Error()},
		}
	}

	return &v1beta1.AdmissionResponse{
		Allowed: true,
	}
}

func validateQueue(queue *v1alpha2.Queue) error {
	errs := field.ErrorList{}
	resourcePath := field.NewPath("requestBody")

	errs = append(errs, validateStateOfQueue(queue.Spec.State, resourcePath.Child("spec").Child("state"))...)

	if len(errs) > 0 {
		return errs.ToAggregate()
	}

	return nil
}

func validateStateOfQueue(value v1alpha2.QueueState, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	if len(value) == 0 {
		return errs
	}

	validQueueStates := []v1alpha2.QueueState{
		v1alpha2.QueueStateOpen,
		v1alpha2.QueueStateClosed,
	}

	for _, validQueue := range validQueueStates {
		if value == validQueue {
			return errs
		}
	}

	return append(errs, field.Invalid(fldPath, value, fmt.Sprintf("queue state must be in %v", validQueueStates)))
}

func validateQueueDeleting(queue string) error {
	if queue == "default" {
		return fmt.Errorf("%s queue can not be deleted", "default")
	}

	q, err := VolcanoClientSet.SchedulingV1alpha2().Queues().Get(queue, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if q.Status.State != v1alpha2.QueueStateClosed {
		return fmt.Errorf("only queue with state %s can be deleted", v1alpha2.QueueStateClosed)
	}

	return nil
}
