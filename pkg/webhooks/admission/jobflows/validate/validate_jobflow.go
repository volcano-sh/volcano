/*
Copyright 2025 The Volcano Authors.

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

package validate

import (
	"errors"
	"fmt"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

var (
	VertexNotDefinedError      = errors.New("vertex is not defined")
	FlowNotDAGError            = errors.New("jobflow Flow is not DAG")
	OperationNotCreateOrUpdate = errors.New("expect operation to be 'CREATE' or 'UPDATE'")
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path:   "/jobflows/validate",
	Func:   AdmitJobFlows,
	Config: config,
	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatejobflow.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update},
					Rule: whv1.Rule{
						APIGroups:   []string{"flow.volcano.sh"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"jobflows"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitJobFlows is to admit jobflows and return response.
func AdmitJobFlows(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("admitting jobflow -- %s", ar.Request.Operation)

	jobFlow, err := schema.DecodeJobFlow(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	return admitJobFlow(jobFlow, ar.Request.Operation)
}

func admitJobFlow(jobFlow *flowv1alpha1.JobFlow, operation admissionv1.Operation) *admissionv1.AdmissionResponse {
	var msg string
	reviewResponse := admissionv1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch operation {
	case admissionv1.Create, admissionv1.Update:
		msg = validateJobFlow(jobFlow, &reviewResponse)
	default:
		return util.ToAdmissionResponse(OperationNotCreateOrUpdate)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

// validateJobFlow checks the DAG and the label values stamped onto every Job
// this JobFlow creates. Those values are "<namespace>.<jobflow name>" and
// "<namespace>.<flow name>", and Kubernetes rejects label values longer than
// 63 bytes.
func validateJobFlow(jobflow *flowv1alpha1.JobFlow, reviewResponse *admissionv1.AdmissionResponse) string {
	var msgs []string
	if labelMsg := validateJobFlowLabels(jobflow); labelMsg != "" {
		reviewResponse.Allowed = false
		msgs = append(msgs, labelMsg)
	}
	if dagMsg := validateJobFlowDAG(jobflow, reviewResponse); dagMsg != "" {
		msgs = append(msgs, dagMsg)
	}
	return strings.Join(msgs, "; ")
}

func validateJobFlowLabels(jobflow *flowv1alpha1.JobFlow) string {
	var msgs []string
	jobFlowValue := jobflow.Namespace + "." + jobflow.Name
	if len(jobFlowValue) > validation.LabelValueMaxLength {
		msgs = append(msgs, fmt.Sprintf("volcano.sh/createdByJobFlow label value %q is %d bytes, which exceeds the %d byte limit; shorten the JobFlow namespace or name",
			jobFlowValue, len(jobFlowValue), validation.LabelValueMaxLength))
	}

	seen := make(map[string]struct{}, len(jobflow.Spec.Flows))
	for _, flow := range jobflow.Spec.Flows {
		if _, ok := seen[flow.Name]; ok {
			continue
		}
		seen[flow.Name] = struct{}{}
		flowValue := jobflow.Namespace + "." + flow.Name
		if len(flowValue) > validation.LabelValueMaxLength {
			msgs = append(msgs, fmt.Sprintf("volcano.sh/createdByJobTemplate label value %q for flow %q is %d bytes, which exceeds the %d byte limit; shorten the namespace or flow name",
				flowValue, flow.Name, len(flowValue), validation.LabelValueMaxLength))
		}
	}
	return strings.Join(msgs, "; ")
}

func validateJobFlowDAG(jobflow *flowv1alpha1.JobFlow, reviewResponse *admissionv1.AdmissionResponse) string {
	var msg string
	graphMap := make(map[string][]string, len(jobflow.Spec.Flows))
	for _, flow := range jobflow.Spec.Flows {
		if flow.DependsOn != nil && len(flow.DependsOn.Targets) > 0 {
			graphMap[flow.Name] = flow.DependsOn.Targets
		} else {
			graphMap[flow.Name] = []string{}
		}
	}

	vetexs, err := LoadVertexs(graphMap)

	if err != nil {
		msg = FlowNotDAGError.Error() + ": " + err.Error()
		if msg != "" {
			reviewResponse.Allowed = false
		}
		return msg
	}

	if !IsDAG(vetexs) {
		msg = FlowNotDAGError.Error()
		if msg != "" {
			reviewResponse.Allowed = false
		}
	}
	return msg
}
