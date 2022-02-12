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

package cache

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"stathat.com/c/consistent"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// responsibleForPod returns false at following conditions:
// 1. The current scheduler is not specified scheduler in Pod's spec.
// 2. The Job which the Pod belongs is not assigned to current scheduler based on the hash algorithm in multi-schedulers scenario
func responsibleForPod(pod *v1.Pod, schedulerName string, mySchedulerPodName string, c *consistent.Consistent) bool {
	if schedulerName != pod.Spec.SchedulerName {
		return false
	}
	if c != nil {
		var key string
		if len(pod.OwnerReferences) != 0 {
			key = pod.OwnerReferences[0].Name
		} else {
			key = pod.Name
		}
		schedulerPodName, err := c.Get(key)
		if err != nil {
			klog.Errorf("Failed to get scheduler by hash algorithm, err: %v", err)
		}
		if schedulerPodName != mySchedulerPodName {
			return false
		}
	}

	klog.V(4).Infof("schedulerPodName %v is responsible to Pod %v/%v", mySchedulerPodName, pod.Namespace, pod.Name)
	return true
}

// responsibleForNode returns true if the Node is assigned to current scheduler in multi-scheduler scenario
func responsibleForNode(nodeName string, mySchedulerPodName string, c *consistent.Consistent) bool {
	if c != nil {
		schedulerPodName, err := c.Get(nodeName)
		if err != nil {
			klog.Errorf("Failed to get scheduler by hash algorithm, err: %v", err)
		}
		if schedulerPodName != mySchedulerPodName {
			return false
		}
	}

	klog.V(4).Infof("schedulerPodName %v is responsible to Node %v", mySchedulerPodName, nodeName)
	return true
}

// responsibleForPodGroup returns true if Job which PodGroup belongs is assigned to current scheduler in multi-schedulers scenario
func responsibleForPodGroup(pg *scheduling.PodGroup, mySchedulerPodName string, c *consistent.Consistent) bool {
	if c != nil {
		var key string
		if len(pg.OwnerReferences) != 0 {
			key = pg.OwnerReferences[0].Name
		} else {
			key = pg.Name
		}
		schedulerPodName, err := c.Get(key)
		if err != nil {
			klog.Errorf("Failed to get scheduler by hash algorithm, err: %v", err)
		}
		if schedulerPodName != mySchedulerPodName {
			return false
		}
	}

	klog.V(4).Infof("schedulerPodName %v is responsible to PodGroup %v/%v", mySchedulerPodName, pg.Namespace, pg.Name)
	return true
}

// getMultiSchedulerInfo return the Pod name of current scheduler and the hash table for all schedulers
func getMultiSchedulerInfo() (schedulerPodName string, c *consistent.Consistent) {
	multiSchedulerEnable := os.Getenv("MULTI_SCHEDULER_ENABLE")
	mySchedulerPodName := os.Getenv("SCHEDULER_POD_NAME")
	c = nil
	if multiSchedulerEnable == "true" {
		klog.V(3).Infof("multiSchedulerEnable true")
		schedulerNumStr := os.Getenv("SCHEDULER_NUM")
		schedulerNum, err := strconv.Atoi(schedulerNumStr)
		if err != nil {
			schedulerNum = 1
		}
		index := strings.LastIndex(mySchedulerPodName, "-")
		baseName := mySchedulerPodName[0:index]
		c = consistent.New()
		for i := 0; i < schedulerNum; i++ {
			name := fmt.Sprintf("%s-%d", baseName, i)
			c.Add(name)
		}
	}
	return mySchedulerPodName, c
}
