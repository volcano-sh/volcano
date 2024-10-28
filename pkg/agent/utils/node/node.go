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
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/utils/eviction"
)

// ActiveNode returns node which is managed by kubelet
type ActiveNode func() (*corev1.Node, error)

type ResourceList struct {
	TotalPodsRequest corev1.ResourceList
	TotalNodeRes     corev1.ResourceList
}

func IsNodeSupportColocation(node *corev1.Node) bool {
	b, err := strconv.ParseBool(node.Labels[apis.ColocationEnableNodeLabelKey])
	return err == nil && b
}

// IsNodeSupportOverSubscription return whether a node is over subscription node.
// IMPORTANT!!! When node has a overSubscription label, it indicates that node is a colocation node too,
// because overSubscription resources are used by low priority workloads, it must be used in colocation case.
func IsNodeSupportOverSubscription(node *corev1.Node) bool {
	b, err := strconv.ParseBool(node.Labels[apis.OverSubscriptionNodeLabelKey])
	return err == nil && b
}

// WatermarkAnnotationSetting return watermark setting in annotation.
// return params lowWatermark, highWatermark, whether annotation exits and error
func WatermarkAnnotationSetting(node *corev1.Node) (apis.Resource, apis.Resource, bool, error) {
	waterMarks := apis.Resource{
		apis.PodEvictedCPUHighWaterMarkKey:    0,
		apis.PodEvictedCPULowWaterMarkKey:     0,
		apis.PodEvictedMemoryHighWaterMarkKey: 0,
		apis.PodEvictedMemoryLowWaterMarkKey:  0,
	}

	annotationExist := false
	for key := range waterMarks {
		value, exist, err := GetIntValueWaterMark(node.Annotations, string(key))
		if err != nil {
			klog.ErrorS(err, "Failed to get watermark", "key", key)
			return nil, nil, true, err
		}
		if exist {
			annotationExist = true
		}
		waterMarks[key] = value
	}
	if !annotationExist {
		return nil, nil, false, nil
	}

	if waterMarks[apis.PodEvictedCPUHighWaterMarkKey] <= waterMarks[apis.PodEvictedCPULowWaterMarkKey] {
		return nil, nil, true, fmt.Errorf("cpu low watermark is higher than high watermark, low: %v, high: %v", waterMarks[apis.PodEvictedCPULowWaterMarkKey], waterMarks[apis.PodEvictedCPUHighWaterMarkKey])
	}

	if waterMarks[apis.PodEvictedMemoryHighWaterMarkKey] <= waterMarks[apis.PodEvictedMemoryLowWaterMarkKey] {
		return nil, nil, true, fmt.Errorf("memory low watermark is higher than high watermark, low: %v, high: %v", waterMarks[apis.PodEvictedMemoryLowWaterMarkKey], waterMarks[apis.PodEvictedMemoryHighWaterMarkKey])
	}

	lowWatermark := apis.Resource{
		corev1.ResourceCPU:    waterMarks[apis.PodEvictedCPULowWaterMarkKey],
		corev1.ResourceMemory: waterMarks[apis.PodEvictedMemoryLowWaterMarkKey],
	}

	highWatermark := apis.Resource{
		corev1.ResourceCPU:    waterMarks[apis.PodEvictedCPUHighWaterMarkKey],
		corev1.ResourceMemory: waterMarks[apis.PodEvictedMemoryHighWaterMarkKey],
	}
	return lowWatermark, highWatermark, true, nil
}

func GetIntValueWaterMark(annotations map[string]string, annotationKey string) (intValue int64, exist bool, err error) {
	realKey := getRealAnnotationKey(annotations, annotationKey)
	if strValue, found := annotations[realKey]; found {
		intValue, err = strconv.ParseInt(strValue, 10, 32)
		if err == nil {
			if intValue >= 0 {
				return intValue, true, nil
			}
			err = fmt.Errorf("value(%d) of %s is negative, must be non-negative integer", intValue, realKey)
		}
	}

	switch realKey {
	case apis.PodEvictedCPULowWaterMarkKey, apis.PodEvictedOverSubscriptionCPULowWaterMarkKey:
		return eviction.DefaultEvictingCPULowWatermark, false, err
	case apis.PodEvictedCPUHighWaterMarkKey, apis.PodEvictedOverSubscriptionCPUHighWaterMarkKey:
		return eviction.DefaultEvictingCPUHighWatermark, false, err
	case apis.PodEvictedMemoryHighWaterMarkKey, apis.PodEvictedOverSubscriptionMemoryHighWaterMarkKey:
		return eviction.DefaultEvictingMemoryHighWatermark, false, err
	case apis.PodEvictedMemoryLowWaterMarkKey, apis.PodEvictedOverSubscriptionMemoryLowWaterMarkKey:
		return eviction.DefaultEvictingMemoryLowWatermark, false, err
	default:
		return 0, false, fmt.Errorf("unknown annotation key: %s", annotationKey)
	}
}

// getRealAnnotationKey used to get real annotation key to be compatible with old api.
func getRealAnnotationKey(annotations map[string]string, annotationKey string) string {
	oldKey := getOldAnnotationKey(annotationKey)
	_, oldKeyExists := annotations[oldKey]
	_, newKeyExists := annotations[annotationKey]
	// Old annotation exists and new annotation not exist, use the old one.
	if oldKeyExists && !newKeyExists {
		return oldKey
	}
	// Other case, always return the new one.
	return annotationKey
}

func getOldAnnotationKey(annotationKey string) string {
	parts := strings.Split(annotationKey, "/")
	if len(parts) != 2 {
		return annotationKey
	}
	return parts[0] + "/oversubscription-" + parts[1]
}
