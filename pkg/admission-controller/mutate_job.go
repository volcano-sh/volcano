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
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// mutate job.
func MutateJobs(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(3).Infof("mutating jobs")

	job, err := DecodeJob(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return ToAdmissionResponse(err)
	}

	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	var patchBytes []byte
	switch ar.Request.Operation {
	case v1beta1.Create:
		patchBytes, err = createPatch(job)
		break
	default:
		err = fmt.Errorf("expect operation to be 'CREATE' ")
		return ToAdmissionResponse(err)
	}

	if err != nil {
		reviewResponse.Result = &metav1.Status{Message: err.Error()}
		return &reviewResponse
	}
	glog.V(3).Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	reviewResponse.Patch = patchBytes
	pt := v1beta1.PatchTypeJSONPatch
	reviewResponse.PatchType = &pt

	return &reviewResponse
}

func createPatch(job v1alpha1.Job) ([]byte, error) {
	var patch []patchOperation
	patch = append(patch, mutateSpec(job.Spec.Tasks, "/spec/tasks")...)
	patch = append(patch, mutateMetadata(job.ObjectMeta, "/metadata")...)

	return json.Marshal(patch)
}

func mutateSpec(tasks []v1alpha1.TaskSpec, basePath string) (patch []patchOperation) {
	for index := range tasks {
		// add default task name
		taskName := tasks[index].Name
		if len(taskName) == 0 {
			tasks[index].Name = v1alpha1.DefaultTaskSpec
		}
	}
	patch = append(patch, patchOperation{
		Op:    "replace",
		Path:  basePath,
		Value: tasks,
	})

	return patch
}

func mutateMetadata(metadata metav1.ObjectMeta, basePath string) (patch []patchOperation) {
	if len(metadata.Annotations) == 0 {
		metadata.Annotations = make(map[string]string)
	}
	randomStr := genRandomStr(5)
	metadata.Annotations[PVCInputName] = fmt.Sprintf("%s-input-%s", metadata.Name, randomStr)
	metadata.Annotations[PVCOutputName] = fmt.Sprintf("%s-output-%s", metadata.Name, randomStr)
	patch = append(patch, patchOperation{
		Op:    "replace",
		Path:  basePath,
		Value: metadata,
	})

	return patch
}

func genRandomStr(l int) string {
	str := "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := []byte(str)
	result := []byte{}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < l; i++ {
		result = append(result, bytes[r.Intn(len(bytes))])
	}
	return string(result)
}
