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
	"encoding/json"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	webconfig "volcano.sh/volcano/pkg/webhooks/config"
)

func TestMutatePods(t *testing.T) {
	affinityJSONStr := `{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"kubernetes.io/os","operator":"In","values":["linux"]}]}]}}}`
	var affinity v1.Affinity
	json.Unmarshal([]byte(affinityJSONStr), &affinity)

	admissionConfigData := &webconfig.AdmissionConfiguration{
		ResGroupsConfig: []webconfig.ResGroupConfig{
			{
				ResourceGroup: "management",
				Object: webconfig.Object{
					Key: "namespace",
					Value: []string{
						"mng-ns-1",
						"mng-ns-2",
					},
				},
				SchedulerName: "default-scheduler",
				Tolerations: []v1.Toleration{
					{
						Key:      "mng-taint-1",
						Operator: v1.TolerationOpExists,
						Effect:   v1.TaintEffectNoSchedule,
					},
				},
				Affinity: affinityJSONStr,
				Labels: map[string]string{
					"volcano.sh/nodetype": "management",
				},
			},
			{
				ResourceGroup: "cpu",
				Object: webconfig.Object{
					Key: "annotation",
					Value: []string{
						"volcano.sh/resource-group: cpu",
					},
				},
				SchedulerName: "volcano",
				Labels: map[string]string{
					"volcano.sh/nodetype": "cpu",
				},
			},
			{
				ResourceGroup: "gpu",
				SchedulerName: "volcano",
				Labels: map[string]string{
					"volcano.sh/nodetype": "gpu",
				},
			},
		},
	}

	config.ConfigData = admissionConfigData

	testCases := []struct {
		Name   string
		Pod    *v1.Pod
		expect []patchOperation
	}{
		{
			Name: "test-1",
			Pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "mng-ns-1",
					Name:      "mng-pod",
				},
				Spec: v1.PodSpec{
					SchedulerName: "default-scheduler",
				},
			},
			expect: []patchOperation{
				{
					Op:   "add",
					Path: "/spec/nodeSelector",
					Value: map[string]string{
						"volcano.sh/nodetype": "management",
					},
				},
				{
					Op:    "add",
					Path:  "/spec/affinity",
					Value: affinity,
				},
				{
					Op:   "add",
					Path: "/spec/tolerations",
					Value: []v1.Toleration{
						{
							Key:      "mng-taint-1",
							Operator: v1.TolerationOpExists,
							Effect:   v1.TaintEffectNoSchedule,
						},
					},
				},
				{
					Op:    "add",
					Path:  "/spec/schedulerName",
					Value: "default-scheduler",
				},
			},
		},
		{
			Name: "test-2",
			Pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "cpu-pod",
					Annotations: map[string]string{
						"volcano.sh/resource-group": "cpu",
					},
				},
			},
			expect: []patchOperation{
				{
					Op:   "add",
					Path: "/spec/nodeSelector",
					Value: map[string]string{
						"volcano.sh/nodetype": "cpu",
					},
				},
				{
					Op:    "add",
					Path:  "/spec/schedulerName",
					Value: "volcano",
				},
			},
		},
		{
			Name: "test-3",
			Pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-pod",
					Annotations: map[string]string{
						"volcano.sh/resource-group": "gpu",
					},
				},
			},
			expect: []patchOperation{
				{
					Op:   "add",
					Path: "/spec/nodeSelector",
					Value: map[string]string{
						"volcano.sh/nodetype": "gpu",
					},
				},
				{
					Op:    "add",
					Path:  "/spec/schedulerName",
					Value: "volcano",
				},
			},
		},
		{
			Name: "test-4",
			Pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "normal-pod",
				},
			},
			expect: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			patchBytes, _ := createPatch(testCase.Pod)
			expectBytes, _ := json.Marshal(testCase.expect)
			if !reflect.DeepEqual(patchBytes, expectBytes) {
				t.Errorf("Test case '%s' failed, expect: %v, got: %v", testCase.Name,
					expectBytes, patchBytes)
			}
		})
	}
}
