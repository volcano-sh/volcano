/*
Copyright 2026 The Volcano Authors.

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

package jobflow

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apivalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

const jobFlowCRDPath = "../../../config/crd/jobflow/bases/flow.volcano.sh_jobflows.yaml"

func jobFlowStatusValidator(t *testing.T) apivalidation.SchemaValidator {
	t.Helper()

	raw, err := os.ReadFile(jobFlowCRDPath)
	if err != nil {
		t.Fatalf("read JobFlow CRD: %v", err)
	}
	var crd apiextv1.CustomResourceDefinition
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatalf("unmarshal JobFlow CRD: %v", err)
	}

	var statusSchema *apiextv1.JSONSchemaProps
	for i := range crd.Spec.Versions {
		schema := crd.Spec.Versions[i].Schema
		if schema == nil || schema.OpenAPIV3Schema == nil {
			continue
		}
		if props, ok := schema.OpenAPIV3Schema.Properties["status"]; ok {
			statusSchema = &props
			break
		}
	}
	if statusSchema == nil {
		t.Fatal("JobFlow CRD has no status schema")
	}

	internal := &apiextensions.JSONSchemaProps{}
	if err := apiextv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(statusSchema, internal, nil); err != nil {
		t.Fatalf("convert status schema: %v", err)
	}
	validator, _, err := apivalidation.NewSchemaValidator(internal)
	if err != nil {
		t.Fatalf("build status validator: %v", err)
	}
	return validator
}

func TestJobFlowStatusAcceptedByCRDSchema(t *testing.T) {
	started := metav1.Time{Time: time.Unix(0, 0).UTC()}

	tests := []struct {
		name   string
		status jobflowv1alpha1.JobFlowStatus
	}{
		{
			name: "job still running, no end timestamps yet",
			status: jobflowv1alpha1.JobFlowStatus{
				RunningJobs: []string{"flowA-A"},
				JobStatusList: []jobflowv1alpha1.JobStatus{{
					Name:           "flowA-A",
					State:          v1alpha1.Running,
					StartTimestamp: started,
					RunningHistories: []jobflowv1alpha1.JobRunningHistory{{
						StartTimestamp: started,
						State:          v1alpha1.Running,
					}},
				}},
			},
		},
		{
			name: "job completed, history entry still open",
			status: jobflowv1alpha1.JobFlowStatus{
				CompletedJobs: []string{"flowA-A"},
				JobStatusList: []jobflowv1alpha1.JobStatus{{
					Name:           "flowA-A",
					State:          v1alpha1.Completed,
					StartTimestamp: started,
					EndTimestamp:   metav1.Time{Time: started.Add(time.Second)},
					RunningHistories: []jobflowv1alpha1.JobRunningHistory{{
						StartTimestamp: started,
						EndTimestamp:   metav1.Time{Time: started.Add(time.Second)},
						State:          v1alpha1.Running,
					}, {
						StartTimestamp: metav1.Time{Time: started.Add(time.Second)},
						State:          v1alpha1.Completed,
					}},
				}},
			},
		},
	}

	validator := jobFlowStatusValidator(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.status)
			if err != nil {
				t.Fatalf("marshal status: %v", err)
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("decode status: %v", err)
			}
			if errs := apivalidation.ValidateCustomResource(nil, decoded, validator); len(errs) > 0 {
				t.Errorf("the apiserver would reject this status: %v\npayload: %s", errs, encoded)
			}
		})
	}
}
