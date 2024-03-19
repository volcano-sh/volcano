package nodegroup

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestNodeGroup(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{
		batch.QueueNameKey: "q1",
	}, make(map[string]string))

	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg2", map[string]string{
		batch.QueueNameKey: "q2",
	}, make(map[string]string))

	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi"), map[string]string{
		NodeGroupNameKey: "group1",
	})
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group2",
	})
	n3 := util.BuildNode("n3", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group3",
	})
	n4 := util.BuildNode("n4", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group4",
	})
	n5 := util.BuildNode("n5", api.BuildResourceList("4", "16Gi"), make(map[string]string))

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue: "q1",
		},
	}

	pg2 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg2",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue: "q2",
		},
	}

	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
			Affinity: &schedulingv1.Affinity{
				NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1", "group3"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
				},
				NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2", "group4"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
				},
			},
		},
	}

	queue2 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q2",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
			Affinity: &schedulingv1.Affinity{
				NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
				},
				NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
				},
			},
		},
	}

	tests := []struct {
		name           string
		podGroups      []*schedulingv1.PodGroup
		pods           []*v1.Pod
		nodes          []*v1.Node
		queues         []*schedulingv1.Queue
		arguments      framework.Arguments
		expected       map[string]map[string]float64
		expectedStatus map[string]map[string]int
	}{
		{
			name: "case: soft constraints is subset of hard constraints",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1,
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 100,
					"n2": 0.0,
					"n3": 150,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p1": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
		},
		{
			// test unnormal case
			name: "case: soft constraints is not subset of hard constraints",
			podGroups: []*schedulingv1.PodGroup{
				pg2,
			},
			queues: []*schedulingv1.Queue{
				queue2,
			},
			pods: []*v1.Pod{
				p2,
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p2": {
					"n1": 100,
					"n2": 0.0,
					"n3": 50,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p2": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %v %v", i, test.name), func(t *testing.T) {
			binder := &util.FakeBinder{
				Binds:   map[string]string{},
				Channel: make(chan string),
			}
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				Binder:        binder,
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  &util.FakeVolumeBinder{},

				Recorder: record.NewFakeRecorder(100),
			}

			for _, node := range test.nodes {
				schedulerCache.AddOrUpdateNode(node)
			}
			for _, pod := range test.pods {
				schedulerCache.AddPod(pod)
			}
			for _, ss := range test.podGroups {
				schedulerCache.AddPodGroupV1beta1(ss)
			}
			for _, q := range test.queues {
				schedulerCache.AddQueueV1beta1(q)
			}

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							EnabledPredicate: &trueValue,
							Arguments:        test.arguments,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)

					for _, node := range ssn.Nodes {
						score, err := ssn.NodeOrderFn(task, node)
						if err != nil {
							t.Errorf("case%d: task %s on node %s has err %v", i, taskID, node.Name, err)
							continue
						}
						if expectScore := test.expected[taskID][node.Name]; expectScore != score {
							t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i, taskID, node.Name, expectScore, score)
						}

						status, _ := ssn.PredicateFn(task, node)
						if expectStatus := test.expectedStatus[taskID][node.Name]; expectStatus != status[0].Code {
							t.Errorf("case%d: task %s on node %s expect have status code %v, but get %v", i, taskID, node.Name, expectStatus, status[0].Code)
						}

					}
				}
			}
			t.Logf("nodegroup unit test finished ")
		})
	}

}
