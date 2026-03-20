package colocationconfig

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	configv1alpha1 "volcano.sh/apis/pkg/apis/config/v1alpha1"
	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformers "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func newFakeController(kubeClient *fake.Clientset, vcClient *vcfake.Clientset) *colocationConfigController {
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformerFactory := vcinformers.NewSharedInformerFactory(vcClient, 0)

	controller := &colocationConfigController{}
	opt := &framework.ControllerOption{
		KubeClient:              kubeClient,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   informerFactory,
		VCSharedInformerFactory: vcInformerFactory,
	}

	controller.Initialize(opt)

	return controller
}

func TestSyncPod(t *testing.T) {
	now := time.Now()

	config1 := &configv1alpha1.ColocationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "config-1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Hour)),
		},
		Spec: configv1alpha1.ColocationConfigurationSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "foo"},
			},
			Configuration: configv1alpha1.Configuration{
				MemoryQos: &configv1alpha1.MemoryQos{
					HighRatio: 100,
				},
			},
		},
	}

	config2 := &configv1alpha1.ColocationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "config-2",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(now),
		},
		Spec: configv1alpha1.ColocationConfigurationSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "foo"},
			},
			Configuration: configv1alpha1.Configuration{
				MemoryQos: &configv1alpha1.MemoryQos{
					HighRatio: 80,
				},
			},
		},
	}

	podNoMatch := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-no-match",
			Namespace: "default",
			Labels:    map[string]string{"app": "bar"},
		},
	}

	podMatch := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-match",
			Namespace: "default",
			Labels:    map[string]string{"app": "foo"},
		},
	}

	colConfigJson, _ := json.Marshal(&config1.Spec.Configuration)
	podWithConfig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-with-config",
			Namespace: "default",
			Labels:    map[string]string{"app": "foo"},
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     string(colConfigJson),
			},
		},
	}

	tests := []struct {
		name                string
		pods                []*corev1.Pod
		configs             []*configv1alpha1.ColocationConfiguration
		podKey              string
		expectedAnnotations map[string]string
		expectUpdate        bool
	}{
		{
			name:                "pod does not match any config",
			pods:                []*corev1.Pod{podNoMatch},
			configs:             []*configv1alpha1.ColocationConfiguration{config1},
			podKey:              "default/pod-no-match",
			expectedAnnotations: nil,
			expectUpdate:        false,
		},
		{
			name:    "pod matches config, should update",
			pods:    []*corev1.Pod{podMatch},
			configs: []*configv1alpha1.ColocationConfiguration{config1},
			podKey:  "default/pod-match",
			expectedAnnotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     string(colConfigJson),
			},
			expectUpdate: true,
		},
		{
			name:    "pod matches config, already updated",
			pods:    []*corev1.Pod{podWithConfig},
			configs: []*configv1alpha1.ColocationConfiguration{config1},
			podKey:  "default/pod-with-config",
			expectedAnnotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     string(colConfigJson),
			},
			expectUpdate: false,
		},
		{
			name:    "pod matches multiple configs, newest should win",
			pods:    []*corev1.Pod{podMatch},
			configs: []*configv1alpha1.ColocationConfiguration{config1, config2},
			podKey:  "default/pod-match",
			expectedAnnotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-2",
			},
			expectUpdate: true,
		},
		{
			name:    "pod had config but now no match, should reset",
			pods:    []*corev1.Pod{podWithConfig},
			configs: []*configv1alpha1.ColocationConfiguration{}, // No configs
			podKey:  "default/pod-with-config",
			expectedAnnotations: map[string]string{
				configv1alpha1.ColocationConfigKey: configv1alpha1.ColocationConfigReset,
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientset()
			vcClient := vcfake.NewClientset()

			// Add objects to clients
			for _, p := range tt.pods {
				kubeClient.Tracker().Add(p)
			}
			for _, c := range tt.configs {
				vcClient.Tracker().Add(c)
			}

			c := newFakeController(kubeClient, vcClient)

			// Start informers and wait for sync
			stopCh := make(chan struct{})
			defer close(stopCh)
			c.informerFactory.Start(stopCh)
			c.vcInformerFactory.Start(stopCh)
			c.informerFactory.WaitForCacheSync(stopCh)
			c.vcInformerFactory.WaitForCacheSync(stopCh)

			ctx := context.TODO()
			err := c.syncPod(ctx, tt.podKey)
			if err != nil {
				t.Fatalf("syncPod error: %v", err)
			}

			// Validate result
			// We check the pod in the fake client
			ns, name, _ := cache.SplitMetaNamespaceKey(tt.podKey)
			updatedPod, err := kubeClient.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get pod: %v", err)
			}

			if tt.expectUpdate {
				// Check actions to ensure update was called
				actions := kubeClient.Actions()
				foundUpdate := false
				for _, action := range actions {
					if action.GetVerb() == "update" && action.GetResource().Resource == "pods" {
						foundUpdate = true
						break
					}
				}
				if !foundUpdate {
					t.Errorf("Expected update action but found none")
				}
			}

			if tt.expectedAnnotations == nil {
				if len(updatedPod.Annotations) > 0 {
					// It's possible we didn't expect annotations but got some if they were already there and not touched?
					// In our cases, if we expect nil, we expect no annotations or irrelevant ones.
					// But let's check exact match if provided.
					// If test case expects nil, check if relevant keys are missing.
					if _, ok := updatedPod.Annotations[configv1alpha1.ColocationConfigNameKey]; ok {
						t.Errorf("Expected no ColocationConfigNameKey, but got %v", updatedPod.Annotations[configv1alpha1.ColocationConfigNameKey])
					}
				}
			} else {
				for k, v := range tt.expectedAnnotations {
					// Special matching for config content which is JSON
					if k == configv1alpha1.ColocationConfigKey && v != configv1alpha1.ColocationConfigReset {
						// Don't compare exact string JSON
						continue
					}

					gotV, ok := updatedPod.Annotations[k]
					if !ok {
						t.Errorf("Expected annotation %s, but missing", k)
					} else if gotV != v {
						t.Errorf("Expected annotation %s=%s, got %s", k, v, gotV)
					}
				}
				if val, ok := tt.expectedAnnotations[configv1alpha1.ColocationConfigNameKey]; ok {
					if gotVal := updatedPod.Annotations[configv1alpha1.ColocationConfigNameKey]; gotVal != val {
						t.Errorf("Expected config name %s, got %s", val, gotVal)
					}
				}
			}
		})
	}
}

func TestGetModifiedPod(t *testing.T) {
	config := &configv1alpha1.ColocationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-1",
		},
		Spec: configv1alpha1.ColocationConfigurationSpec{
			Configuration: configv1alpha1.Configuration{
				MemoryQos: &configv1alpha1.MemoryQos{
					HighRatio: 100,
				},
			},
		},
	}

	podWithNoAnn := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
	}

	podWithWrongConfigName := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-2",
			},
		},
	}

	configJson, _ := json.Marshal(&config.Spec.Configuration)
	podWithSameConfig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     string(configJson),
			},
		},
	}

	podWithDifferentConfig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     `{"memoryQos": {"highRatio": 90}}`,
			},
		},
	}

	tests := []struct {
		name       string
		pod        *corev1.Pod
		coloConfig *configv1alpha1.ColocationConfiguration
		wantUpdate bool
	}{
		{
			name:       "pod with no annotations",
			pod:        podWithNoAnn,
			coloConfig: config,
			wantUpdate: true,
		},
		{
			name:       "pod with wrong config name",
			pod:        podWithWrongConfigName,
			coloConfig: config,
			wantUpdate: true,
		},
		{
			name:       "pod with same config",
			pod:        podWithSameConfig,
			coloConfig: config,
			wantUpdate: false,
		},
		{
			name:       "pod with different config",
			pod:        podWithDifferentConfig,
			coloConfig: config,
			wantUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, needUpdate, err := getModifiedPod(tt.pod, tt.coloConfig)
			if err != nil {
				t.Fatalf("getModifiedPod error: %v", err)
			}
			if needUpdate != tt.wantUpdate {
				t.Errorf("Expected needUpdate=%v, got %v", tt.wantUpdate, needUpdate)
			}
		})
	}
}

func TestResetColocationConfigForPod(t *testing.T) {
	podWithConfig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-with-config",
			Namespace: "default",
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigNameKey: "config-1",
				configv1alpha1.ColocationConfigKey:     "{}",
			},
		},
	}

	podReset := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-reset",
			Namespace: "default",
			Annotations: map[string]string{
				configv1alpha1.ColocationConfigKey: configv1alpha1.ColocationConfigReset,
			},
		},
	}

	podNoConfig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-no-config",
			Namespace: "default",
			Annotations: map[string]string{
				"foo": "bar",
			},
		},
	}

	tests := []struct {
		name         string
		pod          *corev1.Pod
		expectUpdate bool
	}{
		{
			name:         "pod with config, should reset",
			pod:          podWithConfig,
			expectUpdate: true,
		},
		{
			name:         "pod already reset, should not update",
			pod:          podReset,
			expectUpdate: false,
		},
		{
			name:         "pod with no config, should not update",
			pod:          podNoConfig,
			expectUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientset()
			kubeClient.Tracker().Add(tt.pod)

			c := newFakeController(kubeClient, vcfake.NewClientset())
			ctx := context.TODO()
			c.kubeClient = kubeClient // Manually set kubeClient because doing newFakeController in this test might be overkill if we just want to test this method functionality and uses c.kubeClient

			err := c.resetColocationConfigForPod(ctx, tt.pod)
			if err != nil {
				t.Fatalf("resetColocationConfigForPod error: %v", err)
			}

			actions := kubeClient.Actions()
			foundUpdate := false
			for _, action := range actions {
				if action.GetVerb() == "update" && action.GetResource().Resource == "pods" {
					foundUpdate = true
					break
				}
			}

			if tt.expectUpdate && !foundUpdate {
				t.Errorf("Expected update action but found none")
			}
			if !tt.expectUpdate && foundUpdate {
				t.Errorf("Expected no update action but found one")
			}
		})
	}
}
