/*
Copyright 2018 The Kubernetes Authors.

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
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/contrib/DLaaS/pkg/scheduler/framework"
)

var defaultSchedulerConf = map[string]string{
	"actions":                   "decorate, allocate, preempt",
	"plugins":                   "gang, priority, drf",
	"plugin.gang.jobready":      "true",
	"plugin.gang.joborder":      "true",
	"plugin.gang.preemptable":   "true",
	"plugin.priority.joborder":  "true",
	"plugin.priority.taskorder": "true",
	"plugin.drf.preemptable":    "true",
	"plugin.drf.joborder":       "true",
}

func loadSchedulerConf(conf map[string]string) ([]framework.Action, []*framework.PluginArgs) {
	var actions []framework.Action
	var pluginArgs []*framework.PluginArgs

	actionsConf, found := conf["actions"]
	if !found {
		actionsConf = "decorate, allocate, preempt"
	}

	actionNames := strings.Split(actionsConf, ",")
	for _, actionName := range actionNames {
		if action, found := framework.GetAction(strings.TrimSpace(actionName)); found {
			actions = append(actions, action)
		} else {
			glog.Errorf("Failed to found Action %s, ignore it.", actionName)
		}
	}

	pluginsConf, found := conf["plugins"]
	if !found {
		pluginsConf = "gang, priority, drf"
	}

	pluginNames := strings.Split(pluginsConf, ",")
	for _, pluginName := range pluginNames {
		pName := strings.TrimSpace(pluginName)
		pluginArgs = append(pluginArgs, newPluginArgs(pName, conf))
	}

	return actions, pluginArgs
}

func newPluginArgs(name string, conf map[string]string) *framework.PluginArgs {
	args := &framework.PluginArgs{
		Name: name,
	}

	key := fmt.Sprintf("plugin.%s.preemptable", name)
	if preemptable, found := conf[key]; found {
		if enable, err := strconv.ParseBool(preemptable); err != nil {
			glog.Error("Failed to parse '%s' value '%s', ignore it.", key, preemptable)
		} else {
			args.PreemptableFnEnabled = enable
		}
	}

	key = fmt.Sprintf("plugin.%s.joborder", name)
	if joborder, found := conf[key]; found {
		if enable, err := strconv.ParseBool(joborder); err != nil {
			glog.Error("Failed to parse '%s' value '%s', ignore it.", key, joborder)
		} else {
			args.JobOrderFnEnabled = enable
		}
	}

	key = fmt.Sprintf("plugin.%s.taskorder", name)
	if taskorder, found := conf[key]; found {
		if enable, err := strconv.ParseBool(taskorder); err != nil {
			glog.Error("Failed to parse '%s' value '%s', ignore it.", key, taskorder)
		} else {
			args.TaskOrderFnEnabled = enable
		}
	}

	key = fmt.Sprintf("plugin.%s.jobready", name)
	if jobready, found := conf[key]; found {
		if enable, err := strconv.ParseBool(jobready); err != nil {
			glog.Error("Failed to parse '%s' value '%s', ignore it.", key, jobready)
		} else {
			args.JobReadyFnEnabled = enable
		}
	}

	return args
}
