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

package predicates

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
	"testing"

	"github.com/stretchr/testify/assert"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestSetUpVolumeBindingArgs(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.StorageCapacityScoring, true)

	tests := []struct {
		name     string
		rawArgs  framework.Arguments
		expected *wrapVolumeBindingArgs
	}{
		{
			name: "correct configuration",
			rawArgs: framework.Arguments{
				"volumebinding.weight": 1,
				"volumebinding.shape": []interface{}{
					map[string]interface{}{
						"Utilization": 0,
						"score":       10,
					},
					map[string]interface{}{
						"Utilization": 40,
						"score":       8,
					},
					map[string]interface{}{
						"utilization": 80,
						"score":       4,
					},
					map[string]interface{}{
						"utilization": 100,
						"score":       0,
					},
				},
			},
			expected: &wrapVolumeBindingArgs{
				Weight: 1,
				VolumeBindingArgs: &kubeschedulerconfig.VolumeBindingArgs{
					BindTimeoutSeconds: defaultBindTimeoutSeconds,
					Shape: []kubeschedulerconfig.UtilizationShapePoint{
						{
							Utilization: 0,
							Score:       10,
						},
						{
							Utilization: 40,
							Score:       8,
						},
						{
							Utilization: 80,
							Score:       4,
						},
						{
							Utilization: 100,
							Score:       0,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vbArgs := defaultVolumeBindingArgs()
			setUpVolumeBindingArgs(vbArgs, tt.rawArgs)

			assert.Equal(t, tt.expected.Weight, vbArgs.Weight)
			assert.Equal(t, tt.expected.BindTimeoutSeconds, vbArgs.BindTimeoutSeconds)
			assert.Equal(t, tt.expected.Shape, vbArgs.Shape)
		})
	}
}
