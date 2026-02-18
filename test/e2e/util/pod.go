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

package util

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type PodSpec struct {
	Name            string
	Node            string
	Req             v1.ResourceList
	Tolerations     []v1.Toleration
	Annotations     map[string]string
	Labels          map[string]string
	SchedulerName   string
	RestartPolicy   v1.RestartPolicy
	NodeSelector    map[string]string
	SchedulingGates []v1.PodSchedulingGate
}

func CreatePod(ctx *TestContext, spec PodSpec) *v1.Pod {
	meta := metav1.ObjectMeta{Name: spec.Name, Namespace: ctx.Namespace}

	if len(spec.Annotations) > 0 {
		meta.Annotations = spec.Annotations
	}

	if len(spec.Labels) > 0 {
		meta.Labels = spec.Labels
	}

	pod := &v1.Pod{
		ObjectMeta: meta,
		Spec: v1.PodSpec{
			NodeName: spec.Node,
			Containers: []v1.Container{
				{
					Image:           DefaultNginxImage,
					Name:            spec.Name,
					ImagePullPolicy: v1.PullIfNotPresent,
					Resources: v1.ResourceRequirements{
						Requests: spec.Req,
					},
				},
			},
			Tolerations: spec.Tolerations,
		},
	}

	if spec.SchedulerName != "" {
		pod.Spec.SchedulerName = spec.SchedulerName
	}
	if spec.RestartPolicy != "" {
		pod.Spec.RestartPolicy = spec.RestartPolicy
	}

	if len(spec.NodeSelector) > 0 {
		pod.Spec.NodeSelector = spec.NodeSelector
	}

	if len(spec.SchedulingGates) > 0 {
		pod.Spec.SchedulingGates = spec.SchedulingGates
	}

	pod, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", spec.Name)

	return pod
}

func WaitPodReady(ctx *TestContext, pod *v1.Pod) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		pod, err := ctx.Kubeclient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return pod.Status.Phase == v1.PodRunning, nil
	})
}

func DeletePod(ctx *TestContext, pod *v1.Pod) {
	err := ctx.Kubeclient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to delete pod %s", pod.Name)
}

// PodHasSchedulingGates returns true if the pod's scheduling gates exactly match the given list of gate names (order-independent).
func PodHasSchedulingGates(pod *v1.Pod, gateNames ...string) bool {
	if len(pod.Spec.SchedulingGates) != len(gateNames) {
		return false
	}
	nameSet := make(map[string]bool)
	for _, n := range gateNames {
		nameSet[n] = true
	}
	for _, g := range pod.Spec.SchedulingGates {
		if !nameSet[g.Name] {
			return false
		}
	}
	return true
}
