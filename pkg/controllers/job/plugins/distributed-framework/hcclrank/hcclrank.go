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
	workerNames       []string
}

// New creates hcclrank plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	hp := hcclrankPlugin{hcclrankArguments: arguments, clientset: client}
	hp.addFlags()
	return &hp
}

func (hp *hcclrankPlugin) addFlags() {
	worker := ""
	workers := ""
	flagSet := flag.NewFlagSet(hp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&hp.masterName, "master", DefaultMaster, "name of master role task")
	flagSet.StringVar(&worker, "worker", "", "name of worker role task, defaults to "+DefaultWorker)
	flagSet.StringVar(&workers, "workers", "", "comma separated list of worker role task names, takes precedence over worker when set")
	if err := flagSet.Parse(hp.hcclrankArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", hp.Name(), err)
	}
	hp.workerNames = helpers.ResolveWorkerNames(worker, workers)
	if len(hp.workerNames) == 0 {
		hp.workerNames = []string{DefaultWorker}
	}
}

func (hp *hcclrankPlugin) Name() string {
	return HCCLRankPluginName
}

func (hp *hcclrankPlugin) isWorkerTask(taskName string) bool {
	for _, workerName := range hp.workerNames {
		if workerName == taskName {
			return true
		}
	}
	return false
}

// getWorkerRankBase returns the rank base of the given worker task, which is
// the total replicas of all worker tasks listed before it in the worker
// argument, so ranks stay unique across worker tasks.
func (hp *hcclrankPlugin) getWorkerRankBase(taskName string, job *batch.Job) int {
	base := 0
	for _, workerName := range hp.workerNames {
		if workerName == taskName {
			break
		}
		base += int(helpers.GetTaskReplicasUnderJob(workerName, job))
	}
	return base
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
	isWorker := hp.isWorkerTask(taskType)
	if taskType != hp.masterName && !isWorker {
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
		// Calculate rank: master replicas + replicas of worker tasks listed
		// before this one + pod index, keeping ranks unique across tasks.
		rank := int(masterReplicas) + hp.getWorkerRankBase(taskType, job) + taskIndex
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
