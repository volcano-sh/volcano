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

package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		configMapName  string
		configMaps     *corev1.ConfigMap
		expectedConfig *api.NetworkTopologyConfig
		expectedErr    error
	}{
		{
			name:          "successfully load config",
			namespace:     "test-ns",
			configMapName: "test-cm",
			configMaps: &corev1.ConfigMap{

				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"volcano-controller.conf": `
networkTopologyDiscovery:
  - source: ufm
    config:
      url: "http://ufm-host:8080"
      token: "test-token"
`,
				},
			},
			expectedConfig: &api.NetworkTopologyConfig{
				NetworkTopologyDiscovery: []api.DiscoveryConfig{
					{
						Source: "ufm",
						Config: map[string]interface{}{
							"url":   "http://ufm-host:8080",
							"token": "test-token",
						},
					},
				},
			},
		},
		{
			name:          "configmap not found",
			namespace:     "test-ns",
			configMapName: "non-existent-cm",
			configMaps:    &corev1.ConfigMap{},
			expectedErr:   apierrors.NewNotFound(schema.GroupResource{Resource: "configmap"}, "non-existent-cm"),
		},
		{
			name:          "config key not found",
			namespace:     "test-ns",
			configMapName: "test-cm",
			configMaps: &corev1.ConfigMap{

				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"not-exists-key": "content",
				},
			},
			expectedErr: errors.New(`config key not found in ConfigMap: volcano-controller.conf`),
		},
		{
			name:          "yaml parsing error",
			namespace:     "test-ns",
			configMapName: "test-cm",
			configMaps: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"volcano-controller.conf": "invalid yaml: :",
				},
			},
			expectedErr: errors.New(`failed to parse config: yaml: mapping values are not allowed in this context`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			indexer := cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			)
			if err := indexer.Add(test.configMaps); err != nil {
				t.Fatalf("failed to add object to indexer: %v", err)
			}
			configMapLister := v1.NewConfigMapLister(indexer)

			loader := NewConfigLoader(configMapLister, test.namespace, test.configMapName)
			config, err := loader.LoadConfig()

			assert.Equal(t, test.expectedErr, err)
			if !reflect.DeepEqual(config, test.expectedConfig) {
				t.Errorf("config mismatch\nexpected: %+v\nactual: %+v", test.expectedConfig, config)
			}
		})
	}
}
