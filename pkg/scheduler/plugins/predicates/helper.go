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
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/features"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumerestrictions"

	"volcano.sh/volcano/pkg/scheduler/api"
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

// updateVolumeRestrictionsState accounts for existing overlapping consumers, for
// example pods bound before RWOP enforcement was enabled. Kubernetes initializes
// this state assuming at most one reference per RWOP claim; simulation must remove
// every reference before allowing a new consumer.
func (pp *PredicatesPlugin) updateVolumeRestrictionsState(task *api.TaskInfo, state fwk.CycleState, nodes []fwk.NodeInfo) error {
	claims := sets.New[string]()
	for _, volume := range task.Pod.Spec.Volumes {
		if pvc := volume.PersistentVolumeClaim; pvc != nil {
			claims.Insert(pvc.ClaimName)
		}
	}
	if len(claims) == 0 {
		return nil
	}
	seen := sets.New[string]()
	plugin := pp.FilterPlugins[volumerestrictions.Name].(*volumerestrictions.VolumeRestrictions)
	for _, node := range nodes {
		for _, info := range node.GetPods() {
			pod := info.GetPod()
			if pod.Namespace != task.Namespace {
				continue
			}
			var extraVolumes []v1.Volume
			for _, volume := range pod.Spec.Volumes {
				pvc := volume.PersistentVolumeClaim
				if pvc == nil || !claims.Has(pvc.ClaimName) {
					continue
				}
				if seen.Has(pvc.ClaimName) {
					extraVolumes = append(extraVolumes, volume)
				} else {
					seen.Insert(pvc.ClaimName)
				}
			}
			if len(extraVolumes) == 0 {
				continue
			}
			extra := pod.DeepCopy()
			extra.Spec.Volumes = extraVolumes
			extraInfo, err := k8sframework.NewPodInfo(extra)
			if err != nil {
				return err
			}
			if status := plugin.AddPod(context.TODO(), state, task.Pod, extraInfo, node); !status.IsSuccess() {
				return status.AsError()
			}
		}
	}
	return nil
}
