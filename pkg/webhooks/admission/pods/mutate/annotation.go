/*
Copyright 2021 The Volcano Authors.

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

package mutate

import (
	"github.com/imdario/mergo"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	wkconfig "volcano.sh/volcano/pkg/webhooks/config"
)

type annotationResGroup struct{}

const (
	// defaultAnnotationKey: default annotation key
	defaultAnnotationKey = "volcano.sh/resource-group"
)

// NewAnnotationResGroup create a new structure
func NewAnnotationResGroup() ResGroup {
	return &annotationResGroup{}
}

// getAnnotation get annotations from the resource group
func getAnnotation(resGroupConfig wkconfig.ResGroupConfig) map[string]string {
	annotations := make(map[string]string)
	for _, val := range resGroupConfig.Object.Value {
		tmp := make(map[string]string)
		err := yaml.Unmarshal([]byte(val), &tmp)
		if err != nil {
			continue
		}

		if err := mergo.Merge(&annotations, &tmp); err != nil {
			klog.Errorf("annotations merge failed, err=%v", err)
			continue
		}
	}

	return annotations
}

// IsBelongResGroup adjust whether pod is belong to the resource group
func (resGroup *annotationResGroup) IsBelongResGroup(pod *v1.Pod, resGroupConfig wkconfig.ResGroupConfig) bool {
	if resGroupConfig.Object.Key != "" && resGroupConfig.Object.Key != "annotation" {
		return false
	}

	annotations := getAnnotation(resGroupConfig)
	klog.V(3).Infof("annotations : %v", annotations)
	for key, annotation := range annotations {
		if pod.Annotations[key] == annotation {
			return true
		}
	}

	if resGroupConfig.Object.Key == "" && pod.Annotations[defaultAnnotationKey] == resGroupConfig.ResourceGroup {
		return true
	}

	return false
}
