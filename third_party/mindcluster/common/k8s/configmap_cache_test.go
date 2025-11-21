/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package k8s is using for the k8s operation.
*/
package k8s

import (
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type torNodeWithOneMinuteDelayTest struct {
	name              string
	namespace         string
	cmName            string
	want              *v1.ConfigMap
	wantErr           bool
	torNodeUpdateTime int64
	torNodeCache      *v1.ConfigMap
}

const (
	oneMinuteAgo      = 61
	testNamespace     = "test"
	testName          = "test"
	getConfigMapError = "getConfigMapError"
	configmap         = "configmap"
)

func buildTorNodeWithOneMinuteDelayTestCase() []torNodeWithOneMinuteDelayTest {
	tests := []torNodeWithOneMinuteDelayTest{
		{
			name:      "01-GetConfigMapWithRetry will return cm correctly",
			namespace: testNamespace,
			cmName:    testName,
			want:      &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testName, Namespace: testNamespace}},
			wantErr:   false,
		},
		{
			name:              "02-GetConfigMapWithRetry will return cm correctly with set torNodeUpdateTime",
			namespace:         testNamespace,
			cmName:            testName,
			want:              &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testName, Namespace: testNamespace}},
			wantErr:           false,
			torNodeUpdateTime: time.Now().Unix() - oneMinuteAgo,
		},
		{
			name:      "03-GetConfigMapWithRetry will return error",
			namespace: testNamespace,
			cmName:    getConfigMapError,
			want:      nil,
			wantErr:   true,
		},
		{
			name:              "04-GetConfigMapWithRetry will return NewNotFound error",
			namespace:         testNamespace,
			cmName:            getConfigMapError,
			want:              nil,
			wantErr:           true,
			torNodeUpdateTime: time.Now().Unix(),
		},
	}
	return tests
}

func TestGetTorNodeWithOneMinuteDelay(t *testing.T) {
	patch := gomonkey.ApplyFunc(
		GetConfigMap,
		func(client kubernetes.Interface, namespace, cmName string) (*v1.ConfigMap, error) {
			if cmName == getConfigMapError {
				return nil, errors.NewNotFound(v1.Resource(configmap), cmName)
			}
			return &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace}}, nil
		},
	)
	defer patch.Reset()

	tests := buildTorNodeWithOneMinuteDelayTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torNodeUpdateTimePatch := gomonkey.ApplyGlobalVar(&torNodeUpdateTime, tt.torNodeUpdateTime)
			defer torNodeUpdateTimePatch.Reset()
			torNodeCachePatch := gomonkey.ApplyGlobalVar(&torNodeCache, tt.torNodeCache)
			defer torNodeCachePatch.Reset()
			got, err := GetTorNodeWithOneMinuteDelay(nil, tt.namespace, tt.cmName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTorNodeWithOneMinuteDelay() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetTorNodeWithOneMinuteDelay() got = %v, want %v", got, tt.want)
			}
		})
	}
}
