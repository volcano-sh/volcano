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

package validate

import (
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

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path:   "/podgroups/validate",
	Func:   Validate,
	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatepodgroup.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update},
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

// Validate validates the PodGroup object when creating or updating it
func Validate(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("Validating %s PodGroup %s", ar.Request.Operation, ar.Request.Name)

	podgroup, err := schema.DecodePodGroup(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	var errMsg string
	switch ar.Request.Operation {
	case admissionv1.Create:
		errMsg = validatePodGroup(podgroup)
	case admissionv1.Update:
		errMsg = validatePodGroupTopology(podgroup)
	default:
		errMsg = fmt.Sprintf("unsupported operation %s", ar.Request.Operation)
	}

	if errMsg != "" {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result:  &metav1.Status{Message: errMsg},
		}
	}

	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// validatePodGroup validates a PodGroup when it's being created
func validatePodGroup(pg *schedulingv1beta1.PodGroup) string {
	var errs []string

	if msg := checkQueueState(pg.Spec.Queue); msg != "" {
		errs = append(errs, strings.TrimSpace(msg))
	}
	if msg := validateNetworkTopology(pg.Spec.NetworkTopology, pg.Spec.SubGroupPolicy); msg != "" {
		errs = append(errs, strings.TrimSpace(msg))
	}
	if msg := validateTopologyAffinity(pg.Spec.TopologyAffinity); msg != "" {
		errs = append(errs, strings.TrimSpace(msg))
	}

	return strings.Join(errs, "; ")
}

func validatePodGroupTopology(pg *schedulingv1beta1.PodGroup) string {
	var errs []string
	if msg := validateNetworkTopology(pg.Spec.NetworkTopology, pg.Spec.SubGroupPolicy); msg != "" {
		errs = append(errs, strings.TrimSpace(msg))
	}
	if msg := validateTopologyAffinity(pg.Spec.TopologyAffinity); msg != "" {
		errs = append(errs, strings.TrimSpace(msg))
	}
	return strings.Join(errs, "; ")
}

// checkQueueState verifies if the queue exists and is in the open state
func checkQueueState(queueName string) string {
	if queueName == "" {
		return ""
	}

	queue, err := config.QueueLister.Get(queueName)
	if err != nil {
		return fmt.Sprintf("unable to find queue: %s", err.Error())
	}

	if queue.Status.State != schedulingv1beta1.QueueStateOpen {
		return fmt.Sprintf("can only submit PodGroup to queue with state `Open`, queue `%s` status is `%s`. ",
			queue.Name, queue.Status.State)
	}

	return ""
}

func validateNetworkTopology(networkTopology *schedulingv1beta1.NetworkTopologySpec, policies []schedulingv1beta1.SubGroupPolicySpec) string {
	var errs []string
	if networkTopology != nil && networkTopology.HighestTierAllowed != nil && networkTopology.HighestTierName != "" {
		errs = append(errs, "must not specify 'highestTierAllowed' and 'highestTierName' in networkTopology simultaneously.")
	}
	for _, policy := range policies {
		if policy.NetworkTopology != nil && policy.NetworkTopology.HighestTierAllowed != nil && policy.NetworkTopology.HighestTierName != "" {
			errs = append(errs, fmt.Sprintf("in subGroupPolicy '%s': must not specify 'highestTierAllowed' and 'highestTierName' in networkTopology simultaneously.", policy.Name))
		}
	}
	return strings.Join(errs, " ")
}

func validateTopologyAffinity(topologyAffinity *schedulingv1beta1.TopologyAffinitySpec) string {
	if topologyAffinity == nil {
		return ""
	}
	var errs []string
	if anti := topologyAffinity.PodGroupAntiAffinity; anti != nil {
		for index, term := range anti.Required {
			errs = append(errs, validatePodGroupAffinityTerm(
				fmt.Sprintf("topologyAffinity.podGroupAntiAffinity.required[%d]", index), term, true)...)
		}
		for index, term := range anti.Preferred {
			errs = append(errs, validatePodGroupAffinityTerm(
				fmt.Sprintf("topologyAffinity.podGroupAntiAffinity.preferred[%d]", index), term, false)...)
		}
	}
	return strings.Join(errs, "; ")
}

func validatePodGroupAffinityTerm(path string, term schedulingv1beta1.PodGroupAffinityTerm, required bool) []string {
	var errs []string
	if required && term.Weight != 0 {
		errs = append(errs, fmt.Sprintf("%s: weight must not be set on required terms", path))
	}
	if !required && (term.Weight < 1 || term.Weight > 100) {
		errs = append(errs, fmt.Sprintf("%s: weight must be an integer in the range 1-100 for preferred terms", path))
	}
	if term.PodGroupSelector == nil {
		errs = append(errs, fmt.Sprintf("%s: podGroupSelector is required", path))
	} else if _, err := metav1.LabelSelectorAsSelector(term.PodGroupSelector); err != nil {
		errs = append(errs, fmt.Sprintf("%s: invalid podGroupSelector: %v", path, err))
	}
	if term.NamespaceSelector != nil {
		if _, err := metav1.LabelSelectorAsSelector(term.NamespaceSelector); err != nil {
			errs = append(errs, fmt.Sprintf("%s: invalid namespaceSelector: %v", path, err))
		}
	}
	if term.TopologyTier != nil && term.TopologyTierName != "" {
		errs = append(errs, fmt.Sprintf("%s: must not specify topologyTier and topologyTierName simultaneously", path))
	}
	if term.TopologyTier == nil && term.TopologyTierName == "" {
		errs = append(errs, fmt.Sprintf("%s: must specify topologyTier or topologyTierName", path))
	}
	return errs
}
