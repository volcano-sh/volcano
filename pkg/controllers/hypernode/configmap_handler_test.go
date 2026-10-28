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

package hypernode

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	fakevcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"
)

// newFakeConfigMapController creates a test controller with fake clients
func newFakeConfigMapController() *hyperNodeController {
	vcClient := fakevcclientset.NewSimpleClientset()
	kubeClient := fake.NewSimpleClientset()

	vcInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, 0)
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	controller := &hyperNodeController{
		vcClient:          vcClient,
		kubeClient:        kubeClient,
		vcInformerFactory: vcInformerFactory,
		informerFactory:   informerFactory,
		configMapQueue:    workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}

	return controller
}

func TestSetConfigMapNamespaceAndName(t *testing.T) {
	testCases := []struct {
		name              string
		setupEnv          func()
		expectedNamespace string
		expectedName      string
	}{
		{
			name: "With environment variables set",
			setupEnv: func() {
				os.Setenv(config.NamespaceEnvKey, "test-namespace")
				os.Setenv(config.ReleaseNameEnvKey, "test-release")
			},
			expectedNamespace: "test-namespace",
			expectedName:      "test-release-controller-configmap",
		},
		{
			name: "With empty environment variables",
			setupEnv: func() {
				os.Unsetenv(config.NamespaceEnvKey)
				os.Unsetenv(config.ReleaseNameEnvKey)
			},
			expectedNamespace: config.DefaultNamespace,
			expectedName:      config.DefaultReleaseName + "-controller-configmap",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupEnv()

			controller := newFakeConfigMapController()
			controller.setConfigMapNamespaceAndName()

			assert.Equal(t, tc.expectedNamespace, controller.configMapNamespace)
			assert.Equal(t, tc.expectedName, controller.configMapName)
		})
	}
}

func TestSetupConfigMapInformer(t *testing.T) {
	controller := newFakeConfigMapController()

	controller.configMapNamespace = "test-namespace"
	controller.configMapName = "test-configmap"

	controller.setupConfigMapInformer()
	assert.NotNil(t, controller.configMapInformer)

	stopCh := make(chan struct{})
	defer close(stopCh)
	controller.informerFactory.Start(stopCh)
	synced := controller.informerFactory.WaitForCacheSync(stopCh)
	for informerType, ok := range synced {
		assert.True(t, ok, "Failed to sync informer: %v", informerType)
	}
}

func TestAddConfigMap(t *testing.T) {
	controller := newFakeConfigMapController()
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
	}

	controller.addConfigMap(configMap)
	assert.Equal(t, 1, controller.configMapQueue.Len())

	// Test invalid object (non-ConfigMap)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}
	controller.addConfigMap(pod)
	assert.Equal(t, 1, controller.configMapQueue.Len())
}

const testValidConfig = `
networkTopologyDiscovery:
  - source: ufm
    enabled: true
    config:
      url: "http://ufm-host:8080"
`

func TestUpdateConfigMap(t *testing.T) {
	testCases := []struct {
		name              string
		oldConfigMap      *v1.ConfigMap
		newConfigMap      *v1.ConfigMap
		expectedQueueSize int
	}{
		{
			name: "Unrelated key change does not trigger",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			expectedQueueSize: 0,
		},
		{
			name: "Unrelated change with unchanged network topology config does not trigger",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: testValidConfig,
					"key1":                  "value1",
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: testValidConfig,
					"key1":                  "value2",
				},
			},
			expectedQueueSize: 0,
		},
		{
			name: "Network topology config change triggers",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: testValidConfig,
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: `
networkTopologyDiscovery:
  - source: ufm
    enabled: false
    config:
      url: "http://ufm-host:8080"
`,
				},
			},
			expectedQueueSize: 1,
		},
		{
			name: "Network topology config key added triggers",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: testValidConfig,
					"key1":                  "value1",
				},
			},
			expectedQueueSize: 1,
		},
		{
			name: "Network topology config key removed triggers",
			oldConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					config.DefaultConfigKey: testValidConfig,
				},
			},
			newConfigMap: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{},
			},
			expectedQueueSize: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			controller := newFakeConfigMapController()
			controller.updateConfigMap(tc.oldConfigMap, tc.newConfigMap)
			assert.Equal(t, tc.expectedQueueSize, controller.configMapQueue.Len())
		})
	}

	// Test invalid object (non-ConfigMap)
	controller := newFakeConfigMapController()
	oldConfigMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}
	controller.updateConfigMap(oldConfigMap, pod)
	assert.Equal(t, 0, controller.configMapQueue.Len())
}

func TestDeleteConfigMap(t *testing.T) {
	controller := newFakeConfigMapController()

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
	}

	controller.deleteConfigMap(configMap)
	assert.Equal(t, 1, controller.configMapQueue.Len())

	// Test with DeletedFinalStateUnknown
	tombstone := cache.DeletedFinalStateUnknown{
		Key: "test-namespace/test-configmap-2",
		Obj: &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-configmap-2",
				Namespace: "test-namespace",
			},
		},
	}

	controller.deleteConfigMap(tombstone)
	assert.Equal(t, 2, controller.configMapQueue.Len())

	// Test invalid object (non-ConfigMap)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}
	controller.deleteConfigMap(pod)
	assert.Equal(t, 2, controller.configMapQueue.Len())

	// Test with DeletedFinalStateUnknown and invalid object
	invalidTombstone := cache.DeletedFinalStateUnknown{
		Key: "test-namespace/test-pod",
		Obj: pod,
	}
	controller.deleteConfigMap(invalidTombstone)
	assert.Equal(t, 2, controller.configMapQueue.Len())
}

func TestEnqueueConfigMap(t *testing.T) {
	controller := newFakeConfigMapController()

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
	}

	controller.enqueueConfigMap(configMap)

	assert.Equal(t, 1, controller.configMapQueue.Len())

	key, shutdown := controller.configMapQueue.Get()
	assert.False(t, shutdown)
	assert.Equal(t, "test-namespace/test-configmap", key)
}

func TestIntegrationConfigMapHandling(t *testing.T) {
	controller := newFakeConfigMapController()
	controller.configMapNamespace = "test-namespace"
	controller.configMapName = "test-configmap"

	controller.setupConfigMapInformer()
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			config.DefaultConfigKey: testValidConfig,
		},
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	controller.informerFactory.Start(stopCh)
	synced := controller.informerFactory.WaitForCacheSync(stopCh)
	for informerType, ok := range synced {
		assert.True(t, ok, "Failed to sync informer: %v", informerType)
	}

	_, err := controller.kubeClient.CoreV1().ConfigMaps("test-namespace").Create(
		context.Background(), configMap, metav1.CreateOptions{})
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return controller.configMapQueue.Len() == 1
	}, 5*time.Second, 10*time.Millisecond, "expected ConfigMap add to enqueue one key")

	// Process the queue
	key, _ := controller.configMapQueue.Get()
	controller.configMapQueue.Done(key)
	assert.Equal(t, 0, controller.configMapQueue.Len())

	// Unrelated change should not enqueue
	unrelatedUpdate := configMap.DeepCopy()
	unrelatedUpdate.Data["unrelated"] = "value"

	_, err = controller.kubeClient.CoreV1().ConfigMaps("test-namespace").Update(
		context.Background(), unrelatedUpdate, metav1.UpdateOptions{})
	assert.NoError(t, err)

	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, controller.configMapQueue.Len(), "unrelated ConfigMap change should not enqueue")

	// Network topology config change should enqueue
	relevantUpdate := configMap.DeepCopy()
	relevantUpdate.Data[config.DefaultConfigKey] = `
networkTopologyDiscovery:
  - source: ufm
    enabled: false
    config:
      url: "http://ufm-host:8080"
`

	_, err = controller.kubeClient.CoreV1().ConfigMaps("test-namespace").Update(
		context.Background(), relevantUpdate, metav1.UpdateOptions{})
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return controller.configMapQueue.Len() == 1
	}, 5*time.Second, 10*time.Millisecond, "expected network topology config change to enqueue one key")
}
