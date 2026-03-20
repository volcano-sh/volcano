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

package hcclrank

import (
	"flag"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// HCCLRankPluginName is the name of the plugin
	HCCLRankPluginName = "hcclrank"
	// DefaultMaster is the default task name of master host
	DefaultMaster = "master"
	// DefaultWorker is the default task name of worker host
	DefaultWorker = "worker"

	HCCLRankKey = "hccl/rankIndex"

	// EnvRank is the env name of rank
	EnvRank = "RANK"
)

type hcclrankPlugin struct {
	hcclrankArguments []string
	clientset         pluginsinterface.PluginClientset
	masterName        string
	workerName        string
}

// New creates hcclrank plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	hp := hcclrankPlugin{hcclrankArguments: arguments, clientset: client}
	hp.addFlags()
	return &hp
}

func (hp *hcclrankPlugin) addFlags() {
	flagSet := flag.NewFlagSet(hp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&hp.masterName, "master", DefaultMaster, "name of master role task")
	flagSet.StringVar(&hp.workerName, "worker", DefaultWorker, "name of worker role task")
	if err := flagSet.Parse(hp.hcclrankArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", hp.Name(), err)
	}
}

func (hp *hcclrankPlugin) Name() string {
	return HCCLRankPluginName
}

func getEnv(pod *v1.Pod, envName string) string {
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == envName {
				return env.Value
			}
		}
	}
	return ""
}

func (hp *hcclrankPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	if pod.Annotations[HCCLRankKey] != "" || job == nil {
		return nil
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	rank := getEnv(pod, EnvRank)
	if rank != "" {
		pod.Annotations[HCCLRankKey] = rank
		klog.V(4).Infof("Set %v=%s for pod %s in job %s", HCCLRankKey, rank, pod.Name, job.Name)
		return nil
	}

	taskType := helpers.GetTaskKey(pod)
	if taskType != hp.masterName && taskType != hp.workerName {
		return nil
	}

	taskIndex, err := helpers.GetTaskIndexOfPod(pod)
	if err != nil {
		klog.Errorf("failed to parse task index for pod: %v", err)
		return err
	}

	var hcclRank string
	if taskType == hp.masterName {
		hcclRank = strconv.Itoa(taskIndex)
	} else {
		masterReplicas := helpers.GetTaskReplicasUnderJob(hp.masterName, job)
		// Calculate rank: master replicas + pod index
		rank := int(masterReplicas) + taskIndex
		hcclRank = strconv.Itoa(rank)
	}

	pod.Annotations[HCCLRankKey] = hcclRank
	klog.V(4).Infof("Set %v=%s for pod %s in job %s", HCCLRankKey, hcclRank, pod.Name, job.Name)
	return nil
}

func (hp *hcclrankPlugin) OnJobAdd(job *batch.Job) error {
	// No specific action needed for job add
	return nil
}

func (hp *hcclrankPlugin) OnJobDelete(job *batch.Job) error {
	// No specific action needed for job delete
	return nil
}

func (hp *hcclrankPlugin) OnJobUpdate(job *batch.Job) error {
	// No specific action needed for job update
	return nil
}
