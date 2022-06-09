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

package shuffle

import (
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// Shuffle indicates the action name
	Shuffle = "shuffle"
)

// Action defines the action
type Action struct{}

// New returns the action instance
func New() *Action {
	return &Action{}
}

// Name returns the action name
func (shuffle *Action) Name() string {
	return Shuffle
}

// Initialize inits the action
func (shuffle *Action) Initialize() {}

// Execute select evictees according given strategies and evict them.
func (shuffle *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infoln("Enter Shuffle ...")
	defer klog.V(3).Infoln("Leaving Shuffle ...")

	// select pods that may be evicted
	tasks := make([]*api.TaskInfo, 0)
	for _, jobInfo := range ssn.Jobs {
		for _, taskInfo := range jobInfo.Tasks {
			if taskInfo.Status == api.Running {
				tasks = append(tasks, taskInfo)
			}
		}
	}

	// Evict target workloads
	victims := ssn.VictimTasks(tasks)
	for victim := range victims {
		klog.V(3).Infof("pod %s from namespace %s and job %s will be evicted.\n", victim.Name, victim.Namespace, string(victim.Job))
		if err := ssn.Evict(victim, "shuffle"); err != nil {
			klog.Errorf("Failed to evict Task <%s/%s>: %v\n", victim.Namespace, victim.Name, err)
			continue
		}
	}
}

// UnInitialize releases resource which is not useful.
func (shuffle *Action) UnInitialize() {}
