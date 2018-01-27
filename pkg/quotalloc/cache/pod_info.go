/*
Copyright 2017 The Kubernetes Authors.

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

package cache

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
)

type PodInfo struct {
	name string
	pod  *v1.Pod
}

func (p *PodInfo) Name() string {
	return p.name
}

func (p *PodInfo) Pod() *v1.Pod {
	return p.pod
}

func (p *PodInfo) Clone() *PodInfo {
	clone := &PodInfo{
		name: p.name,
		pod:  p.pod.DeepCopy(),
	}
	return clone
}

func ListPodsOnaNode(client clientset.Interface, node *v1.Node) ([]*v1.Pod, error) {
	podList, err := client.CoreV1().Pods(v1.NamespaceAll).List(
		metav1.ListOptions{FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": node.Name}).String()})
	if err != nil {
		return []*v1.Pod{}, err
	}

	pods := make([]*v1.Pod, 0)
	for i := range podList.Items {
		pods = append(pods, &podList.Items[i])
	}

	return pods, nil
}
