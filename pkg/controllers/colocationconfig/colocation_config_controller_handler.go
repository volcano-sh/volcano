/*
Copyright 2026 The Volcano Authors.

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

package colocationconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	configv1alpha1 "volcano.sh/apis/pkg/apis/config/v1alpha1"
)

func (c *colocationConfigController) podHandler(obj interface{}) {
	var pod *corev1.Pod
	switch t := obj.(type) {
	case *corev1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*corev1.Pod)
		if !ok {
			klog.ErrorS(nil, "Failed to get Pod from DeletedFinalStateUnknown", "obj", t.Obj)
			return
		}
	default:
		klog.ErrorS(nil, "Unknown type", "obj", obj)
		return
	}

	if key, err := cache.MetaNamespaceKeyFunc(pod); err != nil {
		klog.ErrorS(err, "Failed to get key for pod", "pod", pod)
	} else {
		c.podQueue.AddAfter(key, time.Second)
	}
}

func (c *colocationConfigController) colocationConfigurationHandler(obj interface{}) {
	var cfg *configv1alpha1.ColocationConfiguration
	switch t := obj.(type) {
	case *configv1alpha1.ColocationConfiguration:
		cfg = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		cfg, ok = t.Obj.(*configv1alpha1.ColocationConfiguration)
		if !ok {
			klog.ErrorS(nil, "Failed to get ColocationConfiguration from DeletedFinalStateUnknown", "obj", t.Obj)
			return
		}
	default:
		klog.ErrorS(nil, "Unknown type", "obj", obj)
		return
	}

	selector, err := metav1.LabelSelectorAsSelector(cfg.Spec.Selector)
	if err != nil {
		klog.ErrorS(err, "Failed to convert LabelSelector to Selector", "selector", cfg.Spec.Selector)
		return
	}

	pods, err := c.podLister.Pods(cfg.Namespace).List(selector)
	if err != nil {
		klog.ErrorS(err, "Failed to list pods", "selector", selector)
		return
	}

	for _, pod := range pods {
		if key, err := cache.MetaNamespaceKeyFunc(pod); err != nil {
			klog.ErrorS(err, "Failed to get key for pod", "pod", pod)
		} else {
			c.podQueue.AddAfter(key, time.Second)
		}
	}
}

func (c *colocationConfigController) processPodQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			key, quit := c.podQueue.Get()
			if quit {
				return
			}

			func() {
				defer c.podQueue.Done(key)

				if err := c.syncPod(ctx, key); err != nil {
					klog.ErrorS(err, "Failed to sync pod", "key", key)
					c.podQueue.AddRateLimited(key)
					return
				}
				c.podQueue.Forget(key)
			}()
		}
	}
}

func (c *colocationConfigController) syncPod(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("failed to split meta namespace key: %w", err)
	}

	pod, err := c.podLister.Pods(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get pod: %w", err)
	}

	coloConfigs, err := c.coloConfigLister.ColocationConfigurations(namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list colocation configs: %w", err)
	}
	slices.SortFunc(coloConfigs, func(a, b *configv1alpha1.ColocationConfiguration) int {
		return b.CreationTimestamp.Time.Compare(a.CreationTimestamp.Time) // sort by creation time descending to apply newest first
	})

	for _, coloConfig := range coloConfigs {
		selector, err := metav1.LabelSelectorAsSelector(coloConfig.Spec.Selector)
		if err != nil {
			klog.Warningf("Failed to convert LabelSelector to Selector. coloConfig: %v", klog.KObj(coloConfig))
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			if err := c.updateColocationConfigToPod(ctx, pod, coloConfig); err != nil {
				return fmt.Errorf("failed to update colocation config to pod: %w", err)
			}
			return nil
		}
	}

	if err = c.resetColocationConfigForPod(ctx, pod); err != nil {
		return fmt.Errorf("failed to cleanup colocation config for pod: %w", err)
	}

	return nil
}

func (c *colocationConfigController) updateColocationConfigToPod(ctx context.Context, pod *corev1.Pod, coloConfig *configv1alpha1.ColocationConfiguration) error {
	podModified, needUpdate, err := getModifiedPod(pod, coloConfig)
	if err != nil {
		return fmt.Errorf("failed to get modified pod: %w", err)
	}
	if !needUpdate {
		klog.V(5).InfoS("Pod does not need update", "pod", klog.KObj(pod), "coloConfig", klog.KObj(coloConfig))
		return nil
	}

	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err = c.kubeClient.CoreV1().Pods(podModified.Namespace).Update(timeout, podModified, metav1.UpdateOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to update pod: %w", err)
	}

	klog.V(3).InfoS("Successfully updated colocation config for pod", "pod", klog.KObj(podModified), "coloConfig", klog.KObj(coloConfig))

	return nil
}

func getModifiedPod(pod *corev1.Pod, coloConfig *configv1alpha1.ColocationConfiguration) (*corev1.Pod, bool, error) {
	needUpdate := func() bool {
		if pod.Annotations == nil {
			return true
		}
		if pod.Annotations[configv1alpha1.ColocationConfigNameKey] != coloConfig.Name {
			return true
		}
		oldData, ok := pod.Annotations[configv1alpha1.ColocationConfigKey]
		if !ok {
			return true
		}
		var oldOptions configv1alpha1.Configuration
		if err := json.Unmarshal([]byte(oldData), &oldOptions); err != nil {
			return true
		}
		return !equality.Semantic.DeepEqual(oldOptions, coloConfig.Spec.Configuration)
	}()

	if !needUpdate {
		return pod, false, nil
	}

	data, err := json.Marshal(&coloConfig.Spec.Configuration)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal colocation config: %w", err)
	}

	podCopy := pod.DeepCopy()
	if podCopy.Annotations == nil {
		podCopy.Annotations = make(map[string]string)
	}
	podCopy.Annotations[configv1alpha1.ColocationConfigNameKey] = coloConfig.Name
	podCopy.Annotations[configv1alpha1.ColocationConfigKey] = string(data)

	return podCopy, true, nil
}

func (c *colocationConfigController) resetColocationConfigForPod(ctx context.Context, pod *corev1.Pod) error {
	if pod.Annotations == nil {
		return nil
	}

	_, ok1 := pod.Annotations[configv1alpha1.ColocationConfigNameKey]
	_, ok2 := pod.Annotations[configv1alpha1.ColocationConfigKey]
	if !ok1 && !ok2 {
		return nil
	}

	podCopy := pod.DeepCopy()
	delete(podCopy.Annotations, configv1alpha1.ColocationConfigNameKey)
	podCopy.Annotations[configv1alpha1.ColocationConfigKey] = configv1alpha1.ColocationConfigReset // Keep the key here to indicate that the pod was once managed by colocation config and need to reset the cgroup settings
	if equality.Semantic.DeepEqual(podCopy.Annotations, pod.Annotations) {
		return nil
	}

	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.kubeClient.CoreV1().Pods(podCopy.Namespace).Update(timeout, podCopy, metav1.UpdateOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to reset colocation config for pod: %w", err)
	}

	klog.V(3).InfoS("Successfully reset colocation config for pod", "pod", klog.KObj(podCopy))
	return nil
}
