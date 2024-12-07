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

func updateLabel(labels map[string]string) Modifier {
	return func(node *v1.Node) {
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		for k, v := range labels {
			node.Labels[k] = v
		}
	}
}

// SetOverSubscriptionLabel set node label "volcano.sh/oversubscription"=true
func SetOverSubscriptionLabel(config *config.Configuration) error {
	return update(config, []Modifier{
		updateLabel(map[string]string{
			apis.OverSubscriptionNodeLabelKey: "true",
		}),
	})
}

func ResetOverSubscriptionLabel() Modifier {
	return func(node *v1.Node) {
		if _, ok := node.Labels[apis.OverSubscriptionNodeLabelKey]; !ok {
			return
		}
		updateLabel(map[string]string{
			apis.OverSubscriptionNodeLabelKey: "false",
		})(node)
	}
}
