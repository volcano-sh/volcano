/*
Copyright 2021 The Volcano Authors.

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

package tensorflow

import (
	"encoding/json"
	"flag"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// TFPluginName is the name of the plugin
	TFPluginName = "tensorflow"
	// DefaultPort defines default port for service
	DefaultPort = 2222
	// TFConfig defines environment variables for TF
	TFConfig = "TF_CONFIG"
)

type tensorflowPlugin struct {
	tfArguments   []string
	Clientset     pluginsinterface.PluginClientset
	psName        string
	workerNames   []string
	chiefName     string
	evaluatorName string
	port          int
}

// New creates tensorflow plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	tp := tensorflowPlugin{tfArguments: arguments, Clientset: client}
	tp.addFlags()
	return &tp
}

func (tp *tensorflowPlugin) addFlags() {
	worker := ""
	workers := ""
	flagSet := flag.NewFlagSet(tp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&tp.psName, "ps", "ps", "name of ps role task")
	flagSet.StringVar(&worker, "worker", "", "name of worker role task, defaults to worker")
	flagSet.StringVar(&workers, "workers", "", "comma separated list of worker role task names, takes precedence over worker when set")
	flagSet.StringVar(&tp.chiefName, "chief", "chief", "name of chief role task")
	flagSet.StringVar(&tp.evaluatorName, "evaluator", "evaluator", "name of evaluator role task")
	flagSet.IntVar(&tp.port, "port", DefaultPort, "service port")
	if err := flagSet.Parse(tp.tfArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", tp.Name(), err)
	}
	tp.workerNames = jobhelpers.ResolveWorkerNames(worker, workers)
	if len(tp.workerNames) == 0 {
		tp.workerNames = []string{"worker"}
	}
}

func (tp *tensorflowPlugin) isWorkerTask(taskName string) bool {
	for _, workerName := range tp.workerNames {
		if workerName == taskName {
			return true
		}
	}
	return false
}

// getWorkerIndexBase returns the index base of the given worker task, which is
// the total replicas of all worker tasks listed before it in the worker
// argument. Indices of pods in the task are the base plus their own pod
// index, so indices stay unique across worker tasks and match their position
// in the cluster worker host list.
func (tp *tensorflowPlugin) getWorkerIndexBase(taskName string, job *batch.Job) int {
	base := 0
	for _, workerName := range tp.workerNames {
		if workerName == taskName {
			break
		}
		base += int(jobhelpers.GetTaskReplicasUnderJob(workerName, job))
	}
	return base
}

func (tp *tensorflowPlugin) Name() string {
	return TFPluginName
}

func (tp *tensorflowPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// No need to generate TF_CONFIG for stand-alone tensorflow job
	if len(job.Spec.Tasks) == 1 && job.Spec.Tasks[0].Replicas == 1 {
		return nil
	}
	// Generate TF_CONFIG value
	spec, err := tp.generateTFClusterSpec(pod, job)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}

	// Add TF_CONFIG environment variables
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
			Name:  TFConfig,
			Value: string(raw),
		})
	}
	return nil
}

func (tp *tensorflowPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+tp.Name()] == tp.Name() {
		return nil
	}

	job.Status.ControlledResources["plugin-"+tp.Name()] = tp.Name()

	return nil
}

func (tp *tensorflowPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+tp.Name()] != tp.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+tp.Name())
	return nil
}

func (tp *tensorflowPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}

func (tp *tensorflowPlugin) generateTFClusterSpec(pod *v1.Pod, job *batch.Job) (tfClusterSpec, error) {
	taskType := jobhelpers.GetTaskKey(pod)
	index, err := strconv.Atoi(jobhelpers.GetPodIndexUnderTask(pod))
	if err != nil {
		return tfClusterSpec{}, err
	}
	if tp.isWorkerTask(taskType) {
		index += tp.getWorkerIndexBase(taskType, job)
	}

	// Generate tensorflow task info
	c := tfClusterSpec{
		Task: taskInfo{
			Type:  tp.getTaskType(taskType),
			Index: index,
		},
	}

	// Generate tensorflow cluster info
	for _, ts := range job.Spec.Tasks {
		hosts := []string{}
		for i := 0; i < int(ts.Replicas); i++ {
			hosts = append(hosts, fmt.Sprintf("%s:%d", jobhelpers.MakeDomainName(ts, job, i), tp.port))
		}
		switch ts.Name {
		case tp.psName:
			c.Cluster.PS = hosts
		case tp.chiefName:
			c.Cluster.Chief = hosts
		case tp.evaluatorName:
			c.Cluster.Evaluator = hosts
		}
	}
	// Worker hosts are appended in the order of the worker argument so each
	// host position matches the global worker index generated above.
	for _, workerName := range tp.workerNames {
		taskIndex := jobhelpers.GetTaskIndexUnderJob(workerName, job)
		if taskIndex == -1 {
			continue
		}
		ts := job.Spec.Tasks[taskIndex]
		for i := 0; i < int(ts.Replicas); i++ {
			c.Cluster.Worker = append(c.Cluster.Worker, fmt.Sprintf("%s:%d", jobhelpers.MakeDomainName(ts, job, i), tp.port))
		}
	}
	return c, nil
}

func (tp *tensorflowPlugin) getTaskType(taskKey string) tfTaskType {
	switch taskKey {
	case tp.chiefName:
		return tfChief
	case tp.psName:
		return tfPS
	case tp.evaluatorName:
		return tfEvaluator
	}
	if tp.isWorkerTask(taskKey) {
		return tfWorker
	}
	return tfTaskType(taskKey)
}

// TfClusterSpec is the spec of a tensorflow cluster
// It will be injected into container's environment variables, and be used by tensorflow framework.
// e.g.
//
//	{
//	   "cluster": {
//	     "worker": ["worker-0:2222", "worker-1:2222"],
//	     "ps": ["ps-0:2222"]
//	   },
//	   "task": {
//	     "type": "worker",
//	     "index": 0
//	   }
//	}
type tfClusterSpec struct {
	Cluster clusterInfo `json:"cluster"`
	Task    taskInfo    `json:"task"`
}

type clusterInfo struct {
	PS        []string `json:"ps,omitempty"`
	Worker    []string `json:"worker,omitempty"`
	Chief     []string `json:"chief,omitempty"`
	Evaluator []string `json:"evaluator,omitempty"`
}

type tfTaskType string

const (
	tfWorker    tfTaskType = "worker"
	tfChief     tfTaskType = "chief"
	tfPS        tfTaskType = "ps"
	tfEvaluator tfTaskType = "evaluator"
)

type taskInfo struct {
	Type  tfTaskType `json:"type"`
	Index int        `json:"index"`
}
