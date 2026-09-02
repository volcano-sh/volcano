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

package helpers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestCreateOrUpdateConfigMap(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			UID:       "test-uid",
		},
	}

	data := map[string]string{
		"test.json": `{"key":"value"}`,
	}
	labels := map[string]string{
		"app": "test",
	}
	annotations := map[string]string{
		"version": "v1",
	}
	name := "test-configmap"

	// Test creating new ConfigMap
	err := CreateOrUpdateConfigMap(job, fakeClient, data, name, labels, annotations)
	if err != nil {
		t.Errorf("Failed to create ConfigMap: %v", err)
	}

	// Verify ConfigMap was created
	configMap, err := fakeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get created ConfigMap: %v", err)
	}

	// Verify properties
	if configMap.Name != name {
		t.Errorf("ConfigMap name mismatch, expected: %s, got: %s", name, configMap.Name)
	}

	if configMap.Namespace != "default" {
		t.Errorf("ConfigMap namespace mismatch, expected: default, got: %s", configMap.Namespace)
	}

	if configMap.Labels["app"] != "test" {
		t.Errorf("ConfigMap label mismatch, expected: test, got: %s", configMap.Labels["app"])
	}

	if configMap.Annotations["version"] != "v1" {
		t.Errorf("ConfigMap annotation mismatch, expected: v1, got: %s", configMap.Annotations["version"])
	}

	if configMap.Data["test.json"] != `{"key":"value"}` {
		t.Errorf("ConfigMap data mismatch, expected: {\"key\":\"value\"}, got: %s", configMap.Data["test.json"])
	}

	// Verify owner reference
	if len(configMap.OwnerReferences) != 1 {
		t.Errorf("Expected 1 owner reference, got %d", len(configMap.OwnerReferences))
	}

	ownerRef := configMap.OwnerReferences[0]
	if ownerRef.Name != "test-job" {
		t.Errorf("Owner reference name mismatch, expected: test-job, got: %s", ownerRef.Name)
	}

	// Test updating existing ConfigMap
	newData := map[string]string{
		"test.json": `{"key":"updated-value"}`,
	}
	newLabels := map[string]string{
		"app": "updated",
	}

	err = CreateOrUpdateConfigMap(job, fakeClient, newData, name, newLabels, nil)
	if err != nil {
		t.Errorf("Failed to update ConfigMap: %v", err)
	}

	// Verify ConfigMap was updated
	updatedConfigMap, err := fakeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get updated ConfigMap: %v", err)
	}

	if updatedConfigMap.Data["test.json"] != `{"key":"updated-value"}` {
		t.Errorf("Updated ConfigMap data mismatch, expected: {\"key\":\"updated-value\"}, got: %s", updatedConfigMap.Data["test.json"])
	}

	if updatedConfigMap.Labels["app"] != "updated" {
		t.Errorf("Updated ConfigMap label mismatch, expected: updated, got: %s", updatedConfigMap.Labels["app"])
	}
}
