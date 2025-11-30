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

package cache

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	vcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

type PodScheduleResult struct {
	Task             *api.TaskInfo
	BindContext      *vcache.BindContext
	SuggestedNodes   []*api.NodeInfo
	ScheduleCycleUID types.UID
}

type ConflictAwareBinder struct {
	// cache Cache
	// BindCheckChannel is used to store allocate result for bind
	BindCheckChannel chan *PodScheduleResult
	nodeBindRecords  map[string]int64
	recordsMutex     sync.Mutex
	cache            Cache
}

func NewConflictAwareBinder(schedulerCache Cache) *ConflictAwareBinder {
	return &ConflictAwareBinder{
		nodeBindRecords:  make(map[string]int64, 0),
		BindCheckChannel: make(chan *PodScheduleResult, 5000),
		cache:            schedulerCache,
	}
}
func (binder *ConflictAwareBinder) Run(stopCh <-chan struct{}) {
	go wait.Until(binder.processScheduleResult, 0, stopCh)
}

func (binder *ConflictAwareBinder) processScheduleResult() {
	for {
		select {
		case scheduleResult, ok := <-binder.BindCheckChannel:
			if !ok {
				return
			}
			binder.CheckAndBindPod(scheduleResult)
		default:
		}
		if len(binder.BindCheckChannel) == 0 {
			break
		}
	}
}

// CheckAndBindPod check the pod schedule result, send pod for binding if no conflict. Put pod back to schedule queue if there is conflict
func (binder *ConflictAwareBinder) CheckAndBindPod(scheduleResult *PodScheduleResult) {
	//1. Check conflict
	node := binder.FindNonConflictingNode(scheduleResult)
	if node == nil {
		klog.V(5).Infof("%d candidates of pod %s/%s are conflict with previouse bind node, put back to queue for retry", len(scheduleResult.SuggestedNodes), scheduleResult.Task.Namespace, scheduleResult.Task.Name)
		//TODO: Put pod back to queue if conflict
		return
	}

	//2. Bind pod if no conflict
	task := scheduleResult.Task
	task.NodeName = node.Name
	task.Pod.Spec.NodeName = node.Name
	nodeBindGeneration := node.BindGeneration
	if err := binder.cache.AddBindTask(scheduleResult.BindContext); err != nil {
		//TODO: Put pod back to queue if conflict
		return
	}
	binder.recordsMutex.Lock()
	defer binder.recordsMutex.Unlock()
	binder.nodeBindRecords[node.Name] = nodeBindGeneration
	metrics.UpdateTaskScheduleDuration(metrics.Duration(task.Pod.CreationTimestamp.Time))
}

// FindNonConflictingNode return node if version of candidate node is newer than the version of node used in last bind
func (binder *ConflictAwareBinder) FindNonConflictingNode(scheduleResult *PodScheduleResult) *api.NodeInfo {
	binder.recordsMutex.Lock()
	defer binder.recordsMutex.Unlock()
	for _, node := range scheduleResult.SuggestedNodes {
		if lastBindGeneration, ok := binder.nodeBindRecords[node.Name]; ok {
			if node.BindGeneration > lastBindGeneration {
				return node
			}
		} else {
			return node
		}
	}
	return nil
}

func (binder *ConflictAwareBinder) EnqueueScheduleResult(scheduleResult *PodScheduleResult) {
	binder.BindCheckChannel <- scheduleResult
}

func (binder *ConflictAwareBinder) RemoveBindRecord(nodeName string) {
	binder.recordsMutex.Lock()
	defer binder.recordsMutex.Unlock()
	delete(binder.nodeBindRecords, nodeName)
}
