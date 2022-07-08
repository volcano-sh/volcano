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

package mutate

import (
	"encoding/json"
	"fmt"
	"strconv"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	controllerMpi "volcano.sh/volcano/pkg/controllers/job/plugins/distributed-framework/mpi"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

const (
	// DefaultQueue constant stores the name of the queue as "default"
	DefaultQueue = "default"
	// DefaultMaxRetry is the default number of retries.
	DefaultMaxRetry = 3

	defaultSchedulerName = "volcano"

	defaultMaxRetry int32 = 3
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/jobs/mutate",
	Func: Jobs,

	MutatingConfig: &whv1.MutatingWebhookConfiguration{
		Webhooks: []whv1.MutatingWebhook{{
			Name: "mutatejob.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create},
					Rule: whv1.Rule{
						APIGroups:   []string{"batch.volcano.sh"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"jobs"},
					},
				},
			},
		}},
	},
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Jobs mutate jobs.
func Jobs(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("mutating jobs")

	job, err := schema.DecodeJob(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	var patchBytes []byte
	switch ar.Request.Operation {
	case admissionv1.Create:
		patchBytes, _ = createPatch(job)
	default:
		err = fmt.Errorf("expect operation to be 'CREATE' ")
		return util.ToAdmissionResponse(err)
	}

	klog.V(3).Infof("AdmissionResponse: patch=%v", string(patchBytes))
	reviewResponse := admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
	}
	if len(patchBytes) > 0 {
		pt := admissionv1.PatchTypeJSONPatch
		reviewResponse.PatchType = &pt
	}

	return &reviewResponse
}

func createPatch(job *v1alpha1.Job) ([]byte, error) {
	var patch []patchOperation
	pathQueue := patchDefaultQueue(job)
	if pathQueue != nil {
		patch = append(patch, *pathQueue)
	}
	pathScheduler := patchDefaultScheduler(job)
	if pathScheduler != nil {
		patch = append(patch, *pathScheduler)
	}
	pathMaxRetry := patchDefaultMaxRetry(job)
	if pathMaxRetry != nil {
		patch = append(patch, *pathMaxRetry)
	}
	pathSpec := mutateSpec(job.Spec.Tasks, "/spec/tasks", job)
	if pathSpec != nil {
		patch = append(patch, *pathSpec)
	}
	pathMinAvailable := patchDefaultMinAvailable(job)
	if pathMinAvailable != nil {
		patch = append(patch, *pathMinAvailable)
	}
	// Add default plugins for some distributed-framework plugin cases
	patchPlugins := patchDefaultPlugins(job)
	if patchPlugins != nil {
		patch = append(patch, *patchPlugins)
	}
	return json.Marshal(patch)
}

func patchDefaultQueue(job *v1alpha1.Job) *patchOperation {
	//Add default queue if not specified.
	if job.Spec.Queue == "" {
		return &patchOperation{Op: "add", Path: "/spec/queue", Value: DefaultQueue}
	}
	return nil
}

func patchDefaultScheduler(job *v1alpha1.Job) *patchOperation {
	// Add default scheduler name if not specified.
	if job.Spec.SchedulerName == "" {
		return &patchOperation{Op: "add", Path: "/spec/schedulerName", Value: defaultSchedulerName}
	}
	return nil
}

func patchDefaultMaxRetry(job *v1alpha1.Job) *patchOperation {
	// Add default maxRetry if maxRetry is zero.
	if job.Spec.MaxRetry == 0 {
		return &patchOperation{Op: "add", Path: "/spec/maxRetry", Value: DefaultMaxRetry}
	}
	return nil
}

func patchDefaultMinAvailable(job *v1alpha1.Job) *patchOperation {
	// Add default minAvailable if minAvailable is zero.
	if job.Spec.MinAvailable == 0 {
		var jobMinAvailable int32
		for _, task := range job.Spec.Tasks {
			if task.MinAvailable != nil {
				jobMinAvailable += *task.MinAvailable
			} else {
				jobMinAvailable += task.Replicas
			}
		}

		return &patchOperation{Op: "add", Path: "/spec/minAvailable", Value: jobMinAvailable}
	}
	return nil
}

func mutateSpec(tasks []v1alpha1.TaskSpec, basePath string, job *v1alpha1.Job) *patchOperation {
	// TODO: Enable this configuration when dependOn supports coexistence with the gang plugin
	// if _, ok := job.Spec.Plugins[controllerMpi.MpiPluginName]; ok {
	// 	mpi.AddDependsOn(job)
	// }
	patched := false
	for index := range tasks {
		// add default task name
		taskName := tasks[index].Name
		if len(taskName) == 0 {
			patched = true
			tasks[index].Name = v1alpha1.DefaultTaskSpec + strconv.Itoa(index)
		}

		if tasks[index].Template.Spec.HostNetwork && tasks[index].Template.Spec.DNSPolicy == "" {
			patched = true
			tasks[index].Template.Spec.DNSPolicy = v1.DNSClusterFirstWithHostNet
		}

		if tasks[index].MinAvailable == nil {
			patched = true
			minAvailable := tasks[index].Replicas
			tasks[index].MinAvailable = &minAvailable
		}

		if tasks[index].MaxRetry == 0 {
			patched = true
			tasks[index].MaxRetry = defaultMaxRetry
		}
	}
	if !patched {
		return nil
	}
	return &patchOperation{
		Op:    "replace",
		Path:  basePath,
		Value: tasks,
	}
}

func patchDefaultPlugins(job *v1alpha1.Job) *patchOperation {
	if job.Spec.Plugins == nil {
		return nil
	}
	plugins := map[string][]string{}
	for k, v := range job.Spec.Plugins {
		plugins[k] = v
	}

	// Because the tensorflow-plugin and mpi-plugin depends on svc-plugin.
	// If the svc-plugin is not defined, we should add it.
	_, hasTf := job.Spec.Plugins["tensorflow"]
	_, hasMPI := job.Spec.Plugins[controllerMpi.MPIPluginName]
	if hasTf || hasMPI {
		if _, ok := plugins["svc"]; !ok {
			plugins["svc"] = []string{}
		}
	}

	if _, ok := job.Spec.Plugins["mpi"]; ok {
		if _, ok := plugins["ssh"]; !ok {
			plugins["ssh"] = []string{}
		}
	}

	return &patchOperation{
		Op:    "replace",
		Path:  "/spec/plugins",
		Value: plugins,
	}
}
