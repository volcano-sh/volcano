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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func makePods() []*v1.Pod {
	return []*v1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "test-1", Namespace: "default"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "test-2", Namespace: "kube-system"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "test-3", Namespace: "default"}, Status: v1.PodStatus{Phase: v1.PodSucceeded}},
		{ObjectMeta: metav1.ObjectMeta{Name: "test-4", Namespace: "kube-system"}, Status: v1.PodStatus{Phase: v1.PodFailed}},
		{ObjectMeta: metav1.ObjectMeta{Name: "test-5", Namespace: "kube-system", DeletionTimestamp: &metav1.Time{Time: time.Now()}}},
	}
}

func TestConfiguration_GetActivePods(t *testing.T) {
	informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewSimpleClientset(), 0)
	objs := []runtime.Object{}
	for _, obj := range makePods() {
		assert.NoError(t, informerFactory.Core().V1().Pods().Informer().GetStore().Add(obj))
		objs = append(objs, obj)
	}
	fakeClient := fakeclientset.NewSimpleClientset(objs...)
	tests := []struct {
		name                 string
		GenericConfiguration *Configuration
		InformerFactory      *InformerFactory
		want                 map[string]struct{}
		wantErr              bool
	}{
		{
			name: "pod informer not synced",
			GenericConfiguration: &Configuration{
				GenericConfiguration: &VolcanoAgentConfiguration{
					KubeClient:    fakeClient,
					PodsHasSynced: func() bool { return false }},
			},
			want:    map[string]struct{}{"test-1": {}, "test-2": {}},
			wantErr: false,
		},
		{
			name: "pod informer synced",
			GenericConfiguration: &Configuration{
				GenericConfiguration: &VolcanoAgentConfiguration{
					PodsHasSynced: func() bool { return true }},
				InformerFactory: &InformerFactory{K8SInformerFactory: informerFactory},
			},
			want:    map[string]struct{}{"test-1": {}, "test-2": {}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.GenericConfiguration.GetActivePods()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetActivePods() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			pods := make(map[string]struct{})
			for _, pod := range got {
				pods[pod.Name] = struct{}{}
			}
			assert.Equalf(t, tt.want, pods, "GetActivePods()")
		})
	}
}
