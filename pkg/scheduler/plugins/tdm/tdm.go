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
	"math"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "tdm"
	// revocableZoneLayout revocable zone layout
	revocableZoneLayout      = "15:04"
	revocableZoneLabelPrefix = "tdm.revocable-zone."
	evictPeriodLabel         = "tdm.evict.period"
	evictMaxNumLabel         = "tdm.evict.maxNum"
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
         tdm.evict.maxNum: 5%
*/

type tdmPlugin struct {
	revocableZone map[string]string
	// evictPeriod
	// default 1m
	evictPeriod time.Duration
	// evictMaxNum value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%)
	// default 5%
	evictMaxNum intstr.IntOrString
}

// New function returns prioritizePlugin object
func New(args framework.Arguments) framework.Plugin {
	revocableZone := make(map[string]string)
	evictPeriod := time.Minute
	evictMaxNum := intstr.FromString("5%")

	for k, v := range args {
		if strings.Contains(k, revocableZoneLabelPrefix) {
			revocableZone[strings.Replace(k, revocableZoneLabelPrefix, "", 1)] = v
		}
	}

	if period, ok := args[evictPeriodLabel]; ok {
		if d, err := time.ParseDuration(period); err == nil {
			evictPeriod = d
		}
	}

	if maxNum, ok := args[evictMaxNumLabel]; ok {
		evictMaxNum = intstr.Parse(maxNum)
	}

	return &tdmPlugin{revocableZone, evictPeriod, evictMaxNum}
}

func (bp *tdmPlugin) Name() string {
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

func (bp *tdmPlugin) availableRevocableZone(rz string) error {
	// rzRaw format 00:00-23:59
	rzRaw, ok := bp.revocableZone[rz]
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

func (bp *tdmPlugin) OnSessionOpen(ssn *framework.Session) {
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

		if err := bp.availableRevocableZone(node.RevocableZone); err != nil {
			return fmt.Errorf("plugin %s predicates %w", bp.Name(), err)
		}

		klog.V(4).Infof("TDM node %v revocable zone %v:%v is active", node.Name, node.RevocableZone, bp.revocableZone[node.RevocableZone])

		if !task.Preemptable {
			msg := fmt.Sprintf("task %s/%s is not preemptable task, skip to schedule to node %s", task.Namespace, task.Name, node.Name)
			return fmt.Errorf("plugin %s predicates %s", bp.Name(), msg)
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

		if err := bp.availableRevocableZone(node.RevocableZone); err != nil {
			klog.V(4).Infof("TDM not available %s", err)
			return score, err
		}

		if !task.Preemptable {
			klog.V(4).Infof("TDM task %s/%s is not preemptable task, skip to schedule to node %s", task.Namespace, task.Name, node.Name)
			return score, nil
		}

		score = float64(v1alpha1.MaxNodeScore)

		klog.V(4).Infof("TDM score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		return score, nil
	}

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
		if preemptor.Preemptable {
			klog.V(4).Infof("TDM task %s/%s is preemptable, do nothing skip", preemptor.Namespace, preemptor.Name)
			return nil
		}

		var victims []*api.TaskInfo

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

			victims = append(victims, task)
		}

		klog.V(4).Infof("TDM victims are %+v", victims)

		return victims
	}

	victimsFn := func() []*api.TaskInfo {
		if lastEvictAt.Add(bp.evictPeriod).After(time.Now()) {
			klog.V(4).Infof("TDM next evict time at %v", lastEvictAt)
			return nil
		}

		klog.V(4).Infof("TDM start to find victims")

		// find preemptable task on timeout revocable zone node
		victims := make([]*api.TaskInfo, 0)
		for rz := range bp.revocableZone {
			if err := bp.availableRevocableZone(rz); err != nil {
				klog.V(4).Infof("TDM revocable zone %v disactive, %v", rz, err)
				// rz disactive, then evict preemptable tasks from the revocable node
				victims = append(victims, bp.revocableNodePreemptableTask(rz, ssn)...)
			}
		}

		victims = bp.maxVictims(victims)
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

	jobPipelinedFn := func(obj interface{}) bool {
		jobInfo := obj.(*api.JobInfo)
		if jobInfo.Preemptable {
			// ignore preemptable job
			return true
		}
		// non preemptable job(high priority) try to feed all its tasks
		occupied := jobInfo.WaitingTaskNum() + jobInfo.ReadyTaskNum()
		return occupied >= (int32)(len(jobInfo.Tasks))

	}

	ssn.AddPredicateFn(bp.Name(), predicateFn)
	ssn.AddNodeOrderFn(bp.Name(), nodeOrderFn)
	ssn.AddPreemptableFn(bp.Name(), preemptableFn)
	ssn.AddVictimTasksFns(bp.Name(), victimsFn)
	ssn.AddJobOrderFn(bp.Name(), jobOrderFn)
	ssn.AddJobPipelinedFn(bp.Name(), jobPipelinedFn)
}

func (bp *tdmPlugin) maxVictims(victims []*api.TaskInfo) []*api.TaskInfo {
	targetNum := 0
	switch bp.evictMaxNum.Type {
	case intstr.Int:
		targetNum = int(math.Min(float64(bp.evictMaxNum.IntValue()), float64(len(victims))))
	case intstr.String:
		if v, err := intstr.GetValueFromIntOrPercent(&bp.evictMaxNum, len(victims), true); err == nil {
			targetNum = v
		} else {
			klog.V(4).Infof("TDM get percent value err: %v", err)
		}
	}

	return victims[:targetNum]
}

func (bp *tdmPlugin) revocableNodePreemptableTask(rz string, ssn *framework.Session) []*api.TaskInfo {
	tasks := make([]*api.TaskInfo, 0)
	for _, node := range ssn.Nodes {
		if node.RevocableZone != rz {
			continue
		}

		for _, task := range node.Tasks {
			if task.Preemptable {
				if task.Status == api.Running {
					tasks = append(tasks, task)
				}
			}
		}
	}

	return tasks
}

func (bp *tdmPlugin) noneRevocableNodePreemptableTask(ssn *framework.Session) []*api.TaskInfo {
	tasks := make([]*api.TaskInfo, 0)

	for _, node := range ssn.Nodes {
		if node.RevocableZone != "" {
			continue
		}

		for _, task := range node.Tasks {
			if task.Preemptable {
				if task.Status == api.Running {
					tasks = append(tasks, task)
				}
			}
		}
	}

	return tasks
}
func (bp *tdmPlugin) OnSessionClose(ssn *framework.Session) {}
