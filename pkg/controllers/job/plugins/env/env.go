/*
Copyright 2019 The Volcano Authors.

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

package env

import (
	v1 "k8s.io/api/core/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type envPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset pluginsinterface.PluginClientset
}

// New creates env plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	envPlugin := envPlugin{pluginArguments: arguments, Clientset: client}

	return &envPlugin
}

func (ep *envPlugin) Name() string {
	return "env"
}

func (ep *envPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	index := jobhelpers.GetPodIndexUnderTask(pod)

	// add VK_TASK_INDEX and VC_TASK_INDEX env to each container
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{Name: TaskVkIndex, Value: index}, v1.EnvVar{Name: TaskIndex, Value: index})
	}

	// add VK_TASK_INDEX and VC_TASK_INDEX env to each init container
	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].Env = append(pod.Spec.InitContainers[i].Env, v1.EnvVar{Name: TaskVkIndex, Value: index}, v1.EnvVar{Name: TaskIndex, Value: index})
	}

	return nil
}

func (ep *envPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+ep.Name()] == ep.Name() {
		return nil
	}

	job.Status.ControlledResources["plugin-"+ep.Name()] = ep.Name()

	return nil
}

func (ep *envPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+ep.Name()] != ep.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+ep.Name())
	return nil
}

func (ep *envPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}
