/*
Copyright 2025 The Kubernetes Authors.

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

package scheduler

import (
	"context"
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// SchedulerSimulator scheduler simulator
type SchedulerSimulator struct {
	*Scheduler
	DecisionRecorder *DecisionRecorder
	KubeClient       *kubernetes.Clientset
}

// NewSchedulerSimulator create a new simulator scheduler
func NewSchedulerSimulator(config *rest.Config, opt *options.ServerOption) (*SchedulerSimulator, error) {
	baseScheduler, err := NewScheduler(config, opt)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &SchedulerSimulator{
		Scheduler:  baseScheduler,
		KubeClient: kubeClient,
	}, nil
}

// Run initializes and starts the SchedulerSimulator. It loads the configuration,
// initializes the cache, and begins the scheduling process.
func (ss *SchedulerSimulator) Run(stopCh <-chan struct{}) {
	ss.Scheduler.loadSchedulerConf()
	go ss.Scheduler.watchSchedulerConf(stopCh)
	// Start cache for policy.
	ss.Scheduler.cache.SetMetricsConf(ss.Scheduler.metricsConf)
	ss.Scheduler.cache.Run(stopCh)
	klog.V(2).Infof("SchedulerSimulator completes Initialization and start to run")
	go wait.Until(ss.runOnce, ss.Scheduler.schedulePeriod, stopCh)
	if options.ServerOpts.EnableCacheDumper {
		ss.Scheduler.dumper.ListenForSignal(stopCh)
	}
	go runSchedulerSocket()
}

// runOnce executes a single scheduling cycle. This function is called periodically
// as defined by the SchedulerSimulator's schedule period.
func (ss *SchedulerSimulator) runOnce() {
	klog.V(4).Infof("Start SchedulerSimulator ...")
	scheduleStartTime := time.Now()
	defer klog.V(4).Infof("End SchedulerSimulator ...")

	ss.Scheduler.mutex.Lock()
	actions := ss.Scheduler.actions
	plugins := ss.Scheduler.plugins
	configurations := ss.Scheduler.configurations
	ss.Scheduler.mutex.Unlock()

	// Load ConfigMap to check which action is enabled.
	conf.EnabledActionMap = make(map[string]bool)
	for _, action := range actions {
		conf.EnabledActionMap[action.Name()] = true
	}

	ssn := framework.OpenSession(ss.Scheduler.cache, plugins, configurations)
	defer func() {
		framework.CloseSession(ssn)
		metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
	}()

	// Initialization decision recorder
	ss.DecisionRecorder = NewDecisionRecorder(ssn, ss.KubeClient)

	// Wrap plug-in functions to document the decision process
	ss.wrapPluginFunctions(ssn)

	for _, action := range actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}

	// Write decision information to the Pod
	if err := ss.DecisionRecorder.WriteDecisionsToPod(context.TODO()); err != nil {
		klog.Errorf("Failed to write decisions to pods: %v", err)
	}
}

// wrapPluginFunctions Wrap plug-in functions to document the decision-making process
func (ss *SchedulerSimulator) wrapPluginFunctions(ssn *framework.Session) {
	// wrap Predicate function
	ssn.PostPredicateFn = func(task *api.TaskInfo, node *api.NodeInfo, err error) {
		klog.Infof("[DEBUG] Predicate function called for task %s on node %s with error: %v", task.Name, node.Name, err)
		ss.DecisionRecorder.RecordPredicate(task, node, err)
	}

	// wrap NodeOrder function
	ssn.PostNodeOrderFn = func(task *api.TaskInfo, node *api.NodeInfo, score float64, err error) {
		klog.Infof("[DEBUG] NodeOrder function called for task %s on node %s with score: %f and error: %v", task.Name, node.Name, score, err)
		ss.DecisionRecorder.RecordPriority(task, node, score, err)
	}

	// wrap the Allocate function to record the final allocation decision
	ssn.PostAllocateFn = func(task *api.TaskInfo, node *api.NodeInfo, err error) {
		klog.Infof("[DEBUG] Allocate function called for task %s on node %s with error: %v", task.Name, node.Name, err)
		ss.DecisionRecorder.RecordAllocation(task, node, err)
	}
}

// DecisionRecorder Intermediate process used to record scheduling decisions
type DecisionRecorder struct {
	Session         *framework.Session
	KubeClient      *kubernetes.Clientset
	DecisionDetails map[string]interface{}
}

// NewDecisionRecorder Create a new decision recorder
func NewDecisionRecorder(sess *framework.Session, client *kubernetes.Clientset) *DecisionRecorder {
	return &DecisionRecorder{
		Session:         sess,
		KubeClient:      client,
		DecisionDetails: make(map[string]interface{}),
	}
}

// RecordPredicate Record the results of node filtering
func (dr *DecisionRecorder) RecordPredicate(task *api.TaskInfo, node *api.NodeInfo, err error) {
	taskID := string(task.UID)
	if _, exists := dr.DecisionDetails[taskID]; !exists {
		dr.DecisionDetails[taskID] = make(map[string]interface{})
	}

	predicateRecord := map[string]interface{}{
		"node":   node.Name,
		"result": err == nil, // if there is no error, it means the prediction is successful
		"time":   time.Now().Format(time.RFC3339),
	}
	if err != nil {
		predicateRecord["error"] = err.Error()
	}

	details := dr.DecisionDetails[taskID].(map[string]interface{})
	if _, exists := details["predicates"]; !exists {
		details["predicates"] = []interface{}{}
	}
	details["predicates"] = append(details["predicates"].([]interface{}), predicateRecord)
}

// RecordPriority Record the results of node scoring
func (dr *DecisionRecorder) RecordPriority(task *api.TaskInfo, node *api.NodeInfo, score float64, err error) {
	taskID := string(task.UID)
	if _, exists := dr.DecisionDetails[taskID]; !exists {
		dr.DecisionDetails[taskID] = make(map[string]interface{})
	}

	priorityRecord := map[string]interface{}{
		"node":   node.Name,
		"score":  score,
		"plugin": "",
		"time":   time.Now().Format(time.RFC3339),
	}

	details := dr.DecisionDetails[taskID].(map[string]interface{})
	if _, exists := details["priorities"]; !exists {
		details["priorities"] = []interface{}{}
	}
	details["priorities"] = append(details["priorities"].([]interface{}), priorityRecord)
}

// RecordAllocation Record the final allocation decision
func (dr *DecisionRecorder) RecordAllocation(task *api.TaskInfo, node *api.NodeInfo, err error) {
	taskID := string(task.UID)
	if _, exists := dr.DecisionDetails[taskID]; !exists {
		dr.DecisionDetails[taskID] = make(map[string]interface{})
	}

	allocationRecord := map[string]interface{}{
		"node":    node.Name,
		"time":    time.Now().Format(time.RFC3339),
		"success": err == nil,
	}
	if err != nil {
		allocationRecord["error"] = err.Error()
	}

	details := dr.DecisionDetails[taskID].(map[string]interface{})
	details["allocated"] = allocationRecord
}

// WriteDecisionsToPod Write the scheduling decision information to the Pod
func (dr *DecisionRecorder) WriteDecisionsToPod(ctx context.Context) error {
	for taskID, details := range dr.DecisionDetails {
		// Get the pod corresponding to the task
		for _, job := range dr.Session.Jobs {
			for _, task := range job.Tasks {
				if string(task.UID) == taskID {
					// 将决策详情转换为JSON
					detailsJSON, err := json.Marshal(details)
					if err != nil {
						klog.Errorf("Failed to marshal decision details: %v", err)
						continue
					}

					// 更新Pod的annotations
					pod := task.Pod
					if pod.Annotations == nil {
						pod.Annotations = make(map[string]string)
					}

					pod.Annotations["volcano.sh/scheduling-decisions"] = string(detailsJSON)

					// 通过API更新Pod
					_, err = dr.KubeClient.CoreV1().Pods(pod.Namespace).Update(
						ctx, pod, metav1.UpdateOptions{})
					if err != nil {
						klog.Errorf("Failed to update pod %s/%s: %v", pod.Namespace, pod.Name, err)
						continue
					}

					klog.Infof("Updated scheduling decisions for pod %s/%s", pod.Namespace, pod.Name)
					break
				}
			}
		}
	}
	return nil
}
