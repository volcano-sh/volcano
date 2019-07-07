package reserve

import (
	"reflect"
	"testing"
	"time"

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	kbapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/util"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
)

func TestReserve(t *testing.T) {
	framework.RegisterPluginBuilder("gang", gang.New)

	n1 := util.BuildNode("n1", util.BuildResourceList("3", "4G"), make(map[string]string))
	n1.Status.Allocatable["pods"] = resource.MustParse("100")

	n2 := util.BuildNode("n2", util.BuildResourceList("3", "4G"), make(map[string]string))
	n2.Status.Allocatable["pods"] = resource.MustParse("100")

	n3 := util.BuildNode("n3", util.BuildResourceList("3", "4G"), make(map[string]string))
	n3.Status.Allocatable["pods"] = resource.MustParse("100")

	n4 := util.BuildNode("n4", util.BuildResourceList("3", "4G"), make(map[string]string))
	n4.Status.Allocatable["pods"] = resource.MustParse("100")

	tests := []struct {
		name           string
		podGroups      []*v1alpha1.PodGroup
		pods           []*v1.Pod
		nodes          []*v1.Node
		queues         []*v1alpha1.Queue
		forceAllocated map[string]api.TaskStatus
		fakeAllocated  map[string]api.TaskStatus
		forceNode      map[string]string
		fakeNode       map[string]string
	}{
		{
			name: "basic reserve action test",
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 2,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("ns1", "p1", "n1", v1.PodRunning,
					util.BuildResourceList("1", "1G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p2", "", v1.PodPending,
					util.BuildResourceList("2", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p3", "", v1.PodPending,
					util.BuildResourceList("1", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				n1,
			},
			queues: []*v1alpha1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: v1alpha1.QueueSpec{
						Weight: 1,
					},
				},
			},
			forceAllocated: map[string]api.TaskStatus{
				"p2": api.ForceAllocated,
			},
			fakeAllocated: map[string]api.TaskStatus{
				"p3": api.FakeAllocated,
			},
			forceNode: map[string]string{
				"p2": "n1",
			},
			fakeNode: map[string]string{
				"p3": "n1",
			},
		},
		{
			name: "reserve failed for task",
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 2,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("ns1", "p1", "n1", v1.PodRunning,
					util.BuildResourceList("1", "1G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p2", "", v1.PodPending,
					util.BuildResourceList("2", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p3", "", v1.PodPending,
					util.BuildResourceList("2", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				n1,
			},
			queues: []*v1alpha1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: v1alpha1.QueueSpec{
						Weight: 1,
					},
				},
			},
			forceAllocated: map[string]api.TaskStatus{},
			fakeAllocated:  map[string]api.TaskStatus{},
			forceNode:      map[string]string{},
			fakeNode:       map[string]string{},
		},
		{
			name: "multiple nodes succeed",
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 3,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 2,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("ns1", "p1", "n1", v1.PodPending,
					util.BuildResourceList("1", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p2", "n2", v1.PodPending,
					util.BuildResourceList("1.5", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p3", "n3", v1.PodPending,
					util.BuildResourceList("2", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p4", "", v1.PodPending,
					util.BuildResourceList("3", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p5", "", v1.PodPending,
					util.BuildResourceList("3", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				n1, n2, n3,
			},
			queues: []*v1alpha1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: v1alpha1.QueueSpec{
						Weight: 1,
					},
				},
			},
			forceAllocated: map[string]api.TaskStatus{},
			fakeAllocated: map[string]api.TaskStatus{
				"p4": api.FakeAllocated,
				"p5": api.FakeAllocated,
			},
			forceNode: map[string]string{},
			fakeNode: map[string]string{
				"p4": "n2",
				"p5": "n1",
			},
		},
		{
			name: "multiple nodes reserve failed due to no enough resources",
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 3,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "ns1",
						CreationTimestamp: metav1.Time{
							time.Now().AddDate(-1, 0, 0),
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 3,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("ns1", "p1", "n1", v1.PodPending,
					util.BuildResourceList("1", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p2", "n2", v1.PodPending,
					util.BuildResourceList("1.5", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p3", "n3", v1.PodPending,
					util.BuildResourceList("2", "1.5G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p4", "", v1.PodPending,
					util.BuildResourceList("3", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p5", "", v1.PodPending,
					util.BuildResourceList("3", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p6", "", v1.PodPending,
					util.BuildResourceList("3", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4,
			},
			queues: []*v1alpha1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: v1alpha1.QueueSpec{
						Weight: 1,
					},
				},
			},
			forceAllocated: map[string]api.TaskStatus{},
			fakeAllocated:  map[string]api.TaskStatus{},
			forceNode:      map[string]string{},
			fakeNode:       map[string]string{},
		},
	}

	reserveAction := New(framework.Arguments{
		conf.ReservedNodePercent:      "50",
		conf.StarvingJobTimeThreshold: "1",
	})

	for i, test := range tests {
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
			Recorder:      record.NewFakeRecorder(100),
		}

		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}

		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, pg := range test.podGroups {
			schedulerCache.AddPodGroupV1alpha1(pg)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueueV1alpha1(q)
		}

		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                     "gang",
						EnabledJobConditionReady: &trueValue,
						EnabledJobReady:          &trueValue,
					},
				},
			},
		})

		defer framework.CloseSession(ssn)

		reserveAction.Execute(ssn)

		forceAllocated := make(map[string]api.TaskStatus)
		fakeAllocated := make(map[string]api.TaskStatus)
		forceNode := make(map[string]string)
		fakeNode := make(map[string]string)
		for _, job := range ssn.Jobs {
			for status, taskMap := range job.TaskStatusIndex {
				switch status {
				case api.ForceAllocated:
					for _, task := range taskMap {
						forceAllocated[task.Name] = api.ForceAllocated
						forceNode[task.Name] = task.NodeName
					}
				case api.FakeAllocated:
					for _, task := range taskMap {
						fakeAllocated[task.Name] = api.FakeAllocated
						fakeNode[task.Name] = task.NodeName
					}
				}
			}
		}

		if !reflect.DeepEqual(test.forceAllocated, forceAllocated) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.forceAllocated, forceAllocated)
		}

		if !reflect.DeepEqual(test.fakeAllocated, fakeAllocated) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.fakeAllocated, fakeAllocated)
		}

		if !reflect.DeepEqual(test.forceNode, forceNode) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.forceNode, forceNode)
		}

		if !reflect.DeepEqual(test.fakeNode, fakeNode) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.fakeNode, fakeNode)
		}
	}
}

func TestCalculateNodeScoreFunc(t *testing.T) {
	framework.RegisterPluginBuilder("gang", gang.New)

	n1 := util.BuildNode("n1", util.BuildResourceList("5", "5G"), make(map[string]string))
	n1.Status.Allocatable["pods"] = resource.MustParse("100")

	n2 := util.BuildNode("n2", util.BuildResourceList("5", "5G"), make(map[string]string))
	n2.Status.Allocatable["pods"] = resource.MustParse("100")

	n3 := util.BuildNode("n3", util.BuildResourceList("5", "5G"), make(map[string]string))
	n3.Status.Allocatable["pods"] = resource.MustParse("100")

	n4 := util.BuildNode("n4", util.BuildResourceList("5", "5G"), make(map[string]string))
	n4.Status.Allocatable["pods"] = resource.MustParse("100")

	tests := []struct {
		name         string
		podGroups    []*v1alpha1.PodGroup
		pods         []*v1.Pod
		nodes        []*v1.Node
		queues       []*v1alpha1.Queue
		expectedHost schedulerapi.HostPriorityList
	}{
		{
			name: "calculateNodeScore function test",
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 4,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "ns1",
					},
					Spec: v1alpha1.PodGroupSpec{
						Queue:     "q1",
						MinMember: 1,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("ns1", "p1", "n1", v1.PodRunning,
					util.BuildResourceList("1", "1G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p2", "n2", v1.PodRunning,
					util.BuildResourceList("2", "2G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p3", "n3", v1.PodRunning,
					util.BuildResourceList("3", "3G"), "pg1",
					make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "p4", "n4", v1.PodRunning,
					util.BuildResourceList("4", "4G"), "pg1",
					make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4,
			},
			queues: []*v1alpha1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: v1alpha1.QueueSpec{
						Weight: 1,
					},
				},
			},
			expectedHost: schedulerapi.HostPriorityList{
				{
					Host:  "n2",
					Score: 100,
				},
				{
					Host:  "n1",
					Score: 100,
				},
				{
					Host:  "n4",
					Score: 0,
				},
				{
					Host:  "n3",
					Score: 0,
				},
			},
		},
	}

	for i, test := range tests {
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
			Recorder:      record.NewFakeRecorder(100),
		}

		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}

		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, pg := range test.podGroups {
			schedulerCache.AddPodGroupV1alpha1(pg)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueueV1alpha1(q)
		}

		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                     "gang",
						EnabledJobConditionReady: &trueValue,
						EnabledJobReady:          &trueValue,
					},
				},
			},
		})

		defer framework.CloseSession(ssn)

		pi := kbapi.NewTaskInfo(util.BuildPod("ns1", "p5", "", v1.PodPending,
			util.BuildResourceList("1", "1G"), "pg2",
			make(map[string]string), make(map[string]string)))

		result := calculateNodeScore(pi, util.GetNodeList(ssn.Nodes))

		if !reflect.DeepEqual(test.expectedHost, result) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expectedHost, result)
		}
	}
}
