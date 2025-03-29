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

package kubescheduler

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// VolumeBinding plugin args keys
	volumeBindingEnabled           = "volumebinding.enabled"
	volumeBindingTimeoutSecondsKey = "volumebinding.bindTimeoutSeconds"
	volumeBindingShapeKey          = "volumebinding.shape"
	volumeBindingWeightKey         = "volumebinding.weight"
	defaultBindTimeoutSeconds      = 30
)

// PluginArgs stores the args of all plugins
type PluginArgs struct {
	// VolumeBinding plugin args
	VolumeBinding *VolumeBindingArgs
}

// VolumeBindingArgs stores the args of volume binding plugin
type VolumeBindingArgs struct {
	Enabled            bool
	Weight             int32
	BindTimeoutSeconds int64
	Shape              []kubeschedulerconfig.UtilizationShapePoint
}

func NewDefaultVolumeBindingArgs() *VolumeBindingArgs {
	return &VolumeBindingArgs{
		Enabled:            true, // volume binding is enabled by default
		Weight:             1,
		BindTimeoutSeconds: defaultBindTimeoutSeconds,
	}
}

func SetUpVolumeBindingArgs(pluginArgs *PluginArgs, rawArgs framework.Arguments) {
	if enabled, ok := framework.Get[bool](rawArgs, volumeBindingEnabled); !ok {
		pluginArgs.VolumeBinding.Enabled = enabled
	}

	if weight, ok := framework.Get[int32](rawArgs, volumeBindingWeightKey); !ok {
		pluginArgs.VolumeBinding.Weight = weight
	}

	if timeout, ok := framework.Get[int64](rawArgs, volumeBindingTimeoutSecondsKey); !ok {
		pluginArgs.VolumeBinding.BindTimeoutSeconds = timeout
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority) {
		shape, _ := framework.Get[[]kubeschedulerconfig.UtilizationShapePoint](rawArgs, volumeBindingShapeKey)
		// If shape is not configured, assigning nil here is ok
		pluginArgs.VolumeBinding.Shape = shape
	}
}
