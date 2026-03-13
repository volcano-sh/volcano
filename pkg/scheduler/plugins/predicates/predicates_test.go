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

package predicates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8sframework "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodevolumelimits"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumezone"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/backfill"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/api"
	vbcap "volcano.sh/volcano/pkg/scheduler/capabilities/volumebinding"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func getWorkerAffinity() *apiv1.Affinity {
	return &apiv1.Affinity{
		PodAntiAffinity: &apiv1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []apiv1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "role",
								Operator: "In",
								Values:   []string{"worker"},
							},
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

func TestEventHandler(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:          New,
		gang.PluginName:     gang.New,
		priority.PluginName: priority.New,
	}

	// pending pods
	w1 := util.BuildPod("ns1", "worker-1", "", apiv1.PodPending, api.BuildResourceList("3", "3k"), "pg1", map[string]string{"role": "worker"}, map[string]string{"selector": "worker"})
	w2 := util.BuildPod("ns1", "worker-2", "", apiv1.PodPending, api.BuildResourceList("5", "5k"), "pg1", map[string]string{"role": "worker"}, map[string]string{})
	w3 := util.BuildPod("ns1", "worker-3", "", apiv1.PodPending, api.BuildResourceList("4", "4k"), "pg2", map[string]string{"role": "worker"}, map[string]string{})
	w1.Spec.Affinity = getWorkerAffinity()
	w2.Spec.Affinity = getWorkerAffinity()
	w3.Spec.Affinity = getWorkerAffinity()

	// nodes
	n1 := util.BuildNode("node1", api.BuildResourceList("14", "14k", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"selector": "worker"})
	n2 := util.BuildNode("node2", api.BuildResourceList("3", "3k", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	n1.Labels["kubernetes.io/hostname"] = "node1"
	n2.Labels["kubernetes.io/hostname"] = "node2"

	// priority
	p1 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Value: 1}
	p2 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p2"}, Value: 2}
	// podgroup
	pg1 := util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, p2.Name)
	pg2 := util.BuildPodGroupWithPrio("pg2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, p1.Name)

	// queue
	queue1 := util.BuildQueue("q1", 0, nil)

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:      "pod-deallocate",
			Plugins:   plugins,
			Pods:      []*apiv1.Pod{w1, w2, w3},
			Nodes:     []*apiv1.Node{n1, n2},
			PriClass:  []*schedulingv1.PriorityClass{p1, p2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:    []*schedulingv1beta1.Queue{queue1},
			ExpectBindMap: map[string]string{ // podKey -> node
				"ns1/worker-3": "node1",
			},
			ExpectBindsNum: 1,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
					{
						Name:                gang.PluginName,
						EnabledJobReady:     &trueValue,
						EnabledJobPipelined: &trueValue,
					},
					{
						Name:            priority.PluginName,
						EnabledJobOrder: &trueValue,
					},
				},
			},
		}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNodeNum(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	// pending pods
	w1 := util.BuildPod("ns1", "worker-1", "", apiv1.PodPending, nil, "pg1", map[string]string{"role": "worker"}, map[string]string{"selector": "worker"})
	w2 := util.BuildPod("ns1", "worker-2", "", apiv1.PodPending, nil, "pg1", map[string]string{"role": "worker"}, map[string]string{})
	w3 := util.BuildPod("ns1", "worker-3", "", apiv1.PodPending, nil, "pg2", map[string]string{"role": "worker"}, map[string]string{})

	// nodes
	n1 := util.BuildNode("node1", api.BuildResourceList("4", "4k", []api.ScalarResource{{Name: "pods", Value: "2"}}...), map[string]string{"selector": "worker"})

	// priority
	p1 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Value: 1}
	p2 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p2"}, Value: 2}

	// podgroup
	pg1 := util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, p2.Name)
	pg2 := util.BuildPodGroupWithPrio("pg2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, p1.Name)

	// queue
	queue1 := util.BuildQueue("q1", 0, nil)

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:      "pod-predicate",
			Plugins:   plugins,
			Pods:      []*apiv1.Pod{w1, w2, w3},
			Nodes:     []*apiv1.Node{n1},
			PriClass:  []*schedulingv1.PriorityClass{p1, p2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:    []*schedulingv1beta1.Queue{queue1},
			ExpectBindMap: map[string]string{ // podKey -> node
				"ns1/worker-1": "node1",
				"ns1/worker-2": "node1",
			},
			ExpectBindsNum: 2,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New(), backfill.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
				},
			},
		}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPodAntiAffinity(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:          New,
		priority.PluginName: priority.New,
	}
	highPrio := util.MakePriorityClass().Name("high-priority").SetValue(100000).Obj()
	lowPrio := util.MakePriorityClass().Name("low-priority").SetValue(10).Obj()

	w1 := util.BuildPodWithPriority("ns1", "worker-1", "n1", apiv1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{"role": "worker"}, map[string]string{}, &highPrio.Value)
	w2 := util.BuildPodWithPriority("ns1", "worker-2", "n1", apiv1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{}, map[string]string{}, &lowPrio.Value)
	w3 := util.BuildPodWithPriority("ns1", "worker-3", "", apiv1.PodPending, api.BuildResourceList("3", "3G"), "pg2", map[string]string{"role": "worker"}, map[string]string{}, &highPrio.Value)
	w1.Spec.Affinity = getWorkerAffinity()
	w3.Spec.Affinity = getWorkerAffinity()

	// nodes
	n1 := util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "2"}}...), map[string]string{})
	n1.Labels["kubernetes.io/hostname"] = "node1"

	// podgroup
	pg1 := util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 0, nil, schedulingv1beta1.PodGroupRunning, lowPrio.Name)
	pg2 := util.BuildPodGroupWithPrio("pg2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, highPrio.Name)

	// queue
	queue1 := util.BuildQueue("q1", 0, api.BuildResourceList("9", "9G"))

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:            "pod-anti-affinity",
			Plugins:         plugins,
			Pods:            []*apiv1.Pod{w1, w2, w3},
			Nodes:           []*apiv1.Node{n1},
			PriClass:        []*schedulingv1.PriorityClass{lowPrio, highPrio},
			PodGroups:       []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:          []*schedulingv1beta1.Queue{queue1},
			ExpectPipeLined: map[string][]string{},
			ExpectEvicted:   []string{},
			ExpectEvictNum:  0,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New(), preempt.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
					{
						Name:               priority.PluginName,
						EnabledPreemptable: &trueValue,
						EnabledJobStarving: &trueValue,
					},
				},
			},
		}
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[1].Name(),
				Arguments: map[string]interface{}{preempt.EnableTopologyAwarePreemptionKey: true}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSetUpDynamicResourcesArgs_Default(t *testing.T) {
	dra := defaultDynamicResourcesArgs()
	setUpDynamicResourcesArgs(dra, nil)

	assert.Equal(t, &metav1.Duration{Duration: defaultDRAFilterTimeout}, dra.FilterTimeout)
}

func TestSetUpDynamicResourcesArgs_OverideSeconds(t *testing.T) {
	tests := []struct {
		name        string
		rawArgs     framework.Arguments
		expectedDur time.Duration
	}{
		{
			name:        "override with seconds (int)",
			rawArgs:     framework.Arguments{draFilterTimeoutSecondsKey: 3},
			expectedDur: 3 * time.Second,
		},
		{
			name:        "ignore negative seconds",
			rawArgs:     framework.Arguments{draFilterTimeoutSecondsKey: -5},
			expectedDur: defaultDRAFilterTimeout,
		},
		{
			name:        "no key keeps default",
			rawArgs:     framework.Arguments{},
			expectedDur: defaultDRAFilterTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dra := defaultDynamicResourcesArgs()
			setUpDynamicResourcesArgs(dra, tt.rawArgs)
			assert.Equal(t, &metav1.Duration{Duration: tt.expectedDur}, dra.FilterTimeout)
		})
	}
}

func TestInitPlugin(t *testing.T) {
	tests := []struct {
		name                    string
		enableNodeAffinity      bool
		enableNodePorts         bool
		enableTaintToleration   bool
		enablePodAffinity       bool
		enableNodeVolumeLimits  bool
		enableVolumeZone        bool
		enablePodTopologySpread bool
		enableVolumeBinding     bool
		enableDRA               bool
		expectInFilter          []string
		expectInStableFilter    []string
		expectInPrefilter       []string
		expectInReserve         []string
		expectInPreBind         []string
		expectInScore           []string
		expectNotInFilter       []string
		expectNotInReserve      []string
		expectNotInPreBind      []string
		expectNotInScore        []string
	}{
		{
			name:                    "all default plugins enabled without volume binding and dra",
			enableNodeAffinity:      true,
			enableNodePorts:         true,
			enableTaintToleration:   true,
			enablePodAffinity:       true,
			enableNodeVolumeLimits:  true,
			enableVolumeZone:        true,
			enablePodTopologySpread: true,
			enableVolumeBinding:     false,
			enableDRA:               false,
			expectInFilter:          []string{nodeunschedulable.Name, nodeaffinity.Name, nodeports.Name, tainttoleration.Name, interpodaffinity.Name, nodevolumelimits.CSIName, volumezone.Name, podtopologyspread.Name},
			expectInStableFilter:    []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name},
			expectInPrefilter:       []string{nodeports.Name, interpodaffinity.Name, podtopologyspread.Name},
			expectInReserve:         []string{},
			expectInPreBind:         []string{},
			expectInScore:           []string{},
			expectNotInFilter:       []string{vbcap.Name, dynamicresources.Name},
			expectNotInReserve:      []string{vbcap.Name, dynamicresources.Name},
			expectNotInPreBind:      []string{vbcap.Name, dynamicresources.Name},
			expectNotInScore:        []string{vbcap.Name},
		},
		{
			name:                    "volume binding enabled",
			enableNodeAffinity:      true,
			enableNodePorts:         false,
			enableTaintToleration:   true,
			enablePodAffinity:       false,
			enableNodeVolumeLimits:  false,
			enableVolumeZone:        false,
			enablePodTopologySpread: false,
			enableVolumeBinding:     true,
			enableDRA:               false,
			expectInFilter:          []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name, vbcap.Name},
			expectInStableFilter:    []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name},
			expectInPrefilter:       []string{vbcap.Name},
			expectInReserve:         []string{vbcap.Name},
			expectInPreBind:         []string{vbcap.Name},
			expectInScore:           []string{vbcap.Name},
			expectNotInFilter:       []string{nodeports.Name, interpodaffinity.Name, dynamicresources.Name},
			expectNotInReserve:      []string{dynamicresources.Name},
			expectNotInPreBind:      []string{dynamicresources.Name},
		},
		{
			name:                    "dra enabled",
			enableNodeAffinity:      true,
			enableNodePorts:         false,
			enableTaintToleration:   true,
			enablePodAffinity:       false,
			enableNodeVolumeLimits:  false,
			enableVolumeZone:        false,
			enablePodTopologySpread: false,
			enableVolumeBinding:     false,
			enableDRA:               true,
			expectInFilter:          []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name, dynamicresources.Name},
			expectInStableFilter:    []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name},
			expectInPrefilter:       []string{dynamicresources.Name},
			expectInReserve:         []string{dynamicresources.Name},
			expectInPreBind:         []string{dynamicresources.Name},
			expectInScore:           []string{},
			expectNotInFilter:       []string{vbcap.Name},
			expectNotInReserve:      []string{vbcap.Name},
			expectNotInPreBind:      []string{vbcap.Name},
			expectNotInScore:        []string{dynamicresources.Name, vbcap.Name},
		},
		{
			name:                    "both volume binding and dra enabled",
			enableNodeAffinity:      true,
			enableNodePorts:         true,
			enableTaintToleration:   true,
			enablePodAffinity:       true,
			enableNodeVolumeLimits:  true,
			enableVolumeZone:        true,
			enablePodTopologySpread: true,
			enableVolumeBinding:     true,
			enableDRA:               true,
			expectInFilter:          []string{nodeunschedulable.Name, nodeaffinity.Name, nodeports.Name, tainttoleration.Name, interpodaffinity.Name, nodevolumelimits.CSIName, volumezone.Name, podtopologyspread.Name, vbcap.Name, dynamicresources.Name},
			expectInStableFilter:    []string{nodeunschedulable.Name, nodeaffinity.Name, tainttoleration.Name},
			expectInPrefilter:       []string{nodeports.Name, interpodaffinity.Name, podtopologyspread.Name, vbcap.Name, dynamicresources.Name},
			expectInReserve:         []string{vbcap.Name, dynamicresources.Name},
			expectInPreBind:         []string{vbcap.Name, dynamicresources.Name},
			expectInScore:           []string{vbcap.Name},
			expectNotInScore:        []string{dynamicresources.Name},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset volume binding plugin for test isolation
			ResetVolumeBindingPluginForTest()

			pp := New(nil).(*PredicatesPlugin)
			pp.enabledPredicates.nodeAffinityEnable = tt.enableNodeAffinity
			pp.enabledPredicates.nodePortEnable = tt.enableNodePorts
			pp.enabledPredicates.taintTolerationEnable = tt.enableTaintToleration
			pp.enabledPredicates.podAffinityEnable = tt.enablePodAffinity
			pp.enabledPredicates.nodeVolumeLimitsEnable = tt.enableNodeVolumeLimits
			pp.enabledPredicates.volumeZoneEnable = tt.enableVolumeZone
			pp.enabledPredicates.podTopologySpreadEnable = tt.enablePodTopologySpread
			pp.enabledPredicates.volumeBindingEnable = tt.enableVolumeBinding
			pp.enabledPredicates.dynamicResourceAllocationEnable = tt.enableDRA

			nodeMap := map[string]k8sframework.NodeInfo{}
			client := k8sfake.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(client, 0)
			pp.Handle = k8s.NewFramework(
				nodeMap,
				k8s.WithClientSet(client),
				k8s.WithInformerFactory(informerFactory),
			)

			pp.InitPlugin()

			// Verify FilterPlugins
			for _, pluginName := range tt.expectInFilter {
				if _, exists := pp.FilterPlugins[pluginName]; !exists {
					t.Errorf("expected %s in FilterPlugins, but not found", pluginName)
				}
			}
			for _, pluginName := range tt.expectNotInFilter {
				if _, exists := pp.FilterPlugins[pluginName]; exists {
					t.Errorf("expected %s not in FilterPlugins, but found", pluginName)
				}
			}

			// Verify StableFilterPlugins
			for _, pluginName := range tt.expectInStableFilter {
				if _, exists := pp.StableFilterPlugins[pluginName]; !exists {
					t.Errorf("expected %s in StableFilterPlugins, but not found", pluginName)
				}
			}

			// Verify PrefilterPlugins
			for _, pluginName := range tt.expectInPrefilter {
				if _, exists := pp.PrefilterPlugins[pluginName]; !exists {
					t.Errorf("expected %s in PrefilterPlugins, but not found", pluginName)
				}
			}

			// Verify ReservePlugins
			for _, pluginName := range tt.expectInReserve {
				if _, exists := pp.ReservePlugins[pluginName]; !exists {
					t.Errorf("expected %s in ReservePlugins, but not found", pluginName)
				}
			}
			for _, pluginName := range tt.expectNotInReserve {
				if _, exists := pp.ReservePlugins[pluginName]; exists {
					t.Errorf("expected %s not in ReservePlugins, but found", pluginName)
				}
			}

			// Verify PreBindPlugins
			for _, pluginName := range tt.expectInPreBind {
				if _, exists := pp.PreBindPlugins[pluginName]; !exists {
					t.Errorf("expected %s in PreBindPlugins, but not found", pluginName)
				}
			}
			for _, pluginName := range tt.expectNotInPreBind {
				if _, exists := pp.PreBindPlugins[pluginName]; exists {
					t.Errorf("expected %s not in PreBindPlugins, but found", pluginName)
				}
			}

			// Verify ScorePlugins
			for _, pluginName := range tt.expectInScore {
				if _, exists := pp.ScorePlugins[pluginName]; !exists {
					t.Errorf("expected %s in ScorePlugins, but not found", pluginName)
				}
			}
			for _, pluginName := range tt.expectNotInScore {
				if _, exists := pp.ScorePlugins[pluginName]; exists {
					t.Errorf("expected %s not in ScorePlugins, but found", pluginName)
				}
			}

			// Verify VolumeBinding weight if enabled
			if tt.enableVolumeBinding {
				if weight, exists := pp.ScoreWeights[vbcap.Name]; !exists || weight == 0 {
					t.Errorf("expected VolumeBinding to have non-zero weight in ScoreWeights")
				}
			}
		})
	}
}
