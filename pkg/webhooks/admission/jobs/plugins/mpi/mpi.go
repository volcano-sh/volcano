/*
Copyright 2022 The Volcano Authors.

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

package mpi

import (
	"k8s.io/klog"
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	controllerMpi "volcano.sh/volcano/pkg/controllers/job/plugins/distributed-framework/mpi"
)

func AddDependsOn(job *v1alpha1.Job) {
	mp := controllerMpi.NewInstance(job.Spec.Plugins[controllerMpi.MPIPluginName])
	masterIndex := helpers.GetTasklndexUnderJob(mp.GetMasterName(), job)
	if masterIndex == -1 {
		klog.Errorln("Failed to find master task")
		return
	}
	if job.Spec.Tasks[masterIndex].DependsOn == nil {
		job.Spec.Tasks[masterIndex].DependsOn = &v1alpha1.DependsOn{
			Name: []string{mp.GetWorkerName()},
		}
	}
}
