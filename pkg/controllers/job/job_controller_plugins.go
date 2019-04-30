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

	"github.com/golang/glog"

	"k8s.io/api/core/v1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vkplugin "volcano.sh/volcano/pkg/controllers/job/plugins"
	vkinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

func (cc *Controller) pluginOnPodCreate(job *vkv1.Job, pod *v1.Pod) error {
	client := vkinterface.PluginClientset{KubeClients: cc.kubeClients}
	for name, args := range job.Spec.Plugins {
		if pb, found := vkplugin.GetPluginBuilder(name); !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			glog.Error(err)
			return err
		} else {
			glog.Infof("Starting to execute plugin at <pluginOnPodCreate>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
			if err := pb(client, args).OnPodCreate(pod, job); err != nil {
				glog.Errorf("Failed to process on pod create plugin %s, err %v.", name, err)
				return err
			}
		}
	}
	return nil
}

func (cc *Controller) pluginOnJobAdd(job *vkv1.Job) error {
	client := vkinterface.PluginClientset{KubeClients: cc.kubeClients}
	if job.Status.ControlledResources == nil {
		job.Status.ControlledResources = make(map[string]string)
	}
	for name, args := range job.Spec.Plugins {
		if pb, found := vkplugin.GetPluginBuilder(name); !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			glog.Error(err)
			return err
		} else {
			glog.Infof("Starting to execute plugin at <pluginOnJobAdd>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
			if err := pb(client, args).OnJobAdd(job); err != nil {
				glog.Errorf("Failed to process on job add plugin %s, err %v.", name, err)
				return err
			}
		}
	}

	return nil
}

func (cc *Controller) pluginOnJobDelete(job *vkv1.Job) error {
	client := vkinterface.PluginClientset{KubeClients: cc.kubeClients}
	for name, args := range job.Spec.Plugins {
		if pb, found := vkplugin.GetPluginBuilder(name); !found {
			err := fmt.Errorf("failed to get plugin %s", name)
			glog.Error(err)
			return err
		} else {
			glog.Infof("Starting to execute plugin at <pluginOnJobDelete>: %s on job: <%s/%s>", name, job.Namespace, job.Name)
			if err := pb(client, args).OnJobDelete(job); err != nil {
				glog.Errorf("failed to process on job delete plugin %s, err %v.", name, err)
				return err
			}
		}
	}

	return nil
}
