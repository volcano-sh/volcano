/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	vkv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/batch/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/apis/helpers"
	vkhelpers "github.com/kubernetes-sigs/kube-batch/pkg/controllers/job/helpers"
	vkinterface "github.com/kubernetes-sigs/kube-batch/pkg/controllers/job/plugins/interface"
)

type envPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset vkinterface.PluginClientset
}

// New create new envPlugin type
func New(client vkinterface.PluginClientset, arguments []string) vkinterface.PluginInterface {
	envPlugin := envPlugin{pluginArguments: arguments, Clientset: client}

	return &envPlugin
}

func (ep *envPlugin) Name() string {
	return "env"
}

func (ep *envPlugin) OnPodCreate(pod *v1.Pod, job *vkv1.Job) error {
	// use podName.serviceName as default pod DNS domain
	if len(pod.Spec.Hostname) == 0 {
		pod.Spec.Hostname = pod.Name
	}
	if len(pod.Spec.Subdomain) == 0 {
		pod.Spec.Subdomain = job.Name
	}

	// add VK_TASK_INDEX env to each container
	for i, c := range pod.Spec.Containers {
		vkIndex := v1.EnvVar{
			Name:  TaskVkIndex,
			Value: vkhelpers.GetTaskIndex(pod),
		}
		pod.Spec.Containers[i].Env = append(c.Env, vkIndex)
	}

	ep.mountConfigmap(pod, job)

	return nil
}

func (ep *envPlugin) OnJobAdd(job *vkv1.Job) error {
	if job.Status.ControlledResources["plugin-"+ep.Name()] == ep.Name() {
		return nil
	}

	data := generateHost(job)

	if err := helpers.CreateConfigMapIfNotExist(job, ep.Clientset.KubeClients, data, ep.cmName(job)); err != nil {
		return err
	}

	job.Status.ControlledResources["plugin-"+ep.Name()] = ep.Name()

	return nil
}

func (ep *envPlugin) OnJobDelete(job *vkv1.Job) error {
	if err := helpers.DeleteConfigmap(job, ep.Clientset.KubeClients, ep.cmName(job)); err != nil {
		return err
	}

	return nil
}

func (ep *envPlugin) mountConfigmap(pod *v1.Pod, job *vkv1.Job) {
	cmName := ep.cmName(job)
	cmVolume := v1.Volume{
		Name: cmName,
	}
	cmVolume.ConfigMap = &v1.ConfigMapVolumeSource{
		LocalObjectReference: v1.LocalObjectReference{
			Name: cmName,
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, cmVolume)

	for i, c := range pod.Spec.Containers {
		vm := v1.VolumeMount{
			MountPath: ConfigMapMountPath,
			Name:      cmName,
		}

		pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
	}
}

func generateHost(job *vkv1.Job) map[string]string {
	data := make(map[string]string, len(job.Spec.Tasks))

	for _, ts := range job.Spec.Tasks {
		hosts := make([]string, 0, ts.Replicas)

		for i := 0; i < int(ts.Replicas); i++ {
			hostName := ts.Template.Spec.Hostname
			subdomain := ts.Template.Spec.Subdomain
			if len(hostName) == 0 {
				hostName = fmt.Sprintf(vkhelpers.TaskNameFmt, job.Name, ts.Name, i)
			}
			if len(subdomain) == 0 {
				subdomain = job.Name
			}
			hosts = append(hosts, hostName+"."+subdomain)
		}

		key := fmt.Sprintf(ConfigMapTaskHostFmt, ts.Name)
		data[key] = strings.Join(hosts, "\n")
	}

	return data
}

func (ep *envPlugin) cmName(job *vkv1.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, ep.Name())
}
