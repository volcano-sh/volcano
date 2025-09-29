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

package ringsconfigmap

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

type ringsConfigMapPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset pluginsinterface.PluginClientset

	// Configuration for the plugin
	config *Config
}

// New creates ringsconfigmap plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	config := DefaultConfig()
	// Parse plugin arguments, use default config as fallback strategy if parsing fails
	if err := config.ParseArguments(arguments); err != nil {
		klog.Errorf("Failed to parse ringsconfigmap plugin arguments: %v", err)
		// Continue using default config when parsing fails to ensure plugin works normally
	}

	ringsConfigMapPlugin := ringsConfigMapPlugin{
		pluginArguments: arguments,
		Clientset:       client,
		config:          config,
	}

	return &ringsConfigMapPlugin
}

func (rcmp *ringsConfigMapPlugin) Name() string {
	return "ringsconfigmap"
}

func (rcmp *ringsConfigMapPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// Get ConfigMap name from template
	configMapName, err := rcmp.config.GetConfigMapName(job)
	if err != nil {
		klog.Errorf("Failed to get ConfigMap name for Job <%s/%s>: %v", job.Namespace, job.Name, err)
		return err
	}

	// Check if volume already exists
	volumeExists := false
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == rcmp.config.VolumeName {
			volumeExists = true
			break
		}
	}

	// Add volume if it doesn't exist
	if !volumeExists {
		volume := v1.Volume{
			Name: rcmp.config.VolumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
	}

	// Check if volumeMount already exists and add to all containers
	for i := range pod.Spec.Containers {
		volumeMountExists := false
		for _, volumeMount := range pod.Spec.Containers[i].VolumeMounts {
			if volumeMount.Name == rcmp.config.VolumeName {
				volumeMountExists = true
				break
			}
		}

		if !volumeMountExists {
			volumeMount := v1.VolumeMount{
				Name:      rcmp.config.VolumeName,
				MountPath: rcmp.config.MountPath,
			}
			pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, volumeMount)
		}
	}

	// Check if volumeMount already exists and add to all init containers
	for i := range pod.Spec.InitContainers {
		volumeMountExists := false
		for _, volumeMount := range pod.Spec.InitContainers[i].VolumeMounts {
			if volumeMount.Name == rcmp.config.VolumeName {
				volumeMountExists = true
				break
			}
		}

		if !volumeMountExists {
			volumeMount := v1.VolumeMount{
				Name:      rcmp.config.VolumeName,
				MountPath: rcmp.config.MountPath,
			}
			pod.Spec.InitContainers[i].VolumeMounts = append(pod.Spec.InitContainers[i].VolumeMounts, volumeMount)
		}
	}

	return nil
}

func (rcmp *ringsConfigMapPlugin) OnJobAdd(job *batch.Job) error {
	// Check if plugin has already been processed
	if job.Status.ControlledResources["plugin-"+rcmp.Name()] == rcmp.Name() {
		return nil
	}

	// Get ConfigMap name from template
	configMapName, err := rcmp.config.GetConfigMapName(job)
	if err != nil {
		klog.Errorf("Failed to get ConfigMap name for Job <%s/%s>: %v", job.Namespace, job.Name, err)
		return err
	}

	// Use helper function to create or update ConfigMap
	err = jobhelpers.CreateOrUpdateConfigMap(job, rcmp.Clientset.KubeClients, rcmp.config.ConfigMapData, configMapName, rcmp.config.ConfigMapLabels, rcmp.config.ConfigMapAnnotations)
	if err != nil {
		klog.V(3).Infof("Failed to create/update ConfigMap for Job <%s/%s>: %v", job.Namespace, job.Name, err)
		return err
	}

	job.Status.ControlledResources["plugin-"+rcmp.Name()] = rcmp.Name()
	return nil
}

func (rcmp *ringsConfigMapPlugin) OnJobDelete(job *batch.Job) error {
	// Check if plugin has been processed
	if job.Status.ControlledResources["plugin-"+rcmp.Name()] != rcmp.Name() {
		return nil
	}

	// Get ConfigMap name from template
	configMapName, err := rcmp.config.GetConfigMapName(job)
	if err != nil {
		klog.Errorf("Failed to get ConfigMap name for Job <%s/%s>: %v", job.Namespace, job.Name, err)
		return err
	}

	// Use helper function to delete ConfigMap
	err = helpers.DeleteConfigmap(job, rcmp.Clientset.KubeClients, configMapName)
	if err != nil {
		klog.V(3).Infof("Failed to delete ConfigMap for Job <%s/%s>: %v", job.Namespace, job.Name, err)
		return err
	}

	delete(job.Status.ControlledResources, "plugin-"+rcmp.Name())
	return nil
}

func (rcmp *ringsConfigMapPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}
