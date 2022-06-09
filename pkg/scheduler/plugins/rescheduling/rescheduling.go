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

	"github.com/mitchellh/mapstructure"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
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

	// RegisteredStrategyConfigs collects all the strategy configurations registered.
	RegisteredStrategyConfigs map[string]interface{}

	// VictimFn contains all the VictimTasksFn for registered the strategies
	VictimFn map[string]api.VictimTasksFn

	// Interval indicates the interval to get metrics, "5m" by default.
	Interval string
)

func init() {
	RegisteredStrategyConfigs = make(map[string]interface{})
	VictimFn = make(map[string]api.VictimTasksFn)
	Interval = "5m"

	// register victim functions for all strategies here
	VictimFn["lowNodeUtilization"] = victimsFnForLnu
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
	klog.V(3).Infof("Enter rescheduling plugin ...")
	defer klog.V(3).Infof("Leaving rescheduling plugin.")

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

	if !timeToRun(configs.interval) {
		klog.V(3).Infof("It is not the time to execute rescheduling strategies.")
		return
	}

	// Get all strategies and register the victim functions
	victimFns := make([]api.VictimTasksFn, 0)
	for _, strategy := range configs.strategies {
		klog.V(4).Infof("strategy: %s\n", strategy.Name)
		victimFns = append(victimFns, VictimFn[strategy.Name])
	}
	ssn.AddVictimTasksFns(rp.Name(), victimFns)
}

func (rp *reschedulingPlugin) OnSessionClose(ssn *framework.Session) {
	Session = nil
	for k := range RegisteredStrategyConfigs {
		delete(RegisteredStrategyConfigs, k)
	}
}

// Configs is the struct for rescheduling plugin arguments
type Configs struct {
	interval   time.Duration
	strategies []Strategy
}

// Strategy is the struct for rescheduling strategy
type Strategy struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

// NewReschedulingConfigs creates an object of rescheduling configurations with default configuration
func NewReschedulingConfigs() *Configs {
	config := &Configs{
		interval: DefaultInterval,
		strategies: []Strategy{
			{
				Name:   DefaultStrategy,
				Params: DefaultLowNodeConf,
			},
		},
	}
	RegisteredStrategyConfigs[DefaultStrategy] = DefaultLowNodeConf
	return config
}

// parseArguments parse all the rescheduling arguments
func (rc *Configs) parseArguments(arguments framework.Arguments) {
	var intervalStr string
	var err error
	if intervalArg, ok := arguments["interval"]; ok {
		intervalStr = intervalArg.(string)
	}
	rc.interval, err = time.ParseDuration(intervalStr)
	if err != nil {
		klog.V(4).Infof("Parse rescheduling interval failed. Reset the interval to 5m by default.")
		rc.interval = DefaultInterval
	} else {
		Interval = intervalStr
	}
	strategies, ok := arguments["strategies"]
	if ok {
		strategyArray, _ := strategies.([]interface{})
		if len(strategyArray) != 0 {
			rc.strategies = rc.strategies[0:0]
		}
		for _, strategyInterface := range strategyArray {
			strategy := new(Strategy)
			err := mapstructure.Decode(strategyInterface, strategy)
			if err != nil {
				klog.V(3).Infof("Decode error: %s\n", err.Error())
			} else {
				rc.strategies = append(rc.strategies, *strategy)
			}
		}
		for k := range RegisteredStrategyConfigs {
			delete(RegisteredStrategyConfigs, k)
		}
		for _, strategy := range rc.strategies {
			RegisteredStrategyConfigs[strategy.Name] = strategy.Params
		}
		klog.V(3).Infof("RegisteredStrategyConfigs: %v\n", RegisteredStrategyConfigs)
	}
	for _, strategy := range rc.strategies {
		klog.V(3).Infof("strategy: %s, params: %v\n", strategy.Name, strategy.Params)
	}
}
