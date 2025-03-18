/*
Copyright 2024 The Volcano Authors.

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

package uthelper

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/apis/pkg/apis/scheduling"
	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// RegisterPlugins plugins
func RegisterPlugins(plugins map[string]framework.PluginBuilder) {
	for name, plugin := range plugins {
		framework.RegisterPluginBuilder(name, plugin)
	}
}

// TestCommonStruct is the most common used resource when do UT
// others can wrap it in a new struct
type TestCommonStruct struct {
	// Name test case name
	Name string
	// Plugins plugins for each case
	Plugins map[string]framework.PluginBuilder
	// Resource objects that need to be added to schedulercache
	Pods           []*v1.Pod
	Nodes          []*v1.Node
	PodGroups      []*vcapisv1.PodGroup
	Queues         []*vcapisv1.Queue
	PriClass       []*schedulingv1.PriorityClass
	ResourceQuotas []*v1.ResourceQuota

	// ExpectBindMap the expected bind results.
	// bind results: ns/podName -> nodeName
	ExpectBindMap map[string]string
	// ExpectPipeLined the expected pipelined results.
	// pipelined results: map[jobID][]{nodeName}
	ExpectPipeLined map[string][]string
	// ExpectEvicted the expected evicted results.
	// evicted pods list of ns/podName
	ExpectEvicted []string
	// ExpectStatus the expected final podgroup status.
	ExpectStatus map[api.JobID]scheduling.PodGroupPhase
	// ExpectBindsNum the expected bind events numbers.
	ExpectBindsNum int
	// ExpectEvictNum the expected evict events numbers, include preempted and reclaimed evict events
	ExpectEvictNum int

	// fake interface instance when check results need
	stop       chan struct{}
	binder     cache.Binder
	evictor    cache.Evictor
	stsUpdator cache.StatusUpdater
	volBinder  cache.VolumeBinder
	ssn        *framework.Session // store opened session
}

var _ Interface = &TestCommonStruct{}

// RegisterSession open session with tiers and configuration, and mock schedulerCache with self-defined FakeBinder and FakeEvictor
func (test *TestCommonStruct) RegisterSession(tiers []conf.Tier, config []conf.Configuration) *framework.Session {
	schedulerCache := test.createSchedulerCache()
	RegisterPlugins(test.Plugins)
	test.ssn = framework.OpenSession(schedulerCache, tiers, config)
	schedulerCache.Run(test.stop)
	return test.ssn
}

// createSchedulerCache create scheduler cache
func (test *TestCommonStruct) createSchedulerCache() *cache.SchedulerCache {
	binder := util.NewFakeBinder(0)
	evictor := util.NewFakeEvictor(0)
	stsUpdator := &util.FakeStatusUpdater{}
	test.binder = binder
	test.evictor = evictor
	test.stop = make(chan struct{})
	// Create scheduler cache with self-defined binder and evictor
	schedulerCache := cache.NewCustomMockSchedulerCache("utmock-scheduler", binder, evictor, stsUpdator, nil, nil, nil)
	test.stsUpdator = schedulerCache.StatusUpdater
	test.volBinder = schedulerCache.VolumeBinder

	for _, node := range test.Nodes {
		schedulerCache.AddOrUpdateNode(node)
	}
	for _, pod := range test.Pods {
		schedulerCache.AddPod(pod)
	}
	for _, pg := range test.PodGroups {
		schedulerCache.AddPodGroupV1beta1(pg)
	}
	for _, queue := range test.Queues {
		schedulerCache.AddQueueV1beta1(queue)
	}
	for _, pc := range test.PriClass {
		schedulerCache.AddPriorityClass(pc)
	}
	for _, rq := range test.ResourceQuotas {
		schedulerCache.AddResourceQuota(rq)
	}

	return schedulerCache
}

// Run choose to run passed in actions; if no actions provided, will panic
func (test *TestCommonStruct) Run(actions []framework.Action) {
	if len(actions) == 0 {
		panic("no actions provided, please specify a list of actions to execute")
	}

	// registry actions in conf variables
	conf.EnabledActionMap = make(map[string]bool, len(actions))
	for _, action := range actions {
		conf.EnabledActionMap[action.Name()] = true
	}

	for _, action := range actions {
		action.Initialize()
		action.Execute(test.ssn)
		action.UnInitialize()
	}
}

// Close do release resource and clean up
func (test *TestCommonStruct) Close() {
	framework.CloseSession(test.ssn)
	framework.CleanupPluginBuilders()
	close(test.stop)
}

// CheckAll checks all the need status
func (test *TestCommonStruct) CheckAll(caseIndex int) (err error) {
	if err = test.CheckBind(caseIndex); err != nil {
		return
	}
	if err = test.CheckEvict(caseIndex); err != nil {
		return
	}
	if err = test.CheckPipelined(caseIndex); err != nil {
		return
	}
	return test.CheckPGStatus(caseIndex)
}

// CheckBind check expected bind result
func (test *TestCommonStruct) CheckBind(caseIndex int) error {
	if test.ExpectBindsNum != len(test.ExpectBindMap) {
		return fmt.Errorf("invalid setting for binding check: want bind count %d, want bind result length %d", test.ExpectBindsNum, len(test.ExpectBindMap))
	}
	binder := test.binder.(*util.FakeBinder)
	for i := 0; i < test.ExpectBindsNum; i++ {
		select {
		case <-binder.Channel:
		case <-time.After(300 * time.Millisecond):
			return fmt.Errorf("failed to get Bind request in case %d(%s)", caseIndex, test.Name)
		}
	}

	// in case expected test.BindsNum is 0, but actually there is a binding and wait the binding goroutine to run
	select {
	case <-time.After(50 * time.Millisecond):
	case key := <-binder.Channel:
		return fmt.Errorf("unexpect binding %s in case %d(%s)", key, caseIndex, test.Name)
	}

	binds := binder.Binds()
	if len(test.ExpectBindMap) != len(binds) {
		return fmt.Errorf("case %d(%s) check bind: \nwant: %v\n got %v ", caseIndex, test.Name, test.ExpectBindMap, binds)
	}
	for key, value := range test.ExpectBindMap {
		got := binds[key]
		if value != got {
			return fmt.Errorf("case %d(%s)  check bind: \nwant: %v->%v\n got: %v->%v ", caseIndex, test.Name, key, value, key, got)
		}
	}
	return nil
}

// CheckEvict check the evicted result
func (test *TestCommonStruct) CheckEvict(caseIndex int) error {
	if test.ExpectEvictNum != len(test.ExpectEvicted) {
		return fmt.Errorf("invalid setting for evicting check: want evict count %d, want evict result length %d", test.ExpectEvictNum, len(test.ExpectEvicted))
	}
	evictor := test.evictor.(*util.FakeEvictor)
	for i := 0; i < test.ExpectEvictNum; i++ {
		select {
		case <-evictor.Channel:
		case <-time.After(300 * time.Millisecond):
			return fmt.Errorf("failed to get Evict request in case %d(%s)", caseIndex, test.Name)
		}
	}

	// in case expected test.EvictNum is 0, but actually there is an evicting and wait the evicting goroutine to run
	select {
	case <-time.After(50 * time.Millisecond):
	case key := <-evictor.Channel:
		return fmt.Errorf("unexpect evicted %s in case %d(%s)", key, caseIndex, test.Name)
	}

	evicts := evictor.Evicts()
	if len(test.ExpectEvicted) != len(evicts) {
		return fmt.Errorf("case %d(%s) check evict: \nwant: %v\n got %v ", caseIndex, test.Name, test.ExpectEvicted, evicts)
	}

	expect := map[string]int{} // evicted number
	got := map[string]int{}
	for _, v := range test.ExpectEvicted {
		expect[v]++
	}
	for _, v := range evicts {
		got[v]++
	}

	if !equality.Semantic.DeepEqual(expect, got) {
		return fmt.Errorf("case %d(%s) check evict: \nwant: %v\n got: %v ", caseIndex, test.Name, expect, got)
	}
	return nil
}

// CheckPGStatus check job's podgroups status
func (test *TestCommonStruct) CheckPGStatus(caseIndex int) error {
	ssn := test.ssn
	for jobID, phase := range test.ExpectStatus {
		job := ssn.Jobs[jobID]
		if job == nil {
			return fmt.Errorf("case %d(%s) check podgroup status, job <%v> doesn't exist in session", caseIndex, test.Name, jobID)
		}
		got := job.PodGroup.Status.Phase
		if phase != got {
			return fmt.Errorf("case %d(%s) check podgroup <%v> status:\n want: %v, got: %v", caseIndex, test.Name, jobID, phase, got)
		}
	}
	return nil
}

// CheckPipelined checks pipeline results
func (test *TestCommonStruct) CheckPipelined(caseIndex int) error {
	ssn := test.ssn
	for jobID, nodes := range test.ExpectPipeLined {
		job := ssn.Jobs[api.JobID(jobID)]
		if job == nil {
			return fmt.Errorf("case %d(%s) check pipeline, job <%v> doesn't exist in session", caseIndex, test.Name, jobID)
		}
		pipeLined := job.TaskStatusIndex[api.Pipelined]
		if len(pipeLined) == 0 {
			return fmt.Errorf("case %d(%s) check pipeline, want pipelined job: %v, actually, no tasks pipelined to nodes %v", caseIndex, test.Name, jobID, nodes)
		}
		for _, task := range pipeLined {
			if !Contains(nodes, task.NodeName) {
				return fmt.Errorf("case %d(%s) check pipeline: actual: %v->%v, want: %v->%v", caseIndex, test.Name, task.Name, task.NodeName, task.Name, nodes)
			}
		}
	}
	return nil
}
