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

	v1core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// schedulerInjectingClientset is used to wrap the Kubernetes clientset and inject a specific scheduler name, which is volcano in our e2e cases by default.
// Then we don't need to use like gomonkey tools to patch k8s e2e methods to inject the scheduler name.
type schedulerInjectingClientset struct {
	kubernetes.Interface
	schedulerName string
}

func (c *schedulerInjectingClientset) CoreV1() v1.CoreV1Interface {
	return &schedulerInjectingCoreV1{
		CoreV1Interface: c.Interface.CoreV1(),
		schedulerName:   c.schedulerName,
	}
}

// schedulerInjectingCoreV1 wraps CoreV1Interface and intercepts the Pods method.
type schedulerInjectingCoreV1 struct {
	v1.CoreV1Interface
	schedulerName string
}

func (c *schedulerInjectingCoreV1) Pods(namespace string) v1.PodInterface {
	return &schedulerInjectingPodInterface{
		PodInterface:  c.CoreV1Interface.Pods(namespace),
		schedulerName: c.schedulerName,
	}
}

// schedulerInjectingPodInterface wraps PodInterface and intercepts the Create method to inject a scheduler name into the Pod spec.
type schedulerInjectingPodInterface struct {
	v1.PodInterface
	schedulerName string
}

func (p *schedulerInjectingPodInterface) Create(ctx context.Context, pod *v1core.Pod, opts metav1.CreateOptions) (*v1core.Pod, error) {
	pod.Spec.SchedulerName = p.schedulerName
	return p.PodInterface.Create(ctx, pod, opts)
}

// NewSchedulerInjectingClientset is a constructor function that creates a new instance of schedulerInjectingClientset.
func NewSchedulerInjectingClientset(client kubernetes.Interface, schedulerName string) kubernetes.Interface {
	return &schedulerInjectingClientset{
		Interface:     client,
		schedulerName: schedulerName,
	}
}
