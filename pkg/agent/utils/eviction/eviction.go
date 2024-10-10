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

package eviction

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/types"

	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
)

const (
	// Default Evicting config
	DefaultEvictingCPUHighWatermark    = 80
	DefaultEvictingMemoryHighWatermark = 60
	DefaultEvictingCPULowWatermark     = 30
	DefaultEvictingMemoryLowWatermark  = 30

	// Reason is the reason reported back in status.
	Reason = "Evicted"
)

func evictPod(ctx context.Context, client clientset.Interface, gracePeriodSeconds *int64, pod *corev1.Pod) error {
	if *gracePeriodSeconds < int64(0) {
		*gracePeriodSeconds = int64(0)
	}
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			GracePeriodSeconds: gracePeriodSeconds,
		},
	}
	// TODO: get policy version dynamically using discovery client.
	return client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
}

type Eviction interface {
	Evict(ctx context.Context, pod *corev1.Pod, eventRecorder record.EventRecorder, gracePeriodSeconds int64, evictMsg string) bool
}

type eviction struct {
	kubeClient  clientset.Interface
	nodeName    string
	killPodFunc utilpod.KillPod
}

func NewEviction(client clientset.Interface, nodeName string) Eviction {
	return &eviction{
		kubeClient:  client,
		nodeName:    nodeName,
		killPodFunc: evictPod,
	}
}

func (e *eviction) Evict(ctx context.Context, pod *corev1.Pod, eventRecorder record.EventRecorder, gracePeriodSeconds int64, evictMsg string) bool {
	if types.IsCriticalPod(pod) {
		klog.ErrorS(nil, "Cannot evict a critical pod", "pod", klog.KObj(pod))
		return false
	}

	eventRecorder.Eventf(pod, corev1.EventTypeWarning, Reason, evictMsg)
	err := e.killPodFunc(ctx, e.kubeClient, &gracePeriodSeconds, pod)
	if err != nil {
		klog.ErrorS(err, "Failed to evict pod", "pod", klog.KObj(pod))
		return false
	}

	klog.InfoS("Successfully evicted pod", "pod", klog.KObj(pod))
	return true
}
