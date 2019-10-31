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
	"encoding/json"
	"fmt"

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MutateQueues process mutating during queue creating
func MutateQueues(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(3).Infof("mutating %s queue %s", ar.Request.Operation, ar.Request.Name)

	queue, err := DecodeQueue(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return ToAdmissionResponse(err)
	}

	var patchBytes []byte
	switch ar.Request.Operation {
	case v1beta1.Create:
		patchBytes, err = createQueuePatch(queue)

		break
	default:
		return ToAdmissionResponse(fmt.Errorf("invalid operation %s, "+
			"expect operation to be `CREATE`", ar.Request.Operation))
	}

	if err != nil {
		return &v1beta1.AdmissionResponse{
			Allowed: false,
			Result:  &metav1.Status{Message: err.Error()},
		}
	}

	pt := v1beta1.PatchTypeJSONPatch
	return &v1beta1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &pt,
	}
}

func createQueuePatch(queue *v1alpha2.Queue) ([]byte, error) {
	var patch []patchOperation

	if len(queue.Spec.State) == 0 {
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  "/spec/state",
			Value: v1alpha2.QueueStateOpen,
		})
	}

	return json.Marshal(patch)
}
