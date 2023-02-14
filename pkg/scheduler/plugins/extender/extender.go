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

package extender

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "extender"

	// ExtenderURLPrefix is the key for providing extender endpoint address
	ExtenderURLPrefix = "extender.urlPrefix"
	// ExtenderHTTPTimeout is the timeout for extender http calls
	ExtenderHTTPTimeout = "extender.httpTimeout"
	// ExtenderOnSessionOpenVerb is the verb of OnSessionOpen method
	ExtenderOnSessionOpenVerb = "extender.onSessionOpenVerb"
	// ExtenderOnSessionCloseVerb is the verb of OnSessionClose method
	ExtenderOnSessionCloseVerb = "extender.onSessionCloseVerb"
	// ExtenderPredicateVerb is the verb of Predicate method
	ExtenderPredicateVerb = "extender.predicateVerb"
	// ExtenderPrioritizeVerb is the verb of Prioritize method
	ExtenderPrioritizeVerb = "extender.prioritizeVerb"
	// ExtenderPreemptableVerb is the verb of Preemptable method
	ExtenderPreemptableVerb = "extender.preemptableVerb"
	// ExtenderReclaimableVerb is the verb of Reclaimable method
	ExtenderReclaimableVerb = "extender.reclaimableVerb"
	// ExtenderQueueOverusedVerb is the verb of QueueOverused method
	ExtenderQueueOverusedVerb = "extender.queueOverusedVerb"
	// ExtenderJobEnqueueableVerb is the verb of JobEnqueueable method
	ExtenderJobEnqueueableVerb = "extender.jobEnqueueableVerb"
	// ExtenderJobReadyVerb is the verb of JobReady method
	ExtenderJobReadyVerb = "extender.jobReadyVerb"
	// ExtenderIgnorable indicates whether the extender can ignore unexpected errors
	ExtenderIgnorable = "extender.ignorable"
	// ExtenderBindTaskVerb is the verb of Bind task method
	ExtenderBindTaskVerb = "extender.bindTaskVerb"
	// ExtenderSupportMultiMachine indicates whether the extender can support multi machine
	ExtenderSupportMultiMachine = "extender.supportMultiMachine"
	// ExtenderManagedResources is a list of extended resources that are managed by this extender
	ExtenderManagedResources = "extender.managedResources"
)

type extenderConfig struct {
	urlPrefix           string
	httpTimeout         time.Duration
	onSessionOpenVerb   string
	onSessionCloseVerb  string
	predicateVerb       string
	prioritizeVerb      string
	preemptableVerb     string
	reclaimableVerb     string
	queueOverusedVerb   string
	jobEnqueueableVerb  string
	jobReadyVerb        string
	ignorable           bool
	bindTaskVerb        string
	supportMultiMachine bool
	managedResources    []string
}

type extenderPlugin struct {
	client map[string]http.Client
	config map[string]extenderConfig
}

func parseExtenderConfig(arguments framework.Arguments) map[string]extenderConfig {
	/*
		actions: "reclaim, allocate, backfill, preempt"
		tiers:
		- plugins:
		  - name: priority
		  - name: gang
		  - name: conformance
		- plugins:
		  - name: drf
		  - name: predicates
		  - name: extender
			arguments:
			  extender:
			  - extender.urlPrefix: http://127.0.0.1
			    extender.httpTimeout: 100ms
			    extender.onSessionOpenVerb: onSessionOpen
			    extender.onSessionCloseVerb: onSessionClose
			    extender.predicateVerb: predicate
			    extender.prioritizeVerb: prioritize
			    extender.preemptableVerb: preemptable
			    extender.reclaimableVerb: reclaimable
			    extender.queueOverusedVerb: queueOverused
			    extender.jobEnqueueableVerb: jobEnqueueable
			    extender.ignorable: true
			    extender.bindTaskVerb: bindTask
			    extender.supportMultiMachine: true
			    extender.managedResources:
			    - aa.com/aa
			    - bb.com/bb
			  - extender.urlPrefix: http://mlushare-schd-extender.kube-system.svc:12345
			    extender.httpTimeout: 100ms
			    extender.predicateVerb: predicate
			    extender.bindTaskVerb: bindTask
			    extender.managedResources:
			   - cambricon.com/mlu-mem
		  - name: proportion
		  - name: nodeorder
	*/
	ecs := make(map[string]extenderConfig)
	for _, arg := range arguments[PluginName].([]interface{}) {
		argument := arg.(map[interface{}]interface{})
		urlPrefix, ok := argument[ExtenderURLPrefix].(string)
		if !ok {
			klog.Info("extender urlPrefix is empty")
			continue
		}
		if _, ok := ecs[urlPrefix]; ok {
			continue
		}
		ec := extenderConfig{}
		ec.urlPrefix, _ = argument[ExtenderURLPrefix].(string)
		ec.onSessionOpenVerb, _ = argument[ExtenderOnSessionOpenVerb].(string)
		ec.onSessionCloseVerb, _ = argument[ExtenderOnSessionCloseVerb].(string)
		ec.predicateVerb, _ = argument[ExtenderPredicateVerb].(string)
		ec.prioritizeVerb, _ = argument[ExtenderPrioritizeVerb].(string)
		ec.preemptableVerb, _ = argument[ExtenderPreemptableVerb].(string)
		ec.reclaimableVerb, _ = argument[ExtenderReclaimableVerb].(string)
		ec.queueOverusedVerb, _ = argument[ExtenderQueueOverusedVerb].(string)
		ec.jobEnqueueableVerb, _ = argument[ExtenderJobEnqueueableVerb].(string)
		ec.jobReadyVerb, _ = argument[ExtenderJobReadyVerb].(string)
		ec.bindTaskVerb, _ = argument[ExtenderBindTaskVerb].(string)
		managedResources, _ := argument[ExtenderManagedResources].([]interface{})
		for _, resource := range managedResources {
			ec.managedResources = append(ec.managedResources, resource.(string))
		}
		if ignorable, ok := argument[ExtenderIgnorable].(string); ok {
			arguments.GetBool(&ec.ignorable, ignorable)
		}
		if supportMultiMachine, ok := argument[ExtenderSupportMultiMachine].(string); ok {
			arguments.GetBool(&ec.supportMultiMachine, supportMultiMachine)
		}
		ec.httpTimeout = time.Second
		if httpTimeout, ok := argument[ExtenderHTTPTimeout].(string); ok && httpTimeout != "" {
			if timeoutDuration, err := time.ParseDuration(httpTimeout); err == nil {
				ec.httpTimeout = timeoutDuration
			}
		}
		ecs[urlPrefix] = ec
	}

	klog.V(4).Infof("parse extender config result: %v", ecs)
	return ecs
}

func New(arguments framework.Arguments) framework.Plugin {
	var ep extenderPlugin
	ep.client = make(map[string]http.Client)
	ep.config = make(map[string]extenderConfig)
	cfgs := parseExtenderConfig(arguments)
	for url, cfg := range cfgs {
		klog.V(4).Infof("Initialize extender plugin with resource %v endpoint address %s",
			cfg.managedResources, url)
		ep.client[url] = http.Client{Timeout: cfg.httpTimeout}
		ep.config[url] = cfg
	}
	return &ep
}

func (ep *extenderPlugin) Name() string {
	return PluginName
}

func (ep *extenderPlugin) OnSessionOpen(ssn *framework.Session) {
	for url, cfg := range ep.config {
		if cfg.onSessionOpenVerb == "" {
			continue
		}
		err := ep.send(cfg.onSessionOpenVerb, url, &OnSessionOpenRequest{
			Jobs:           ssn.Jobs,
			Nodes:          ssn.Nodes,
			Queues:         ssn.Queues,
			NamespaceInfo:  ssn.NamespaceInfo,
			RevocableNodes: ssn.RevocableNodes,
		}, nil)
		if err != nil {
			klog.Warningf("OnSessionOpen failed with url %s with error %v",
				url, err)
		}
	}

	ssn.AddPredicateFn(ep.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		for url, cfg := range ep.config {
			if cfg.predicateVerb == "" || !isInterested(task.Pod, cfg) {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(ssn.Jobs[task.Job], cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				return fmt.Errorf("plugin %s with url %s predicates failed for task %s on node %s of multi-machines",
					ep.Name(), url, task.Name, node.Name)
			}
			resp := &PredicateResponse{}
			err := ep.send(cfg.predicateVerb, url, &PredicateRequest{Task: task, Node: node}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s predicate failed for task %s on node %s with error %v",
					ep.Name(), url, task.Name, node.Name, err)
				return err
			}
			if resp.ErrorMessage != "" {
				klog.Warningf("plugin %s with url %s predicate failed for task %s on node %s with msg %s",
					ep.Name(), url, task.Name, node.Name, resp.ErrorMessage)
				return errors.New(resp.ErrorMessage)
			}
		}
		return nil
	})

	ssn.AddBatchNodeOrderFn(ep.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		nodeScore := map[string]float64{}
		for url, cfg := range ep.config {
			if cfg.prioritizeVerb == "" || !isInterested(task.Pod, cfg) {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(ssn.Jobs[task.Job], cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				return nil, fmt.Errorf("plugin %s prioritize with url %s failed for task %s of multi-machines",
					ep.Name(), url, task.Name)
			}
			resp := &PrioritizeResponse{}
			err := ep.send(cfg.prioritizeVerb, url, &PrioritizeRequest{Task: task, Nodes: nodes}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s prioritize failed for task %s with error %v",
					ep.Name(), url, task.Name, err)
				return nil, err
			}
			if resp.ErrorMessage != "" {
				klog.Warningf("plugin %s with url %s prioritize failed for task %s with msg %s",
					ep.Name(), url, task.Name, resp.ErrorMessage)
				return nil, errors.New(resp.ErrorMessage)
			}
			for n, v := range resp.NodeScore {
				if s, ok := nodeScore[n]; ok {
					nodeScore[n] = s + v
				} else {
					nodeScore[n] = v
				}
			}
		}
		return nodeScore, nil
	})

	ssn.AddPreemptableFn(ep.Name(), func(evictor *api.TaskInfo, evictees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		for url, cfg := range ep.config {
			if cfg.preemptableVerb == "" || !isInterested(evictor.Pod, cfg) {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(ssn.Jobs[evictor.Job], cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s preempt failed for task %s of multi-machines",
					ep.Name(), url, evictor.Name)
				return nil, util.Reject
			}
			resp := &PreemptableResponse{}
			err := ep.send(cfg.preemptableVerb, url, &PreemptableRequest{Evictor: evictor, Evictees: evictees}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s preempt failed for task %s with error %v",
					ep.Name(), url, evictor.Name, err)
				return nil, util.Reject
			}
			if resp.Status == util.Reject {
				return nil, util.Reject
			}
			evictees = resp.Victims
		}
		return evictees, util.Permit
	})

	ssn.AddReclaimableFn(ep.Name(), func(evictor *api.TaskInfo, evictees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		for url, cfg := range ep.config {
			if cfg.reclaimableVerb == "" || !isInterested(evictor.Pod, cfg) {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(ssn.Jobs[evictor.Job], cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s reclaim failed for task %s of multi-machines",
					ep.Name(), url, evictor.Name)
				return nil, util.Reject
			}
			resp := &ReclaimableResponse{}
			err := ep.send(cfg.reclaimableVerb, url, &ReclaimableRequest{Evictor: evictor, Evictees: evictees}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s reclaim failed for task %s with error %v",
					ep.Name(), url, evictor.Name, err)
				return nil, util.Reject
			}
			if resp.Status == util.Reject {
				return nil, util.Reject
			}
			evictees = resp.Victims
		}
		return evictees, util.Permit
	})

	ssn.AddJobEnqueueableFn(ep.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		for url, cfg := range ep.config {
			if cfg.jobEnqueueableVerb == "" {
				continue
			}
			var matchResource bool
			for _, task := range job.Tasks {
				if isInterested(task.Pod, cfg) {
					matchResource = true
					break
				}
			}
			if !matchResource {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(job, cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s jobEnqueue failed for job %s of multi-machines",
					ep.Name(), url, job.Name)
				return util.Reject

			}
			resp := &JobEnqueueableResponse{}
			err := ep.send(cfg.jobEnqueueableVerb, url, &JobEnqueueableRequest{Job: job}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s jobEnqueue failed for job %s with error %v",
					ep.Name(), url, job.Name, err)
				return util.Reject
			}
			if resp.Status == util.Reject {
				return util.Reject
			}
		}
		return util.Permit
	})

	ssn.AddOverusedFn(ep.Name(), func(obj interface{}) bool {
		for url, cfg := range ep.config {
			if cfg.queueOverusedVerb == "" {
				continue
			}
			queue := obj.(*api.QueueInfo)
			resp := &QueueOverusedResponse{}
			err := ep.send(cfg.queueOverusedVerb, url, &QueueOverusedRequest{Queue: queue}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s get queueOverused with error %v",
					ep.Name(), url, err)
				return true
			}
			if resp.Overused {
				return true
			}
		}
		return false
	})

	ssn.AddJobReadyFn(ep.Name(), func(obj interface{}) bool {
		job := obj.(*api.JobInfo)
		for url, cfg := range ep.config {
			if cfg.jobReadyVerb == "" {
				continue
			}
			var matchResource bool
			for _, task := range job.Tasks {
				if isInterested(task.Pod, cfg) {
					matchResource = true
					break
				}
			}
			if !matchResource {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(job, cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s jobReady failed for job %s of multi-machines",
					ep.Name(), url, job.Name)
				return false
			}
			resp := &JobReadyResponse{}
			err := ep.send(cfg.jobReadyVerb, url, &JobReadyRequest{Job: job}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s with url %s jobReady failed for job %s with error %v",
					ep.Name(), url, job.Name, err)
				return false
			}
			if !resp.Status {
				return false
			}
		}
		return true
	})

	ssn.AddBindTaskFn(ep.Name(), func(task *api.TaskInfo) error {
		for url, cfg := range ep.config {
			if cfg.bindTaskVerb == "" || !isInterested(task.Pod, cfg) {
				continue
			}
			if !cfg.supportMultiMachine && isMultiMachine(ssn.Jobs[task.Job], cfg.managedResources) {
				if cfg.ignorable {
					continue
				}
				return fmt.Errorf("plugin %s bind task failed for task %s on node %s because of multi-machines",
					ep.Name(), task.Name, task.NodeName)
			}
			resp := &BindTaskResponse{}
			err := ep.send(cfg.bindTaskVerb, url, &BindTaskRequest{Task: task}, resp)
			if err != nil {
				if cfg.ignorable {
					continue
				}
				klog.Warningf("plugin %s bind task failed for task %s on node %s with error %v",
					ep.Name(), task.Name, task.NodeName, err)
			}
			if resp.ErrorMessage != "" {
				klog.Warningf("plugin %s bind task failed for task %s on node %s with msg %s",
					ep.Name(), task.Name, task.NodeName, resp.ErrorMessage)
				return errors.New(resp.ErrorMessage)
			}
		}
		return nil
	})
}

func (ep *extenderPlugin) OnSessionClose(ssn *framework.Session) {
	for url, cfg := range ep.config {
		if cfg.onSessionCloseVerb != "" {
			if err := ep.send(cfg.onSessionCloseVerb, url, &OnSessionCloseRequest{}, nil); err != nil {
				klog.Warningf("OnSessionClose failed with url %s with error %v", url, err)
			}
		}
	}
}

func (ep *extenderPlugin) send(action, url string, args interface{}, result interface{}) error {
	out, err := json.Marshal(args)
	if err != nil {
		return err
	}

	fullURl := strings.TrimRight(url, "/") + "/" + action

	req, err := http.NewRequest("POST", fullURl, bytes.NewReader(out))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	cli := ep.client[url]
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed %v with extender at URL %v, code %v",
			action, url, resp.StatusCode)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func isMultiMachine(job *api.JobInfo, managedResources []string) bool {
	if job.MinAvailable <= 1 {
		return false
	}
	for _, min := range job.TaskMinAvailable {
		if min > 1 {
			return true
		}
	}
	if len(job.Tasks) <= 1 {
		return false
	}
	var taskUseResource int
	for _, task := range job.Tasks {
		if _, ok := api.MatchResource(task.Pod, managedResources); ok {
			taskUseResource++
		}
	}
	return taskUseResource > 1
}

// isInterested returns true if at least one extended resource requested by
// this pod is managed by this extender.
func isInterested(pod *v1.Pod, cfg extenderConfig) bool {
	if len(cfg.managedResources) == 0 {
		return true
	}
	if _, ok := api.MatchResource(pod, cfg.managedResources); ok {
		return true
	}
	return false
}
