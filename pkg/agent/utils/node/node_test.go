/*
Copyright 2024 The Volcano Authors.

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

package node

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/agent/apis"
)

func TestWatermarkAnnotationSetting(t *testing.T) {
	tests := []struct {
		name          string
		node          *corev1.Node
		lowWatermark  apis.Resource
		highWatermark apis.Resource
		exists        bool
		wantErr       assert.ErrorAssertionFunc
	}{
		{
			name: "not exists",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			lowWatermark:  nil,
			highWatermark: nil,
			exists:        false,
			wantErr:       assert.NoError,
		},
		{
			name: "negative watermark",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						apis.PodEvictedMemoryLowWaterMarkKey: "-1",
					},
				},
			},
			lowWatermark:  nil,
			highWatermark: nil,
			exists:        true,
			wantErr:       assert.Error,
		},
		{
			name: "invalid watermark, setting one annotation",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						apis.PodEvictedCPULowWaterMarkKey: "90",
					},
				},
			},
			lowWatermark:  nil,
			highWatermark: nil,
			exists:        true,
			wantErr:       assert.Error,
		},
		{
			name: "invalid watermark, setting two annotations",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						apis.PodEvictedOverSubscriptionMemoryHighWaterMarkKey: "10",
						apis.PodEvictedMemoryLowWaterMarkKey:                  "20",
					},
				},
			},
			lowWatermark:  nil,
			highWatermark: nil,
			exists:        true,
			wantErr:       assert.Error,
		},
		{
			name: "valid watermark",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						apis.PodEvictedCPULowWaterMarkKey:                    "10",
						apis.PodEvictedOverSubscriptionCPUHighWaterMarkKey:   "80",
						apis.PodEvictedOverSubscriptionMemoryLowWaterMarkKey: "20",
						apis.PodEvictedMemoryHighWaterMarkKey:                "30",
					},
				},
			},
			lowWatermark: apis.Resource{
				corev1.ResourceCPU:    10,
				corev1.ResourceMemory: 20,
			},
			highWatermark: apis.Resource{
				corev1.ResourceCPU:    80,
				corev1.ResourceMemory: 30,
			},
			exists:  true,
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, err := WatermarkAnnotationSetting(tt.node)
			if !tt.wantErr(t, err, fmt.Sprintf("WatermarkAnnotationSetting(%v)", tt.node)) {
				return
			}
			assert.Equalf(t, tt.lowWatermark, got, "WatermarkAnnotationSetting(%v)", tt.node)
			assert.Equalf(t, tt.highWatermark, got1, "WatermarkAnnotationSetting(%v)", tt.node)
			assert.Equalf(t, tt.exists, got2, "WatermarkAnnotationSetting(%v)", tt.node)
		})
	}
}

func TestIsNodeSupportColocation(t *testing.T) {
	testCases := []struct {
		name           string
		node           *corev1.Node
		expectedResult bool
	}{
		{
			name: "missing colocation label && missing oversubscription label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			expectedResult: false,
		},

		{
			name: "colocation label set false",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						apis.ColocationEnableNodeLabelKey: "false",
					},
				},
			},
			expectedResult: false,
		},

		{
			name: "colocation label set true",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						apis.ColocationEnableNodeLabelKey: "true",
					},
				},
			},
			expectedResult: true,
		},

		{
			name: "oversubscription label set true",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						apis.OverSubscriptionNodeLabelKey: "true",
					},
				},
			},
			expectedResult: true,
		},

		{
			name: "oversubscription label set false",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						apis.OverSubscriptionNodeLabelKey: "false",
					},
				},
			},
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		actualResult := IsNodeSupportColocation(tc.node) || IsNodeSupportOverSubscription(tc.node)
		assert.Equal(t, actualResult, tc.expectedResult, tc.name)
	}
}
