package validate

import (
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/klog"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/jobflows/validate",
	Func: AdmitJobFlows,

	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatejobflow.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create},
					Rule: whv1.Rule{
						APIGroups:   []string{helpers.JobFlowKind.Group},
						APIVersions: []string{helpers.JobFlowKind.Version},
						Resources:   []string{"jobflows"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitJobFlows is to admit jobFlows and return response.
func AdmitJobFlows(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("admitting jobflows -- %s", ar.Request.Operation)

	jobFlow, err := schema.DecodeJobFlow(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	switch ar.Request.Operation {
	case admissionv1.Create:
		err = validateJobFlowCreate(jobFlow)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}
		reviewResponse := admissionv1.AdmissionResponse{}
		reviewResponse.Allowed = true
		return &reviewResponse
	default:
		return util.ToAdmissionResponse(fmt.Errorf("only support 'CREATE' operation"))
	}
}

func validateJobFlowCreate(jobFlow *jobflowv1alpha1.JobFlow) error {
	flows := jobFlow.Spec.Flows
	var msg string
	templateNames := map[string][]string{}
	vertexMap := make(map[string]*Vertex)
	dag := &DAG{}
	var duplicatedTemplate = false
	for _, template := range flows {
		if _, found := templateNames[template.Name]; found {
			// duplicate task name
			msg += fmt.Sprintf(" duplicated template name %s;", template.Name)
			duplicatedTemplate = true
			break
		} else {
			if template.DependsOn == nil || template.DependsOn.Targets == nil {
				template.DependsOn = new(jobflowv1alpha1.DependsOn)
			}
			templateNames[template.Name] = template.DependsOn.Targets
			vertexMap[template.Name] = &Vertex{Key: template.Name}
		}
	}
	// Skip closed-loop detection if there are duplicate templates
	if !duplicatedTemplate {
		// Build dag through dependencies
		for current, parents := range templateNames {
			if len(parents) > 0 {
				for _, parent := range parents {
					if _, found := vertexMap[parent]; !found {
						msg += fmt.Sprintf("cannot find the template: %s ", parent)
						vertexMap = nil
						break
					}
					dag.AddEdge(vertexMap[parent], vertexMap[current])
				}
			}
		}
		// Check if there is a closed loop
		for k := range vertexMap {
			if err := dag.BFS(vertexMap[k]); err != nil {
				msg += fmt.Sprintf("%v;", err)
				break
			}
		}
	}

	if msg != "" {
		return fmt.Errorf("failed to create jobFlow for: %s", msg)
	}

	return nil
}