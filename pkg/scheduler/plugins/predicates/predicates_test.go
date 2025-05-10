package predicates

import (
	"testing"

	apiv1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/backfill"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
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
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

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
