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

package schedulercache

import (
	"k8s.io/api/core/v1"
)

type PodInfo struct {
	name string
	pod  *v1.Pod
}

func (p *PodInfo) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *PodInfo) Pod() *v1.Pod {
	if p == nil {
		return nil
	}
	return p.pod
}

func (p *PodInfo) Clone() *PodInfo {
	clone := &PodInfo{
		name: p.name,
		pod:  p.pod.DeepCopy(),
	}
	return clone
}
