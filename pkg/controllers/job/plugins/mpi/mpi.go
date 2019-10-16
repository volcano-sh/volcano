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

package mpi

import (
	"flag"
	"fmt"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/helpers"
	plugininterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
	"volcano.sh/volcano/pkg/controllers/job/plugins/ssh"
	"volcano.sh/volcano/pkg/controllers/job/plugins/svc"
)

type mpi struct {
	// Arguments given for the plugin
	pluginArguments []string

	clientset plugininterface.PluginClientset

	ssh plugininterface.PluginInterface

	// mpi launcher index, by default it is 0.
	launcher int
	// mpi worker index, by default it is 1.
	worker int
	// slots per worker, by default it is 1.
	slotsPerWorker int

	// hosts specify which hosts to launcher MPI processes
	hosts string
}

// New creates ssh plugin
func New(client plugininterface.PluginClientset, arguments []string) plugininterface.PluginInterface {
	mpi := mpi{pluginArguments: arguments, clientset: client}
	mpi.ssh = ssh.New(client, nil)
	mpi.addFlags()

	return &mpi
}

func (p *mpi) Name() string {
	return "mpi"
}

func (p *mpi) OnPodCreate(pod *v1.Pod, job *vkv1.Job) error {
	p.ssh.OnPodCreate(pod, job)

	// only set `MPI_HOST` env and mount hostfile to launcher pod
	if p.isLauncher(pod, job) {
		cmName := p.cmName(job)
		cmVolume := v1.Volume{
			Name: cmName,
		}
		cmVolume.ConfigMap = &v1.ConfigMapVolumeSource{
			LocalObjectReference: v1.LocalObjectReference{
				Name: cmName,
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, cmVolume)

		data := svc.GenerateHost(job)
		key := fmt.Sprintf(svc.ConfigMapTaskHostFmt, job.Spec.Tasks[p.worker].Name)
		mpiHosts := strings.ReplaceAll(data[key], "\n", ",")
		for i, c := range pod.Spec.Containers {
			vm := v1.VolumeMount{
				MountPath: HOST_FILE_PATH,
				Name:      cmName,
			}

			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{Name: MPI_HOST, Value: mpiHosts})
		}

	} else {
		// use podName.serviceName as default pod DNS domain
		if len(pod.Spec.Hostname) == 0 {
			pod.Spec.Hostname = pod.Name
		}
		if len(pod.Spec.Subdomain) == 0 {
			pod.Spec.Subdomain = job.Name
		}
	}
	return nil
}

func (p *mpi) OnJobAdd(job *vkv1.Job) error {
	p.ssh.OnJobAdd(job)

	// Generate MPI_HOST
	if len(job.Spec.Tasks) <= p.worker {
		return fmt.Errorf("invalid MPI job, should contains at least launcher and worker")
	}
	data := svc.GenerateHost(job)
	key := fmt.Sprintf(svc.ConfigMapTaskHostFmt, job.Spec.Tasks[p.worker].Name)

	// create MPI hostfile configmap
	hosts := strings.Split(data[key], "\n")
	for i, host := range hosts {
		hosts[i] = fmt.Sprintf("%s slots=%d", host, p.slotsPerWorker)
	}

	mpiHostFileData := map[string]string{HOST_FILE: strings.Join(hosts, "\n")}
	if err := helpers.CreateConfigMapIfNotExist(job, p.clientset.KubeClients, mpiHostFileData, p.cmName(job)); err != nil {
		return err
	}

	if err := svc.CreateServiceIfNotExist(p.clientset.KubeClients, job); err != nil {
		return err
	}

	return nil
}

func (p *mpi) OnJobDelete(job *vkv1.Job) error {
	// resource clean up
	p.ssh.OnJobDelete(job)

	if err := helpers.DeleteConfigmap(job, p.clientset.KubeClients, p.cmName(job)); err != nil {
		return err
	}

	return nil
}

func (p *mpi) addFlags() {
	flagSet := flag.NewFlagSet(p.Name(), flag.ContinueOnError)
	flagSet.IntVar(&p.launcher, "launcher", 0, "The mpi launcher index of the job.spec.tasks, by default is 0.")
	flagSet.IntVar(&p.worker, "worker", 1, "The mpi worker index of the job.spec.tasks, by default is 1.")
	flagSet.IntVar(&p.slotsPerWorker, "slots-per-worker", 1, "Slots per worker, by default is 1.")

	if err := flagSet.Parse(p.pluginArguments); err != nil {
		glog.Errorf("plugin %s flagset parse failed, err: %v", p.Name(), err)
	}
	return
}

func (p *mpi) isLauncher(pod *v1.Pod, job *vkv1.Job) bool {
	taskIndex := -1
	for i := range job.Spec.Tasks {
		if job.Spec.Tasks[i].Name == pod.Annotations[vkv1.TaskSpecKey] {
			taskIndex = i
		}
	}

	if taskIndex == p.launcher {
		return true
	}

	return false
}

func (p *mpi) cmName(job *vkv1.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, p.Name())
}
