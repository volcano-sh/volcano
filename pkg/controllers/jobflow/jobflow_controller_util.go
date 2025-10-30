/*
Copyright 2022 The Volcano Authors.

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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	v1alpha1flow "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func getJobName(jobFlowName string, jobTemplateName string) string {
	return jobFlowName + "-" + jobTemplateName
}

// GenerateObjectString generates the object information string using namespace and name
func GenerateObjectString(namespace, name string) string {
	return namespace + "." + name
}

func isControlledBy(obj metav1.Object, gvk schema.GroupVersionKind) bool {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef == nil {
		return false
	}
	if controllerRef.APIVersion == gvk.GroupVersion().String() && controllerRef.Kind == gvk.Kind {
		return true
	}
	return false
}

func getJobFlowNameByJob(job *batch.Job) string {
	for _, owner := range job.OwnerReferences {
		if owner.Kind == JobFlow && strings.Contains(owner.APIVersion, Volcano) {
			return owner.Name
		}
	}
	return ""
}

func getFlowByName(jobFlow *v1alpha1flow.JobFlow, flowName string) (*v1alpha1flow.Flow, error) {
	if jobFlow == nil {
		return nil, fmt.Errorf("jobFlow is nil")
	}
	if jobFlow.Spec.Flows == nil {
		return nil, fmt.Errorf("no flows defined in JobFlow %q", jobFlow.Name)
	}
	var flow *v1alpha1flow.Flow
	for i := range jobFlow.Spec.Flows {
		if jobFlow.Spec.Flows[i].Name == flowName {
			flow = &jobFlow.Spec.Flows[i]
			break
		}
	}
	if flow == nil {
		klog.Infof("Flow %q not found in JobFlow %q", flowName, jobFlow.Name)
		return nil, fmt.Errorf("flow %q not found in JobFlow %q", flowName, jobFlow.Name)
	}
	return flow, nil
}

func mergeJobLevelVolumes(base, patch []batch.VolumeSpec) []batch.VolumeSpec {
	if patch == nil {
		return base
	}

	volumeMap := make(map[string]bool)
	merged := make([]batch.VolumeSpec, 0)

	for _, v := range base {
		volumeMap[v.MountPath] = true
		merged = append(merged, v)
	}

	for _, v := range patch {
		if volumeMap[v.MountPath] {
			for i, existing := range merged {
				if existing.MountPath == v.MountPath {
					merged[i] = v
					break
				}
			}
		} else {
			merged = append(merged, v)
		}
	}

	return merged
}

func mergeJobLevelTasks(base, patch []batch.TaskSpec) ([]batch.TaskSpec, error) {
	if base == nil {
		return patch, nil
	}
	if patch == nil {
		return base, nil
	}

	taskMap := make(map[string]bool)
	merged := make([]batch.TaskSpec, 0)

	for _, t := range base {
		taskMap[t.Name] = true
		merged = append(merged, t)
	}

	for _, t := range patch {
		if taskMap[t.Name] {
			for i, existing := range merged {
				if existing.Name == t.Name {
					originalJSON, _ := json.Marshal(existing.Template)
					modifiedJSON, _ := json.Marshal(t.Template)

					patchResult, err := strategicpatch.StrategicMergePatch(originalJSON, modifiedJSON, v1.PodTemplateSpec{})
					if err != nil {
						klog.Errorf("Failed to create strategic merge patch: %v", err)
						continue
					}

					var mergedTemplate v1.PodTemplateSpec
					if err := json.Unmarshal(patchResult, &mergedTemplate); err != nil {
						klog.Errorf("Failed to unmarshal merged template: %v", err)
						continue
					}

					t.Template = mergedTemplate
					merged[i] = t
					break
				}
			}
		} else {
			merged = append(merged, t)
		}
	}

	return merged, nil
}
