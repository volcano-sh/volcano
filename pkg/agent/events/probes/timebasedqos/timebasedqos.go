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

package timebasedqos

import (
	"reflect"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

const (
	probeName = "TimeBasedQoSProbe"
)

func init() {
	probes.RegisterEventProbeFunc(string(framework.TimeBaseQosEventName), NewTimeBasedQoSProbe)
}

type timeBasedQoSProbe struct {
	policyWorkers map[string]*policyWorker
	queue         workqueue.RateLimitingInterface
}

func NewTimeBasedQoSProbe(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, queue workqueue.RateLimitingInterface) framework.Probe {
	return &timeBasedQoSProbe{
		queue:         queue,
		policyWorkers: make(map[string]*policyWorker),
	}
}

func (t *timeBasedQoSProbe) ProbeName() string {
	return probeName
}

func (t *timeBasedQoSProbe) Run(stop <-chan struct{}) {
	klog.InfoS("TimeBasedQos probe is running")
	<-stop
	t.stopAllWorkers()
	klog.InfoS("TimeBasedQos probe stopped")
}

func (t *timeBasedQoSProbe) RefreshCfg(cfg *api.ColocationConfig) error {
	if cfg == nil || cfg.TimeBasedQoSPolicies == nil {
		return nil
	}

	activePolicies := make(map[string]*api.TimeBasedQoSPolicy)
	for _, p := range cfg.TimeBasedQoSPolicies {
		if *p.Enable {
			activePolicies[p.Name] = p
		}
	}

	// 1. Stop or restart existing workers if policy has changed
	for policyName, worker := range t.policyWorkers {
		activePolicy, exists := activePolicies[policyName]
		if !exists || !reflect.DeepEqual(worker.policy, activePolicy) {
			klog.InfoS("TimeBasedQoS policy updated", "policy", activePolicy.Name, "details", activePolicy)
			worker.stop()
			delete(t.policyWorkers, policyName)
		}
	}

	// 2. Start new workers for new policies or restart workers if the policy has changed
	for policyName, policy := range activePolicies {
		if _, exists := t.policyWorkers[policyName]; !exists {
			klog.InfoS("Starting new/restarted worker for policy", "policy", policy.Name)
			worker := newPolicyWorker(policy, t.queue)
			t.policyWorkers[policyName] = worker
			go worker.run()
		}
	}

	return nil
}

func (t *timeBasedQoSProbe) stopAllWorkers() {
	for name, worker := range t.policyWorkers {
		worker.stop()
		delete(t.policyWorkers, name)
	}
	klog.InfoS("All time based qos policy workers stopped")
}
