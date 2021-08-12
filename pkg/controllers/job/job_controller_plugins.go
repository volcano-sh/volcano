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

package job

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/plugins"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func (cc *jobcontroller) pluginOnPodCreate(job *batch.Job, pod *v1.Pod) error {
	client := pluginsinterface.PluginClientset{KubeClients: cc.kubeClient}
	for name, args := range job.Spec.Plugins {
		pb, found := plugins.GetPluginBuilder(name)
		if !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			klog.Error(err)
			return err
		}
		klog.Infof("Starting to execute plugin at <pluginOnPodCreate>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
		if err := pb(client, args).OnPodCreate(pod, job); err != nil {
			klog.Errorf("Failed to process on pod create plugin %s, err %v.", name, err)
			return err
		}
	}
	return nil
}

func (cc *jobcontroller) pluginOnJobAdd(job *batch.Job) error {
	client := pluginsinterface.PluginClientset{KubeClients: cc.kubeClient}
	if job.Status.ControlledResources == nil {
		job.Status.ControlledResources = make(map[string]string)
	}
	for name, args := range job.Spec.Plugins {
		pb, found := plugins.GetPluginBuilder(name)
		if !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			klog.Error(err)
			return err
		}
		klog.Infof("Starting to execute plugin at <pluginOnJobAdd>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
		if err := pb(client, args).OnJobAdd(job); err != nil {
			klog.Errorf("Failed to process on job add plugin %s, err %v.", name, err)
			return err
		}
	}

	return nil
}

func (cc *jobcontroller) pluginOnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources == nil {
		job.Status.ControlledResources = make(map[string]string)
	}
	client := pluginsinterface.PluginClientset{KubeClients: cc.kubeClient}
	for name, args := range job.Spec.Plugins {
		pb, found := plugins.GetPluginBuilder(name)
		if !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			klog.Error(err)
			return err
		}
		klog.Infof("Starting to execute plugin at <pluginOnJobDelete>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
		if err := pb(client, args).OnJobDelete(job); err != nil {
			klog.Errorf("failed to process on job delete plugin %s, err %v.", name, err)
			return err
		}
	}

	return nil
}

func (cc *jobcontroller) pluginOnJobUpdate(job *batch.Job) error {
	client := pluginsinterface.PluginClientset{KubeClients: cc.kubeClient}
	if job.Status.ControlledResources == nil {
		job.Status.ControlledResources = make(map[string]string)
	}
	for name, args := range job.Spec.Plugins {
		pb, found := plugins.GetPluginBuilder(name)
		if !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			klog.Error(err)
			return err
		}
		klog.Infof("Starting to execute plugin at <pluginOnJobUpdate>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
		if err := pb(client, args).OnJobUpdate(job); err != nil {
			klog.Errorf("Failed to process on job update plugin %s, err %v.", name, err)
			return err
		}
	}

	return nil
}
