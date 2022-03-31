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

package reserve

import (
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// Action defines the action
type Action struct{}

// New returns the action instance
func New() *Action {
	return &Action{}
}

// Name returns the action name
func (reserve *Action) Name() string {
	return "reserve"
}

// Initialize inits the action
func (reserve *Action) Initialize() {}

// Execute selects a node which is not locked and has the most idle resource
func (reserve *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Reserve ...")
	defer klog.V(3).Infof("Leaving Reserve ...")
	if util.Reservation.TargetJob == nil {
		klog.V(4).Infof("No target job, skip reserve action")
		return
	}
	// if target job has not been scheduled, select a locked node for it
	// else reset target job and locked nodes
	targetJob := ssn.Jobs[util.Reservation.TargetJob.UID]
	if targetJob == nil {
		// targetJob is deleted
		klog.V(3).Infof("Target Job has been deleted. Reset target job")
		util.Reservation.TargetJob = nil
		for node := range util.Reservation.LockedNodes {
			delete(util.Reservation.LockedNodes, node)
		}
		return
	}

	util.Reservation.TargetJob = targetJob

	if !util.Reservation.TargetJob.Ready() {
		ssn.ReservedNodes()
	} else {
		klog.V(3).Infof("Target Job has been scheduled. Reset target job")
		util.Reservation.TargetJob = nil
		for node := range util.Reservation.LockedNodes {
			delete(util.Reservation.LockedNodes, node)
		}
	}
}

// UnInitialize releases resource which are not useful.
func (reserve *Action) UnInitialize() {}
