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
	"flag"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// MPIPluginName is the name of the plugin
	MPIPluginName = "mpi"
	// DefaultPort is the default port for ssh
	DefaultPort = 22
	// DefaultMaster is the default task name of master host
	DefaultMaster = "master"
	// DefaultWorker is the default task name of worker host
	DefaultWorker = "worker"
	// MPIHost is the environment variable key of MPI host
	MPIHost = "MPI_HOST"
)

type Plugin struct {
	mpiArguments []string
	clientset    pluginsinterface.PluginClientset
	masterName   string
	workerName   string
	port         int
}

// New creates mpi plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	mp := Plugin{mpiArguments: arguments, clientset: client}
	mp.addFlags()
	return &mp
}

func NewInstance(arguments []string) Plugin {
	mp := Plugin{mpiArguments: arguments}
	mp.addFlags()
	return mp
}

func (mp *Plugin) addFlags() {
	flagSet := flag.NewFlagSet(mp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&mp.masterName, "master", DefaultMaster, "name of master role task")
	flagSet.StringVar(&mp.workerName, "worker", DefaultWorker, "name of worker role task")
	flagSet.IntVar(&mp.port, "port", DefaultPort, "open port for containers")
	if err := flagSet.Parse(mp.mpiArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", mp.Name(), err)
	}
}

func (mp *Plugin) Name() string {
	return MPIPluginName
}

func (mp *Plugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	isMaster := false
	workerHosts := ""
	env := v1.EnvVar{}
	if helpers.GetTaskKey(pod) == mp.masterName {
		taskIndex := helpers.GetTaskIndexUnderJob(mp.workerName, job)
		if taskIndex == -1 {
			return nil
		}
		workerHosts = mp.generateTaskHosts(job.Spec.Tasks[taskIndex], job.Name)
		env = v1.EnvVar{
			Name:  MPIHost,
			Value: workerHosts,
		}

		isMaster = true
	}

	// open port for ssh and add MPI_HOST env for master task
	for index, ic := range pod.Spec.InitContainers {
		mp.openContainerPort(&ic, index, pod, true)
		if isMaster {
			pod.Spec.InitContainers[index].Env = append(pod.Spec.InitContainers[index].Env, env)
		}
	}

	for index, c := range pod.Spec.Containers {
		mp.openContainerPort(&c, index, pod, false)
		if isMaster {
			pod.Spec.Containers[index].Env = append(pod.Spec.Containers[index].Env, env)
		}
	}

	return nil
}

func (mp *Plugin) generateTaskHosts(task batch.TaskSpec, jobName string) string {
	if task.Replicas == 0 {
		return ""
	}

	var builder strings.Builder
	for i := 0; i < int(task.Replicas); i++ {
		hostName := task.Template.Spec.Hostname
		subdomain := task.Template.Spec.Subdomain

		if hostName == "" {
			hostName = helpers.MakePodName(jobName, task.Name, i)
		}
		if subdomain == "" {
			subdomain = jobName
		}

		builder.WriteString(hostName)
		builder.WriteString(".")
		builder.WriteString(subdomain)

		if task.Template.Spec.Hostname == "" && i < int(task.Replicas)-1 {
			builder.WriteString(",")
		}

		// If a hostname is explicitly specified, assume only one host is needed.
		// Break the loop early to avoid generating additional hosts.
		if task.Template.Spec.Hostname != "" {
			break
		}
	}

	return builder.String()
}

func (mp *Plugin) openContainerPort(c *v1.Container, index int, pod *v1.Pod, isInitContainer bool) {
	SSHPortRight := false
	for _, p := range c.Ports {
		if p.ContainerPort == int32(mp.port) {
			SSHPortRight = true
			break
		}
	}
	if !SSHPortRight {
		sshPort := v1.ContainerPort{
			Name:          "mpijob-port",
			ContainerPort: int32(mp.port),
		}
		if isInitContainer {
			pod.Spec.InitContainers[index].Ports = append(pod.Spec.InitContainers[index].Ports, sshPort)
		} else {
			pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, sshPort)
		}
	}
}

func (mp *Plugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+mp.Name()] == mp.Name() {
		return nil
	}
	job.Status.ControlledResources["plugin-"+mp.Name()] = mp.Name()
	return nil
}

func (mp *Plugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+mp.Name()] != mp.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+mp.Name())
	return nil
}

func (mp *Plugin) OnJobUpdate(job *batch.Job) error {
	return nil
}

func (mp *Plugin) GetMasterName() string {
	return mp.masterName
}

func (mp *Plugin) GetWorkerName() string {
	return mp.workerName
}

func (mp *Plugin) GetMpiArguments() []string {
	return mp.mpiArguments
}
