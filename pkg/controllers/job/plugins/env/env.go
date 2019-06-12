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
	"k8s.io/api/core/v1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	vkinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type envPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset vkinterface.PluginClientset
}

// New creates env plugin
func New(client vkinterface.PluginClientset, arguments []string) vkinterface.PluginInterface {
	envPlugin := envPlugin{pluginArguments: arguments, Clientset: client}

	return &envPlugin
}

func (ep *envPlugin) Name() string {
	return "env"
}

func (ep *envPlugin) OnPodCreate(pod *v1.Pod, job *vkv1.Job) error {
	// add VK_TASK_INDEX env to each container
	for i, c := range pod.Spec.Containers {
		vkIndex := v1.EnvVar{
			Name:  TaskVkIndex,
			Value: vkhelpers.GetTaskIndex(pod),
		}
		pod.Spec.Containers[i].Env = append(c.Env, vkIndex)
	}

	return nil
}

func (ep *envPlugin) OnJobAdd(job *vkv1.Job) error {
	if job.Status.ControlledResources["plugin-"+ep.Name()] == ep.Name() {
		return nil
	}

	job.Status.ControlledResources["plugin-"+ep.Name()] = ep.Name()

	return nil
}

func (ep *envPlugin) OnJobDelete(job *vkv1.Job) error {
	return nil
}
