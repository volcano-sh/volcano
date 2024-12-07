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
	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/config"
)

func RemoveEvictionAnnotation(config *config.Configuration) error {
	return update(config, []Modifier{removeEvictionAnnotation()})
}

func AddEvictionAnnotation(config *config.Configuration) error {
	return update(config, []Modifier{updateAnnotation(map[string]string{
		apis.PodEvictingKey: "true",
	})})
}

func updateAnnotation(annotations map[string]string) Modifier {
	return func(node *v1.Node) {
		if node.Annotations == nil {
			node.Annotations = make(map[string]string)
		}
		for k, v := range annotations {
			node.Annotations[k] = v
		}
	}
}

func removeEvictionAnnotation() Modifier {
	return func(node *v1.Node) {
		if _, ok := node.Annotations[apis.PodEvictingKey]; !ok {
			return
		}
		updateAnnotation(map[string]string{
			apis.PodEvictingKey: "false",
		})(node)
	}
}
