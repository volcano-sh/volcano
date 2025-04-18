/*
Copyright 2025 The Kubernetes Authors.

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

package predicates

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	volumeBindingTimeoutSecondsKey = "volumebinding.bindTimeoutSeconds"
	volumeBindingShapeKey          = "volumebinding.shape"
	volumeBindingWeightKey         = "volumebinding.weight"
	defaultBindTimeoutSeconds      = 30
)

type wrapVolumeBindingArgs struct {
	Weight int
	*kubeschedulerconfig.VolumeBindingArgs
}

func defaultVolumeBindingArgs() *wrapVolumeBindingArgs {
	return &wrapVolumeBindingArgs{
		Weight: 1,
		VolumeBindingArgs: &kubeschedulerconfig.VolumeBindingArgs{
			BindTimeoutSeconds: defaultBindTimeoutSeconds,
		},
	}
}

func setUpVolumeBindingArgs(vbArgs *wrapVolumeBindingArgs, rawArgs framework.Arguments) {
	if weight, ok := framework.Get[int](rawArgs, volumeBindingWeightKey); ok {
		vbArgs.Weight = weight
	}

	if timeout, ok := framework.Get[int64](rawArgs, volumeBindingTimeoutSecondsKey); ok {
		vbArgs.BindTimeoutSeconds = timeout
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority) {
		shape, _ := framework.Get[[]kubeschedulerconfig.UtilizationShapePoint](rawArgs, volumeBindingShapeKey)
		// If shape is not configured, assigning nil here is ok
		vbArgs.Shape = shape
	}
}
