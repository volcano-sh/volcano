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

package loadaware

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func SetDefaults_LoadAwareUtilizationArgs(obj runtime.Object) {
	args, ok := obj.(*LoadAwareUtilizationArgs)
	if !ok {
		klog.Errorf("obj with type %T could not parse", obj)
	}
	if !args.UseDeviationThresholds {
		args.UseDeviationThresholds = false
	}
	if args.Thresholds == nil {
		args.Thresholds = nil
	}
	if args.TargetThresholds == nil {
		args.TargetThresholds = nil
	}
	if args.NumberOfNodes == 0 {
		args.NumberOfNodes = 0
	}
	if args.Duration == "" {
		args.Duration = "2m"
	}
	//defaultEvictor
	if args.NodeSelector == "" {
		args.NodeSelector = ""
	}
	if !args.EvictLocalStoragePods {
		args.EvictLocalStoragePods = false
	}
	if !args.EvictSystemCriticalPods {
		args.EvictSystemCriticalPods = false
	}
	if !args.IgnorePvcPods {
		args.IgnorePvcPods = false
	}
	if !args.EvictFailedBarePods {
		args.EvictFailedBarePods = false
	}
	if args.LabelSelector == nil {
		args.LabelSelector = nil
	}
	if args.PriorityThreshold == nil {
		args.PriorityThreshold = nil
	}
	if !args.NodeFit {
		args.NodeFit = false
	}
}
