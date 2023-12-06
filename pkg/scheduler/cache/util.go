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
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"stathat.com/c/consistent"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

// responsibleForPod returns false at following conditions:
// 1. The current scheduler is not specified scheduler in Pod's spec.
// 2. The Job which the Pod belongs is not assigned to current scheduler based on the hash algorithm in multi-schedulers scenario
func responsibleForPod(pod *v1.Pod, schedulerNames []string, mySchedulerPodName string, c *consistent.Consistent) bool {
	if !commonutil.Contains(schedulerNames, pod.Spec.SchedulerName) {
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

type TestArg struct {
	PodGroups []*scheduling.PodGroup
	Pods      []*v1.Pod
	Nodes     []*v1.Node
	Queues    []*scheduling.Queue
	Pvs       []*v1.PersistentVolume
	Pvcs      []*v1.PersistentVolumeClaim
	Sc        *storagev1.StorageClass
}

func CreateCacheForTest(testArg *TestArg, highPriority, lowPriority int32) (*SchedulerCache, *util.FakeBinder, *util.FakeEvictor, *util.FakeVolumeBinder) {
	binder := &util.FakeBinder{
		Binds:   map[string]string{},
		Channel: make(chan string, 1),
	}
	evictor := &util.FakeEvictor{
		Channel: make(chan string),
	}

	volumenBinder := &util.FakeVolumeBinder{}
	if testArg.Sc != nil {
		kubeClient := fake.NewSimpleClientset()
		kubeClient.StorageV1().StorageClasses().Create(context.TODO(), testArg.Sc, metav1.CreateOptions{})
		for _, pv := range testArg.Pvs {
			kubeClient.CoreV1().PersistentVolumes().Create(context.TODO(), pv, metav1.CreateOptions{})
		}
		for _, pvc := range testArg.Pvcs {
			kubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
		}

		volumenBinder = util.NewFakeVolumeBinder(kubeClient)
	}

	schedulerCache := &SchedulerCache{
		Nodes:           make(map[string]*api.NodeInfo),
		Jobs:            make(map[api.JobID]*api.JobInfo),
		Queues:          make(map[api.QueueID]*api.QueueInfo),
		Binder:          binder,
		Evictor:         evictor,
		StatusUpdater:   &util.FakeStatusUpdater{},
		VolumeBinder:    volumenBinder,
		PriorityClasses: make(map[string]*schedulingv1.PriorityClass),

		Recorder: record.NewFakeRecorder(100),
	}
	schedulerCache.PriorityClasses["high-priority"] = &schedulingv1.PriorityClass{
		Value: highPriority,
	}
	schedulerCache.PriorityClasses["low-priority"] = &schedulingv1.PriorityClass{
		Value: lowPriority,
	}

	for _, node := range testArg.Nodes {
		schedulerCache.AddOrUpdateNode(node)
	}
	for _, pod := range testArg.Pods {
		schedulerCache.AddPod(pod)
	}
	for _, ss := range testArg.PodGroups {
		schedulerCache.AddPodGroupV1beta1(ss)
	}
	for _, q := range testArg.Queues {
		schedulerCache.AddQueueV1beta1(q)
	}

	return schedulerCache, binder, evictor, volumenBinder
}
