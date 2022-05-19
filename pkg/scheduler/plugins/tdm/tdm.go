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

package tdm

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	tutil "volcano.sh/volcano/pkg/scheduler/plugins/util"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "tdm"
	// revocableZoneLayout revocable zone layout
	revocableZoneLayout      = "15:04"
	revocableZoneLabelPrefix = "tdm.revocable-zone."
	evictPeriodLabel         = "tdm.evict.period"
	defaultPodEvictNum       = 1
)

var lastEvictAt time.Time

/*
   actions: "enqueue, reclaim, allocate, preempt"
   tiers:
   - plugins:
     - name: tdm
       arguments:
         tdm.revocable-zone.rz1: 10:00-21:00
         tdm.revocable-zone.rz2: 12:00-14:00
         tdm.evict.period: 1m
*/

type tdmPlugin struct {
	revocableZone map[string]string
	// evictPeriod
	// default 1m
	evictPeriod time.Duration
}

// New function returns prioritizePlugin object
func New(args framework.Arguments) framework.Plugin {
	revocableZone := make(map[string]string)
	evictPeriod := time.Minute

	for k, v := range args {
		if strings.Contains(k, revocableZoneLabelPrefix) {
			revocableZone[strings.Replace(k, revocableZoneLabelPrefix, "", 1)] = v.(string)
		}
	}

	if period, ok := args[evictPeriodLabel]; ok {
		if d, err := time.ParseDuration(period.(string)); err == nil {
			evictPeriod = d
		}
	}

	return &tdmPlugin{revocableZone, evictPeriod}
}

func (tp *tdmPlugin) Name() string {
	return PluginName
}

func parseRevocableZone(rzRaw string) (start, end time.Time, err error) {
	rzValues := strings.Split(strings.TrimSpace(rzRaw), "-")

	if len(rzValues) != 2 {
		err = fmt.Errorf("revocable zone %v format error", rzRaw)
		return
	}

	t1, err := time.Parse(revocableZoneLayout, rzValues[0])
	if err != nil {
		return
	}

	t2, err := time.Parse(revocableZoneLayout, rzValues[1])
	if err != nil {
		return
	}

	now := time.Now()

	start = time.Date(now.Year(), now.Month(), now.Day(), t1.Hour(), t1.Minute(), 0, 0, now.Location())
	if t1.After(t2) || t1.Equal(t2) {
		end = time.Date(now.Year(), now.Month(), now.Day()+1, t2.Hour(), t2.Minute(), 0, 0, now.Location())
	} else {
		end = time.Date(now.Year(), now.Month(), now.Day(), t2.Hour(), t2.Minute(), 0, 0, now.Location())
	}

	return
}

func (tp *tdmPlugin) availableRevocableZone(rz string) error {
	// rzRaw format 00:00-23:59
	rzRaw, ok := tp.revocableZone[rz]
	if !ok {
		return fmt.Errorf("revocable zone %v not support", rz)
	}

	now := time.Now()

	start, end, err := parseRevocableZone(rzRaw)
	if err != nil {
		return err
	}

	if now.Unix() < start.Unix() || now.Unix() > end.Unix() {
		return fmt.Errorf("current time beyond revocable zone %v:%v", rz, rzRaw)
	}

	return nil
}

func (tp *tdmPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter tdm plugin ...")
	if klog.V(4) {
		defer func() {
			klog.V(4).Infof("Leaving tdm plugin.")
		}()
	}

	// tdm plugin just handle revocable node
	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		if node.RevocableZone == "" {
			return nil
		}

		if err := tp.availableRevocableZone(node.RevocableZone); err != nil {
			return fmt.Errorf("plugin %s predicates %w", tp.Name(), err)
		}

		klog.V(4).Infof("TDM node %v revocable zone %v:%v is active", node.Name, node.RevocableZone, tp.revocableZone[node.RevocableZone])

		if len(task.RevocableZone) == 0 {
			msg := fmt.Sprintf("task %s/%s is not allow to dispatch to revocable node %s", task.Namespace, task.Name, node.Name)
			return fmt.Errorf("plugin %s predicates %s", tp.Name(), msg)
		}

		klog.V(4).Infof("TDM filter for Task %s/%s on node %s pass.", task.Namespace, task.Name, node.Name)
		return nil
	}

	// tdm plugin just handle revocable node
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		score := 0.0

		if node.RevocableZone == "" {
			return score, nil
		}

		if err := tp.availableRevocableZone(node.RevocableZone); err != nil {
			klog.V(4).Infof("TDM not available %s", err)
			return score, err
		}

		if len(task.RevocableZone) == 0 {
			klog.V(4).Infof("TDM task %s/%s is not allow to dispatch to revocable node %s", task.Namespace, task.Name, node.Name)
			return score, nil
		}

		score = float64(k8sFramework.MaxNodeScore)

		klog.V(4).Infof("TDM score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		return score, nil
	}

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		// for the preemptable or can use revocablezone workload, they can not preempt other tasks.
		if preemptor.Preemptable || len(preemptor.RevocableZone) > 0 {
			klog.V(4).Infof("TDM task %s/%s is preemptable, do nothing skip", preemptor.Namespace, preemptor.Name)
			return nil, tutil.Reject
		}

		var victims []*api.TaskInfo
		tasksMap := make(map[api.JobID][]*api.TaskInfo)

		// find preemptable tasks which appear on none revocable node
		for _, task := range preemptees {
			if !task.Preemptable || task.Status != api.Running {
				continue
			}

			node, ok := ssn.Nodes[task.NodeName]
			if !ok {
				continue
			}

			if node.RevocableZone != "" {
				continue
			}

			tasksMap[task.Job] = append(tasksMap[task.Job], task)
		}

		for jobID, preemptableTasks := range tasksMap {
			if job, ok := ssn.Jobs[jobID]; ok {
				victims = append(victims, tp.maxVictims(job, preemptableTasks)...)
			}
		}

		klog.V(4).Infof("TDM victims are %+v", victims)

		return victims, tutil.Permit
	}

	victimsFn := func([]*api.TaskInfo) []*api.TaskInfo {
		if lastEvictAt.Add(tp.evictPeriod).After(time.Now()) {
			klog.V(4).Infof("TDM next evict time at %v", lastEvictAt)
			return nil
		}

		klog.V(4).Infof("TDM start to find victims")

		// find preemptable task on timeout revocable zone node
		victims := make([]*api.TaskInfo, 0)
		for rz := range tp.revocableZone {
			if err := tp.availableRevocableZone(rz); err != nil {
				klog.V(4).Infof("TDM revocable zone %v disactive, %v", rz, err)
				// rz disactive, then evict preemptable tasks by job from the revocable node
				for jobID, preemtableTasks := range tp.revocableNodePreemptableTask(rz, ssn) {
					if job, ok := ssn.Jobs[jobID]; ok {
						victims = append(victims, tp.maxVictims(job, preemtableTasks)...)
					}
				}
			}
		}

		// need to consider concurrency?
		lastEvictAt = time.Now()

		klog.V(4).Infof("TDM got %v victims", len(victims))

		return victims
	}

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		if lv.Preemptable == rv.Preemptable {
			return 0
		}

		if !lv.Preemptable {
			return -1
		}

		return 1
	}

	jobPipelinedFn := func(obj interface{}) int {
		jobInfo := obj.(*api.JobInfo)
		occupied := jobInfo.WaitingTaskNum() + jobInfo.ReadyTaskNum()
		if occupied >= jobInfo.MinAvailable {
			return tutil.Permit
		}
		return tutil.Reject
	}

	jobStarvingFn := func(obj interface{}) bool {
		jobInfo := obj.(*api.JobInfo)
		// allow none preemptable elastic job (deployment) preempt task
		if jobInfo.Preemptable {
			return false
		}
		return len(jobInfo.TaskStatusIndex[api.Pending]) > 0
	}

	victimsFns := make([]api.VictimTasksFn, 0)
	victimsFns = append(victimsFns, victimsFn)
	ssn.AddPredicateFn(tp.Name(), predicateFn)
	ssn.AddNodeOrderFn(tp.Name(), nodeOrderFn)
	ssn.AddPreemptableFn(tp.Name(), preemptableFn)
	ssn.AddVictimTasksFns(tp.Name(), victimsFns)
	ssn.AddJobOrderFn(tp.Name(), jobOrderFn)
	ssn.AddJobPipelinedFn(tp.Name(), jobPipelinedFn)
	ssn.AddJobStarvingFns(tp.Name(), jobStarvingFn)
}

func (tp *tdmPlugin) maxVictims(job *api.JobInfo, victims []*api.TaskInfo) []*api.TaskInfo {
	maxPodEvictNum := tp.getMaxPodEvictNum(job)
	targetNum := util.GetMinInt(maxPodEvictNum, len(victims))
	klog.V(3).Infof("Job <%s/%s> max evict:%v, potential victims number:%v, max victims number:%v",
		job.Namespace, job.Name, maxPodEvictNum, len(victims), targetNum)

	return victims[:targetNum]
}

// get max pod evict number from job budget configure
func (tp *tdmPlugin) getMaxPodEvictNum(job *api.JobInfo) int {
	jobRunningTaskNum := len(job.TaskStatusIndex[api.Running])
	if job.Budget.MaxUnavilable != "" {
		maxUnavilable := tp.parseIntStr(job.Budget.MaxUnavilable, len(job.Tasks))
		finalTaskNum := len(job.TaskStatusIndex[api.Succeeded]) + len(job.TaskStatusIndex[api.Failed])
		realUnavilable := len(job.Tasks) - finalTaskNum - jobRunningTaskNum
		if realUnavilable >= maxUnavilable {
			return 0
		}
		return maxUnavilable - realUnavilable
	}

	if job.Budget.MinAvailable != "" {
		minAvailable := tp.parseIntStr(job.Budget.MinAvailable, len(job.Tasks))
		if jobRunningTaskNum >= minAvailable {
			return jobRunningTaskNum - minAvailable
		}
	}

	return defaultPodEvictNum
}

func (tp *tdmPlugin) parseIntStr(input string, taskNum int) int {
	resultValue := 0
	tmp := intstr.Parse(input)
	switch tmp.Type {
	case intstr.Int:
		resultValue = tmp.IntValue()
	case intstr.String:
		if v, err := intstr.GetValueFromIntOrPercent(&tmp, taskNum, true); err == nil {
			resultValue = v
		} else {
			klog.Warningf("TDM get percent value err: %v", err)
		}
	}

	return resultValue
}

func (tp *tdmPlugin) revocableNodePreemptableTask(rz string, ssn *framework.Session) map[api.JobID][]*api.TaskInfo {
	tasksMap := make(map[api.JobID][]*api.TaskInfo)
	for _, node := range ssn.RevocableNodes {
		if node.RevocableZone != rz {
			continue
		}

		for _, task := range node.Tasks {
			if task.Preemptable {
				if task.Status == api.Running {
					tasksMap[task.Job] = append(tasksMap[task.Job], task)
				}
			}
		}
	}

	return tasksMap
}

func (tp *tdmPlugin) OnSessionClose(ssn *framework.Session) {}
