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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/controller"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/config"
)

var taint = &v1.Taint{
	Key:    apis.PodEvictingKey,
	Effect: v1.TaintEffectNoSchedule,
}

func DisableSchedule(config *config.Configuration) error {
	return controller.AddOrUpdateTaintOnNode(context.TODO(), config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName, taint)
}

func RecoverSchedule(config *config.Configuration) error {
	node, err := config.GetNode()
	if err != nil {
		return fmt.Errorf("failed to get node, err: %v", err)
	}
	return controller.RemoveTaintOffNode(context.TODO(), config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName, node, taint)
}
