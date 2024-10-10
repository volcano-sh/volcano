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
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

const (
	KubeParentQueueLabelKey = "volcano.sh/parent-queue"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path:   "/queues/mutate",
	Func:   Queues,
	Config: config,

	MutatingConfig: &whv1.MutatingWebhookConfiguration{
		Webhooks: []whv1.MutatingWebhook{{
			Name: "mutatequeue.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update},
					Rule: whv1.Rule{
						APIGroups:   []string{schedulingv1beta1.SchemeGroupVersion.Group},
						APIVersions: []string{schedulingv1beta1.SchemeGroupVersion.Version},
						Resources:   []string{"queues"},
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

var config = &router.AdmissionServiceConfig{}

// Queues mutate queues.
func Queues(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("Mutating %s queue %s.", ar.Request.Operation, ar.Request.Name)

	queue, err := schema.DecodeQueue(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}
	config.ConfigData.Lock()
	enableHierarchy := config.ConfigData.EnableHierarchyCapacity
	config.ConfigData.Unlock()

	var patchBytes []byte
	switch ar.Request.Operation {
	case admissionv1.Create:
		patchBytes, err = createQueuePatch(queue, enableHierarchy)
	case admissionv1.Update:
		if !enableHierarchy {
			break
		}

		var oldQueue *schedulingv1beta1.Queue
		oldQueue, err = schema.DecodeQueue(ar.Request.OldObject, ar.Request.Resource)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}

		if oldQueue.Spec.Parent != queue.Spec.Parent {
			patchBytes, err = updateQueuePatch(queue)
		}
	default:
		return util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
			"expect operation to be `CREATE` or `UPDATE`", ar.Request.Operation))
	}

	if err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result:  &metav1.Status{Message: err.Error()},
		}
	}

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

func createQueuePatch(queue *schedulingv1beta1.Queue, enableCapacityHierarchy bool) ([]byte, error) {
	var patch []patchOperation

	// add root node if the root node not specified
	hierarchy := queue.Annotations[schedulingv1beta1.KubeHierarchyAnnotationKey]
	hierarchicalWeights := queue.Annotations[schedulingv1beta1.KubeHierarchyWeightAnnotationKey]

	if hierarchy != "" && hierarchicalWeights != "" && !strings.HasPrefix(hierarchy, "root") {
		// based on https://tools.ietf.org/html/rfc6901#section-3
		// escape "/" with "~1"
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(schedulingv1beta1.KubeHierarchyAnnotationKey, "/", "~1")),
			Value: fmt.Sprintf("root/%s", hierarchy),
		})
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(schedulingv1beta1.KubeHierarchyWeightAnnotationKey, "/", "~1")),
			Value: fmt.Sprintf("1/%s", hierarchicalWeights),
		})
	}

	trueValue := true
	if queue.Spec.Reclaimable == nil {
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  "/spec/reclaimable",
			Value: &trueValue,
		})
	}

	defaultWeight := 1
	if queue.Spec.Weight == 0 {
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  "/spec/weight",
			Value: &defaultWeight,
		})
	}

	if enableCapacityHierarchy {
		parent := queue.Spec.Parent
		if parent == "" {
			parent = "root"
		}

		if len(queue.Labels) == 0 {
			patch = append(patch, patchOperation{
				Op:   "replace",
				Path: "/metadata/labels",
				Value: map[string]string{
					KubeParentQueueLabelKey: parent,
				},
			})
		} else {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(KubeParentQueueLabelKey, "/", "~1")),
				Value: parent,
			})
		}
	}

	return json.Marshal(patch)
}

func updateQueuePatch(queue *schedulingv1beta1.Queue) ([]byte, error) {
	var patch []patchOperation

	parent := queue.Spec.Parent
	if parent == "" {
		parent = "root"
	}
	if len(queue.Labels) == 0 {
		patch = append(patch, patchOperation{
			Op:   "replace",
			Path: "/metadata/labels",
			Value: map[string]string{
				KubeParentQueueLabelKey: parent,
			},
		})
	} else {
		patch = append(patch, patchOperation{
			Op:    "replace",
			Path:  fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(KubeParentQueueLabelKey, "/", "~1")),
			Value: parent,
		})
	}

	return json.Marshal(patch)
}
