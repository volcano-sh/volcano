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

package rescheduling

import (
	"time"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin
	PluginName = "rescheduling"
	// DefaultInterval indicates the default interval rescheduling plugin works for
	DefaultInterval = 5 * time.Minute
	// DefaultStrategy indicates the default strategy rescheduling plugin making use of
	DefaultStrategy = "lowNodeUtilization"
)

var (
	// Session contains all the data in session object which will be used for all the rescheduling package
	Session *framework.Session

	// RegisteredStrategies collects all the strategies registered.
	RegisteredStrategies []string

	// RegisteredStrategyConfigs collects all the strategy configurations registered.
	RegisteredStrategyConfigs map[string]interface{}

	// VictimFns contains all the victimFns for registered the strategies
	VictimFns map[string]api.VictimTasksFromCandidatesFn
)

func init() {
	RegisteredStrategies = make([]string, 0)
	RegisteredStrategyConfigs = make(map[string]interface{})
	VictimFns = make(map[string]api.VictimTasksFromCandidatesFn)

	// register victim functions for all strategies here
	VictimFns["lowNodeUtilization"] = victimsFnForLnu
}

type reschedulingPlugin struct {
	// Arguments given for rescheduling plugin
	pluginArguments framework.Arguments
}

// New function returns rescheduling plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &reschedulingPlugin{
		pluginArguments: arguments,
	}
}

// Name returns the name of rescheduling plugin
func (rp *reschedulingPlugin) Name() string {
	return PluginName
}

func (rp *reschedulingPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter rescheduling plugin ...")
	defer klog.V(4).Infof("Leaving rescheduling plugin.")

	// Parse all the rescheduling strategies and execution interval
	Session = ssn
	configs := NewReschedulingConfigs()
	for _, tier := range ssn.Tiers {
		for _, pluginOption := range tier.Plugins {
			if pluginOption.Name == PluginName {
				configs.parseArguments(pluginOption.Arguments)
				break
			}
		}
	}
	klog.V(4).Infof("rescheduling config: %v", configs)

	// Judge whether it is time to execute rescheduling now
	if !util.IsToBeExecuted("reschedulingFns", configs.interval) {
		klog.V(4).Infof("It is not the time to execute rescheduling strategies.")
		return
	}

	// Get all strategies and register the VictimTasksFromCandidatesFns
	victimFns := make([]api.VictimTasksFromCandidatesFn, 0)
	for _, strategy := range configs.strategies {
		victimFns = append(victimFns, VictimFns[strategy.Name])
	}
	ssn.AddVictimTasksFromCandidatesFns(rp.Name(), victimFns)
}

func (rp *reschedulingPlugin) OnSessionClose(ssn *framework.Session) {
	Session = nil
	RegisteredStrategies = RegisteredStrategies[0:0]
	for k := range RegisteredStrategyConfigs {
		delete(RegisteredStrategyConfigs, k)
	}
	VictimFns = nil
}

// ReschedulingConfigs is the struct for rescheduling plugin arguments
type ReschedulingConfigs struct {
	interval   time.Duration
	strategies []Strategy
}

// Strategy is the struct for rescheduling strategy
type Strategy struct {
	Name       string
	Parameters map[string]interface{}
}

// NewReschedulingConfigs creates an object of rescheduling configurations with default configuration
func NewReschedulingConfigs() *ReschedulingConfigs {
	config := &ReschedulingConfigs{
		interval: DefaultInterval,
		strategies: []Strategy{
			{
				Name:       DefaultStrategy,
				Parameters: DefaultLowNodeConf,
			},
		},
	}
	RegisteredStrategies = append(RegisteredStrategies, DefaultStrategy)
	RegisteredStrategyConfigs[DefaultStrategy] = DefaultLowNodeConf
	return config
}

// parseArguments parse all the rescheduling arguments
func (rc *ReschedulingConfigs) parseArguments(arguments framework.Arguments) {
	var intervalStr string
	var err error
	if intervalArg, ok := arguments["interval"]; ok {
		intervalStr = intervalArg.(string)
	}
	rc.interval, err = time.ParseDuration(intervalStr)
	if err != nil {
		klog.V(4).Infof("Parse rescheduling interval failed. Reset the interval to 5m by default.")
		rc.interval = DefaultInterval
	}
	strategies, ok := arguments["strategies"]
	if ok {
		rc.strategies = strategies.([]Strategy)
		RegisteredStrategies = RegisteredStrategies[0:0]
		for k := range RegisteredStrategyConfigs {
			delete(RegisteredStrategyConfigs, k)
		}
		for _, strategy := range rc.strategies {
			RegisteredStrategies = append(RegisteredStrategies, strategy.Name)
			RegisteredStrategyConfigs[strategy.Name] = strategy.Parameters
		}
	}
}
