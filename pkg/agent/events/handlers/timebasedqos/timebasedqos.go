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

package timebasedqos

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clientset "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewTimeBasedQoSHandle)
	handlers.RegisterEventHandleFunc(string(framework.TimeBaseQosEventName), NewTimeBasedQoSHandle)
}

type podQoSOperation func(pod *corev1.Pod, activePolicy activeQoSPolicy) error

// activeQoSPolicy represents an active time-based QoS policy with selector and target QoS level
type activeQoSPolicy struct {
	Name           string
	Selector       labels.Selector
	TargetQoSLevel string
}

type timeBasedQoSHandle struct {
	*base.BaseHandle
	// activeSelectors is a map of active label selectors for time-based QoS policies。 When a PodEvent arrives,
	// the QoS level can be set for the Pod based on the matching activeSelector
	activePolicies map[string]activeQoSPolicy
	podLister      v1.PodLister
	client         clientset.Interface
	eventRecorder  record.EventRecorder
}

func (t *timeBasedQoSHandle) Handle(event interface{}) error {
	switch e := event.(type) {
	case framework.PodEvent:
		return t.handlePodEvent(e)
	case framework.TimeBasedQoSPolicyEvent:
		return t.handleTimeBasedQoSPolicyEvent(e)
	default:
		return nil
	}
}

func (t *timeBasedQoSHandle) handleTimeBasedQoSPolicyEvent(event framework.TimeBasedQoSPolicyEvent) error {
	switch event.Action {
	case framework.TimeBasedQoSPolicyActive:
		// 1. add policy to activePolicies if not exist
		if _, exist := t.activePolicies[event.Policy.Name]; !exist {
			selector, err := metav1.LabelSelectorAsSelector(event.Policy.Selector)
			if err != nil {
				klog.ErrorS(err, "Failed to convert label selector", "selector", event.Policy.Selector)
				return err
			}
			aqp := activeQoSPolicy{
				Name:           event.Policy.Name,
				Selector:       selector,
				TargetQoSLevel: strconv.Itoa(*event.Policy.TargetQoSLevel),
			}
			t.activePolicies[event.Policy.Name] = aqp
			klog.V(4).InfoS("Time-based QoS policy activated", "policy", event.Policy.Name, "targetQoSLevel", event.Policy.TargetQoSLevel)
		}
		// 2. apply policy to existing pods
		activePolicy := t.activePolicies[event.Policy.Name]
		err := t.applyQoSPolicyToPods(activePolicy, t.updatePodQoSLevel)
		if err != nil {
			return err
		}
	case framework.TimeBasedQoSPolicyExpired:
		if activePolicy, exist := t.activePolicies[event.Policy.Name]; exist {
			// 1. revert QoS level for pods matching the policy
			err := t.applyQoSPolicyToPods(activePolicy, t.revertPodQoSLevel)
			if err != nil {
				return err
			}
			// 2. remove policy from activePolicies
			delete(t.activePolicies, event.Policy.Name)
			klog.V(4).InfoS("Time-based QoS policy expired", "policy", event.Policy.Name)
		} else {
			klog.V(4).InfoS("Time-based QoS policy expired but not found in active policies, skipping", "policy", event.Policy.Name)
		}
	default:
		return fmt.Errorf("unknown action %v for TimeBasedQoSPolicyEvent", event.Action)
	}

	return nil
}

func (t *timeBasedQoSHandle) handlePodEvent(event framework.PodEvent) error {
	// Currently, if multiple time-based QoS policies match a pod, only the first one will be applied.
	for _, activePolicy := range t.activePolicies {
		if activePolicy.Selector.Matches(labels.Set(event.Pod.Labels)) {
			err := t.updatePodQoSLevel(event.Pod, activePolicy)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *timeBasedQoSHandle) applyQoSPolicyToPods(activePolicy activeQoSPolicy, operation podQoSOperation) error {
	pods, err := t.podLister.List(activePolicy.Selector)
	if err != nil {
		klog.ErrorS(err, "Failed to list pods for active QoS policy", "policy", activePolicy.Name)
		return err
	}
	errChan := make(chan error, len(pods))
	for _, pod := range pods {
		go func(pod *corev1.Pod, errChan chan<- error) {
			errChan <- operation(pod, activePolicy)
		}(pod, errChan)
	}

	var errs []error
	for i := 0; i < len(pods); i++ {
		if err := <-errChan; err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

func (t *timeBasedQoSHandle) updatePodQoSLevel(pod *corev1.Pod, activePolicy activeQoSPolicy) error {
	if pod.Annotations[apis.PodQosLevelKey] == activePolicy.TargetQoSLevel {
		klog.V(4).InfoS("Pod already has the target QoS level", "pod", klog.KObj(pod), "policy", activePolicy.Name, "qosLevel", activePolicy.TargetQoSLevel)
		return nil
	}

	podCopy := pod.DeepCopy()
	if podCopy.Annotations == nil {
		podCopy.Annotations = make(map[string]string)
	}
	if podCopy.Annotations[apis.PodQosLevelKey] != "" {
		// Store the original QoS level, it's used to revert the QoS level when the policy expires
		podCopy.Annotations[apis.PodOriginalQosLevelKey] = podCopy.Annotations[apis.PodQosLevelKey]
	}
	podCopy.Annotations[apis.PodQosLevelKey] = activePolicy.TargetQoSLevel
	if _, err := t.client.CoreV1().Pods(podCopy.Namespace).Update(context.TODO(), podCopy, metav1.UpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to update pod QoS level", "pod", klog.KObj(pod), "policy", activePolicy.Name, "qosLevel", activePolicy.TargetQoSLevel)
		return err
	}

	klog.V(4).InfoS("Updated pod QoS level", "pod", klog.KObj(pod), "policy", activePolicy.Name, "qosLevel", activePolicy.TargetQoSLevel)
	t.eventRecorder.Eventf(pod, corev1.EventTypeNormal, framework.ReasonPolicyActivated, framework.MsgPolicyActivated, activePolicy.Name, activePolicy.TargetQoSLevel)
	return nil
}

func (t *timeBasedQoSHandle) revertPodQoSLevel(pod *corev1.Pod, activePolicy activeQoSPolicy) error {
	if pod.Annotations == nil || pod.Annotations[apis.PodOriginalQosLevelKey] == "" {
		return nil
	}

	podCopy := pod.DeepCopy()
	originalQoS := podCopy.Annotations[apis.PodOriginalQosLevelKey]
	podCopy.Annotations[apis.PodQosLevelKey] = originalQoS
	delete(podCopy.Annotations, apis.PodOriginalQosLevelKey)
	if _, err := t.client.CoreV1().Pods(podCopy.Namespace).Update(context.TODO(), podCopy, metav1.UpdateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to revert pod QoS level", "pod", klog.KObj(pod), "policy", activePolicy.Name, "qosLevel", pod.Annotations[apis.PodOriginalQosLevelKey])
		return err
	}

	klog.V(4).InfoS("Reverted pod QoS level", "pod", klog.KObj(pod), "policy", activePolicy.Name, "qosLevel", originalQoS)
	t.eventRecorder.Eventf(pod, corev1.EventTypeNormal, framework.ReasonPolicyExpired, framework.MsgPolicyExpired, activePolicy.Name, originalQoS)
	return nil
}

func NewTimeBasedQoSHandle(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &timeBasedQoSHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.TimeBasedQoSFeature),
			Config: config,
		},
		podLister: config.GenericConfiguration.PodLister,
	}
}
