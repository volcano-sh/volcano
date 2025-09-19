/*
 Copyright 2023 The Volcano Authors.

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

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// WorkLoadSupport can cache and operate **K8s native resource**, Deployment/Replicas/ReplicationController/StatefulSet resources currently.
	WorkLoadSupport featuregate.Feature = "WorkLoadSupport"

	// VolcanoJobSupport can identify and schedule volcano job.
	VolcanoJobSupport featuregate.Feature = "VolcanoJobSupport"

	// PodDisruptionBudgetsSupport can cache and support PodDisruptionBudgets
	PodDisruptionBudgetsSupport featuregate.Feature = "PodDisruptionBudgetsSupport"

	// QueueCommandSync supports queue command sync.
	QueueCommandSync featuregate.Feature = "QueueCommandSync"

	// PriorityClass to provide the capacity of preemption at pod group level.
	PriorityClass featuregate.Feature = "PriorityClass"

	// CSIStorage tracking of available storage capacity that CSI drivers provide
	CSIStorage featuregate.Feature = "CSIStorage"

	// ResourceTopology supports resources like cpu/memory topology aware.
	ResourceTopology featuregate.Feature = "ResourceTopology"

	// CronVolcanoJobSupport can identify and schedule volcano cronjob.
	CronVolcanoJobSupport featuregate.Feature = "CronVolcanoJobSupport"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultVolcanoFeatureGates))
}

var defaultVolcanoFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	WorkLoadSupport:             {Default: true, PreRelease: featuregate.Alpha},
	VolcanoJobSupport:           {Default: true, PreRelease: featuregate.Alpha},
	PodDisruptionBudgetsSupport: {Default: true, PreRelease: featuregate.Alpha},
	QueueCommandSync:            {Default: true, PreRelease: featuregate.Alpha},
	PriorityClass:               {Default: true, PreRelease: featuregate.Alpha},
	// CSIStorage is explicitly set to false by default.
	CSIStorage:            {Default: false, PreRelease: featuregate.Alpha},
	ResourceTopology:      {Default: true, PreRelease: featuregate.Alpha},
	CronVolcanoJobSupport: {Default: true, PreRelease: featuregate.Alpha},
}
