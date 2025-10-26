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

package predicates

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	volumeBindingTimeoutSecondsKey = "volumebinding.bindTimeoutSeconds"
	volumeBindingShapeKey          = "volumebinding.shape"
	volumeBindingWeightKey         = "volumebinding.weight"
	defaultBindTimeoutSeconds      = 600
	draFilterTimeoutSecondsKey     = "dynamicresources.filterTimeoutSeconds"
	defaultDRAFilterTimeout        = 10 * time.Second
)

type wrapVolumeBindingArgs struct {
	Weight int
	*kubeschedulerconfig.VolumeBindingArgs
}

type wrapDynamicResourcesArgs struct {
	*kubeschedulerconfig.DynamicResourcesArgs
}

func defaultVolumeBindingArgs() *wrapVolumeBindingArgs {
	args := &wrapVolumeBindingArgs{
		Weight: 1,
		VolumeBindingArgs: &kubeschedulerconfig.VolumeBindingArgs{
			BindTimeoutSeconds: defaultBindTimeoutSeconds,
		},
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.StorageCapacityScoring) {
		// default shape setting if VolumeCapacityPriority is enabled
		args.Shape = []kubeschedulerconfig.UtilizationShapePoint{
			{
				Utilization: 0,
				Score:       int32(kubeschedulerconfig.MaxCustomPriorityScore),
			},
			{
				Utilization: 100,
				Score:       0,
			},
		}
	}

	return args
}

// setUpVolumeBindingArgs sets up volume binding arguments from scheduler configuration.
//
// Example configuration in scheduler yaml:
//
//	actions: "enqueue, allocate, backfill"
//	tiers:
//	- plugins:
//	  - name: predicates
//	    arguments:
//	      volumebinding.bindTimeoutSeconds: 600
//	      volumebinding.weight: 1
//	      volumebinding.shape:
//	      - utilization: 0
//	        score: 10
//	      - utilization: 40
//	        score: 8
//	      - utilization: 80
//	        score: 4
//	      - utilization: 100
//	        score: 0
//		  ...
func setUpVolumeBindingArgs(vbArgs *wrapVolumeBindingArgs, rawArgs framework.Arguments) {
	if weight, ok := framework.Get[int](rawArgs, volumeBindingWeightKey); ok {
		vbArgs.Weight = weight
	}

	if timeout, ok := framework.Get[int64](rawArgs, volumeBindingTimeoutSecondsKey); ok {
		vbArgs.BindTimeoutSeconds = timeout
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.StorageCapacityScoring) {
		shape, _ := framework.Get[[]kubeschedulerconfig.UtilizationShapePoint](rawArgs, volumeBindingShapeKey)
		if len(shape) != 0 {
			vbArgs.Shape = shape
		}
	}
}

func defaultDynamicResourcesArgs() *wrapDynamicResourcesArgs {
	return &wrapDynamicResourcesArgs{
		DynamicResourcesArgs: &kubeschedulerconfig.DynamicResourcesArgs{
			FilterTimeout: &metav1.Duration{Duration: defaultDRAFilterTimeout},
		},
	}
}

// setUp DynamicResourcesArg populates args from volcano framework arguments.
//
//	Supported override:
//	 - dynamicresources.filterTimeoutSeconds: integer seconds (e.g., 15)
func setUpDynamicResourcesArgs(dra *wrapDynamicResourcesArgs, rawArgs framework.Arguments) {
	if rawArgs == nil {
		return
	}

	if secs, ok := framework.Get[int](rawArgs, draFilterTimeoutSecondsKey); ok {
		if secs >= 0 {
			dra.FilterTimeout = &metav1.Duration{Duration: time.Duration(secs) * time.Second}
		}
	}
}
