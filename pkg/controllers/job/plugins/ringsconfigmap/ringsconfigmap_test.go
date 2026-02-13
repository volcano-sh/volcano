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

package ringsconfigmap

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func TestRingsConfigMapPlugin_OnPodCreate(t *testing.T) {
	plugin := New(pluginsinterface.PluginClientset{KubeClients: fake.NewSimpleClientset()}, []string{})

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "test-container",
				},
			},
		},
	}

	// Test adding volume and volumeMount
	err := plugin.OnPodCreate(pod, job)
	if err != nil {
		t.Errorf("OnPodCreate failed: %v", err)
	}

	// Check if volume was added
	volumeFound := false
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "ascend-910-config" {
			volumeFound = true
			if volume.ConfigMap == nil || volume.ConfigMap.Name != "rings-config-test-job" {
				t.Errorf("Volume ConfigMap name mismatch, expected: rings-config-test-job, got: %s", volume.ConfigMap.Name)
			}
			break
		}
	}
	if !volumeFound {
		t.Error("Volume 'ascend-910-config' was not added")
	}

	// Check if volumeMount was added
	volumeMountFound := false
	for _, volumeMount := range pod.Spec.Containers[0].VolumeMounts {
		if volumeMount.Name == "ascend-910-config" {
			volumeMountFound = true
			if volumeMount.MountPath != "/user/serverid/devindex/config" {
				t.Errorf("VolumeMount path mismatch, expected: /user/serverid/devindex/config, got: %s", volumeMount.MountPath)
			}
			break
		}
	}
	if !volumeMountFound {
		t.Error("VolumeMount 'ascend-910-config' was not added")
	}

	// Test idempotency - should not add duplicate volume and volumeMount
	originalVolumeCount := len(pod.Spec.Volumes)
	originalVolumeMountCount := len(pod.Spec.Containers[0].VolumeMounts)

	err = plugin.OnPodCreate(pod, job)
	if err != nil {
		t.Errorf("OnPodCreate failed on second call: %v", err)
	}

	if len(pod.Spec.Volumes) != originalVolumeCount {
		t.Errorf("Volume count changed on second call, expected: %d, got: %d", originalVolumeCount, len(pod.Spec.Volumes))
	}

	if len(pod.Spec.Containers[0].VolumeMounts) != originalVolumeMountCount {
		t.Errorf("VolumeMount count changed on second call, expected: %d, got: %d", originalVolumeMountCount, len(pod.Spec.Containers[0].VolumeMounts))
	}
}

func TestRingsConfigMapPlugin_WithCustomConfig(t *testing.T) {
	// Test with custom configuration
	arguments := []string{
		"-configmap-name-template", "custom-config-{{.Name}}-{{.Namespace}}",
		"-volume-name", "custom-volume",
		"-mount-path", "/custom/path",
	}

	plugin := New(pluginsinterface.PluginClientset{KubeClients: fake.NewSimpleClientset()}, arguments)

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "test-container",
				},
			},
		},
	}

	// Test adding volume and volumeMount with custom config
	err := plugin.OnPodCreate(pod, job)
	if err != nil {
		t.Errorf("OnPodCreate failed: %v", err)
	}

	// Check if volume was added with custom name
	volumeFound := false
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "custom-volume" {
			volumeFound = true
			expectedConfigMapName := "custom-config-test-job-test-ns"
			if volume.ConfigMap == nil || volume.ConfigMap.Name != expectedConfigMapName {
				t.Errorf("Volume ConfigMap name mismatch, expected: %s, got: %s", expectedConfigMapName, volume.ConfigMap.Name)
			}
			break
		}
	}
	if !volumeFound {
		t.Error("Volume 'custom-volume' was not added")
	}

	// Check if volumeMount was added with custom path
	volumeMountFound := false
	for _, volumeMount := range pod.Spec.Containers[0].VolumeMounts {
		if volumeMount.Name == "custom-volume" {
			volumeMountFound = true
			if volumeMount.MountPath != "/custom/path" {
				t.Errorf("VolumeMount path mismatch, expected: /custom/path, got: %s", volumeMount.MountPath)
			}
			break
		}
	}
	if !volumeMountFound {
		t.Error("VolumeMount 'custom-volume' was not added")
	}
}

func TestRingsConfigMapPlugin_Name(t *testing.T) {
	plugin := New(pluginsinterface.PluginClientset{KubeClients: fake.NewSimpleClientset()}, []string{})

	expectedName := "ringsconfigmap"
	if plugin.Name() != expectedName {
		t.Errorf("Plugin name mismatch, expected: %s, got: %s", expectedName, plugin.Name())
	}
}

func TestConfig_ParseArguments(t *testing.T) {
	config := DefaultConfig()

	// Test with custom arguments
	arguments := []string{
		"-configmap-name-template", "my-config-{{.Name}}",
		"-volume-name", "my-volume",
		"-mount-path", "/my/path",
	}

	err := config.ParseArguments(arguments)
	if err != nil {
		t.Errorf("ParseArguments failed: %v", err)
	}

	if config.ConfigMapNameTemplate != "my-config-{{.Name}}" {
		t.Errorf("ConfigMapNameTemplate mismatch, expected: my-config-{{.Name}}, got: %s", config.ConfigMapNameTemplate)
	}

	if config.VolumeName != "my-volume" {
		t.Errorf("VolumeName mismatch, expected: my-volume, got: %s", config.VolumeName)
	}

	if config.MountPath != "/my/path" {
		t.Errorf("MountPath mismatch, expected: /my/path, got: %s", config.MountPath)
	}
}

func TestConfig_GetConfigMapName(t *testing.T) {
	config := DefaultConfig()

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	name, err := config.GetConfigMapName(job)
	if err != nil {
		t.Errorf("GetConfigMapName failed: %v", err)
	}

	expectedName := "rings-config-test-job"
	if name != expectedName {
		t.Errorf("ConfigMap name mismatch, expected: %s, got: %s", expectedName, name)
	}

	// Test with custom template
	config.ConfigMapNameTemplate = "custom-{{.Name}}-{{.Namespace}}"
	// Recompile template after changing the template string
	if err := config.compileTemplate(); err != nil {
		t.Errorf("Failed to compile custom template: %v", err)
	}

	name, err = config.GetConfigMapName(job)
	if err != nil {
		t.Errorf("GetConfigMapName failed with custom template: %v", err)
	}

	expectedName = "custom-test-job-default"
	if name != expectedName {
		t.Errorf("ConfigMap name mismatch with custom template, expected: %s, got: %s", expectedName, name)
	}
}

func TestRingsConfigMapPlugin_OnJobAdd(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	plugin := New(pluginsinterface.PluginClientset{KubeClients: fakeClient}, []string{})

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			UID:       "test-uid",
		},
		Status: batch.JobStatus{
			ControlledResources: make(map[string]string),
		},
	}

	// Test first call - should create ConfigMap
	err := plugin.OnJobAdd(job)
	if err != nil {
		t.Errorf("OnJobAdd failed: %v", err)
	}

	// Verify ConfigMap was created
	configMap, err := fakeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), "rings-config-test-job", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get created ConfigMap: %v", err)
	}

	// Verify ConfigMap properties
	if configMap.Name != "rings-config-test-job" {
		t.Errorf("ConfigMap name mismatch, expected: rings-config-test-job, got: %s", configMap.Name)
	}

	if configMap.Namespace != "default" {
		t.Errorf("ConfigMap namespace mismatch, expected: default, got: %s", configMap.Namespace)
	}

	// Verify labels
	expectedLabel := "ascend-910b"
	if configMap.Labels["ring-controller.atlas"] != expectedLabel {
		t.Errorf("ConfigMap label mismatch, expected: %s, got: %s", expectedLabel, configMap.Labels["ring-controller.atlas"])
	}

	// Verify data
	expectedData := `{
        "status":"initializing"
    }`
	if configMap.Data["hccl.json"] != expectedData {
		t.Errorf("ConfigMap data mismatch, expected: %s, got: %s", expectedData, configMap.Data["hccl.json"])
	}

	// Verify owner reference
	if len(configMap.OwnerReferences) != 1 {
		t.Errorf("Expected 1 owner reference, got %d", len(configMap.OwnerReferences))
	}

	ownerRef := configMap.OwnerReferences[0]
	if ownerRef.Name != "test-job" {
		t.Errorf("Owner reference name mismatch, expected: test-job, got: %s", ownerRef.Name)
	}

	// Verify job status was updated
	if job.Status.ControlledResources["plugin-ringsconfigmap"] != "ringsconfigmap" {
		t.Errorf("Job status not updated correctly")
	}

	// Test idempotency - second call should not create duplicate ConfigMap
	err = plugin.OnJobAdd(job)
	if err != nil {
		t.Errorf("OnJobAdd failed on second call: %v", err)
	}

	// Verify only one ConfigMap exists
	configMaps, err := fakeClient.CoreV1().ConfigMaps("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("Failed to list ConfigMaps: %v", err)
	}

	if len(configMaps.Items) != 1 {
		t.Errorf("Expected 1 ConfigMap, got %d", len(configMaps.Items))
	}
}

func TestRingsConfigMapPlugin_OnJobDelete(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	plugin := New(pluginsinterface.PluginClientset{KubeClients: fakeClient}, []string{})

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
		Status: batch.JobStatus{
			ControlledResources: map[string]string{
				"plugin-ringsconfigmap": "ringsconfigmap",
			},
		},
	}

	// Create a ConfigMap first
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rings-config-test-job",
			Namespace: "default",
		},
		Data: map[string]string{
			"hccl.json": `{"status":"initializing"}`,
		},
	}

	_, err := fakeClient.CoreV1().ConfigMaps("default").Create(context.TODO(), configMap, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Failed to create test ConfigMap: %v", err)
	}

	// Test OnJobDelete - should delete ConfigMap
	err = plugin.OnJobDelete(job)
	if err != nil {
		t.Errorf("OnJobDelete failed: %v", err)
	}

	// Verify ConfigMap was deleted
	_, err = fakeClient.CoreV1().ConfigMaps("default").Get(context.TODO(), "rings-config-test-job", metav1.GetOptions{})
	if err == nil {
		t.Error("ConfigMap should have been deleted")
	}

	// Verify plugin state was removed from job status
	if _, exists := job.Status.ControlledResources["plugin-ringsconfigmap"]; exists {
		t.Error("Plugin state should have been removed from job status")
	}

	// Test idempotency - second call should not error
	err = plugin.OnJobDelete(job)
	if err != nil {
		t.Errorf("OnJobDelete failed on second call: %v", err)
	}
}

func TestRingsConfigMapPlugin_OnJobDelete_NotProcessed(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	plugin := New(pluginsinterface.PluginClientset{KubeClients: fakeClient}, []string{})

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
		Status: batch.JobStatus{
			ControlledResources: map[string]string{
				// Plugin not marked as processed
			},
		},
	}

	// Test OnJobDelete when plugin was not processed - should do nothing
	err := plugin.OnJobDelete(job)
	if err != nil {
		t.Errorf("OnJobDelete failed: %v", err)
	}

	// Verify job status was not modified
	if len(job.Status.ControlledResources) != 0 {
		t.Error("Job status should not have been modified")
	}
}

func TestConfig_ParseArguments_WithLabelsAndData(t *testing.T) {
	config := DefaultConfig()

	// Test with custom labels, data, and annotations
	arguments := []string{
		"-configmap-labels", `{"custom-label":"custom-value","app":"test"}`,
		"-configmap-data", `{"custom.json":"{\"key\":\"value\"}","config.yaml":"apiVersion: v1"}`,
		"-configmap-annotations", `{"version":"v1.0","team":"ai"}`,
	}

	err := config.ParseArguments(arguments)
	if err != nil {
		t.Errorf("ParseArguments failed: %v", err)
	}

	// Verify labels
	if config.ConfigMapLabels["custom-label"] != "custom-value" {
		t.Errorf("Custom label not set correctly")
	}
	if config.ConfigMapLabels["app"] != "test" {
		t.Errorf("App label not set correctly")
	}

	// Verify data
	if config.ConfigMapData["custom.json"] != `{"key":"value"}` {
		t.Errorf("Custom data not set correctly")
	}
	if config.ConfigMapData["config.yaml"] != "apiVersion: v1" {
		t.Errorf("Config data not set correctly")
	}

	// Verify annotations
	if config.ConfigMapAnnotations["version"] != "v1.0" {
		t.Errorf("Version annotation not set correctly")
	}
	if config.ConfigMapAnnotations["team"] != "ai" {
		t.Errorf("Team annotation not set correctly")
	}
}

func TestConfig_ParseArguments_InvalidJSON(t *testing.T) {
	config := DefaultConfig()

	// Test with invalid JSON
	arguments := []string{
		"-configmap-labels", `{"invalid-json"`,
	}

	err := config.ParseArguments(arguments)
	if err == nil {
		t.Error("Expected error for invalid JSON, but got none")
	}
}

func TestPlugin_InitializationFailure(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Test with invalid arguments that should cause parsing failure but plugin continues with default config
	arguments := []string{
		"-configmap-name-template", "", // Empty template should fail validation
	}

	plugin := New(pluginsinterface.PluginClientset{KubeClients: fakeClient}, arguments)

	// Plugin should not be nil even when parsing fails, as it falls back to default config
	if plugin == nil {
		t.Error("Plugin should not be nil when parsing fails, should use default config")
	}

	// Verify that plugin uses default config when parsing fails
	if plugin.Name() != "ringsconfigmap" {
		t.Errorf("Plugin name should be 'ringsconfigmap', got: %s", plugin.Name())
	}
}
