/*
Copyright 2021 The Volcano Authors.

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
	"context"
	"encoding/json"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path:   "/podgroups/mutate",
	Func:   PodGroups,
	Config: config,
	MutatingConfig: &whv1.MutatingWebhookConfiguration{
		Webhooks: []whv1.MutatingWebhook{{
			Name: "mutatepodgroup.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create},
					Rule: whv1.Rule{
						APIGroups:   []string{schedulingv1beta1.SchemeGroupVersion.Group},
						APIVersions: []string{schedulingv1beta1.SchemeGroupVersion.Version},
						Resources:   []string{"podgroups"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// PodGroups mutate podgroups.
func PodGroups(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("Mutating %s podgroup %s.", ar.Request.Operation, ar.Request.Name)

	podgroup, err := schema.DecodePodGroup(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	var patchBytes []byte
	switch ar.Request.Operation {
	case admissionv1.Create:
		patchBytes, err = createPodGroupPatch(podgroup)
	default:
		return util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
			"expect operation to be `CREATE`", ar.Request.Operation))
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

func createPodGroupPatch(podgroup *schedulingv1beta1.PodGroup) ([]byte, error) {
	if podgroup.Spec.Queue != schedulingv1beta1.DefaultQueue {
		return nil, nil
	}
	ns, err := config.KubeClient.CoreV1().Namespaces().Get(context.TODO(), podgroup.Namespace, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get namespace", "namespace", podgroup.Namespace)
		return nil, nil
	}

	if val, ok := ns.GetAnnotations()[schedulingv1beta1.QueueNameAnnotationKey]; ok {
		var patch []patchOperation
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  "/spec/queue",
			Value: val,
		})
		return json.Marshal(patch)
	}

	return nil, nil
}
