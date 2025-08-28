/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// NewHandler new fault policy handler
func NewHandler() plugin.FaultHandler {
	return &ReScheduler{}
}

// Execute pre-processing actions for rescheduler handler
func (reScheduler *ReScheduler) Execute(env *plugin.ScheduleEnv, ssn *framework.Session) error {
	klog.V(util.LogInfoLev).Infof("Entering reScheduler Execute")
	defer klog.V(util.LogInfoLev).Infof("Leaving reScheduler Execute")
	if reScheduler == nil || ssn == nil || env == nil {
		return fmt.Errorf("reScheduler handler not enabled or ssn is nil: %s", util.ArgumentError)
	}

	reScheduler.initialize(env)
	reScheduler.AddFaultNodeWithSession()
	reScheduler.synCacheFaultJobWithSession(ssn)
	reScheduler.SyncJobRemainRetryTimes(ssn)
	reScheduler.SyncJobRecentRescheduleReason(ssn)
	// 1. restart Fault Jobs that are recorded in cache
	if restartErr := reScheduler.RestartNeedForceDeleteJobs(ssn, *env); restartErr != nil &&
		restartErr.Error() != util.ArgumentError {
		klog.V(util.LogErrorLev).Infof("RestartNeedForceDeleteJobs: %s", restartErr.Error())
	}
	// 2. get all jobs in session
	runningJobs := reScheduler.GetRunningJobs(ssn)
	// 3. get nodes of session and fault jobs
	if err := reScheduler.AddFaultJobWithSession(runningJobs, *env); err != nil {
		klog.V(util.LogErrorLev).Infof("AddFaultJobWithSession %s", err)
	}
	// 4. restart the fault jobs
	if restartErr := reScheduler.RestartFaultJobs(ssn, *env); restartErr != nil {
		klog.V(util.LogErrorLev).Infof("RestartFaultJobs: %s", restartErr.Error())
		return restartErr
	}
	return nil
}

// PreStopAction post-processing actions for re-scheduling
func (reScheduler *ReScheduler) PreStopAction(env *plugin.ScheduleEnv) error {
	if reScheduler == nil || env == nil {
		return fmt.Errorf("reSchedule not enabled or nil env: %s", util.ArgumentError)
	}
	if err := reScheduler.WriteReSchedulerCacheToEnvCache(env, CmFaultJob); err != nil {
		return err
	}
	return nil
}

// initialize init ReScheduler
func (reScheduler *ReScheduler) initialize(env *plugin.ScheduleEnv) {
	// 1. Initialise ReScheduler.graceDeleteTime
	klog.V(util.LogDebugLev).Infof("Initialising graceDeleteTime.")
	reScheduler.setGraceOverTime(env.FrameAttr.GraceDeleteTime)
	reScheduler.DealReSchedulerCache = reSchedulerCache // 2.4 set DealReSchedulerCache
	if recordErr := reSchedulerCache.SetJobRecentRescheduleRecords(env.FrameAttr.IsFirstSession,
		env.FrameAttr.KubeClient); recordErr != nil {
		klog.V(util.LogErrorLev).Infof("SetJobRecentRescheduleRecords: %s", util.SafePrint(recordErr))
	}
	reScheduler.Jobs = env.Jobs // 3 Initialise session Jobs Nodes copying data from env
	reScheduler.Nodes = env.Nodes
	reScheduler.isFirstSession = env.FrameAttr.IsFirstSession
}
