/*
Copyright 2017 The Kubernetes Authors.

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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func jobInfoEqual(l, r *JobInfo) bool {
	return equality.Semantic.DeepEqual(l, r)
}

func TestAddTaskInfo(t *testing.T) {
	// case1
	case01UID := JobID("uid")
	case01Ns := "c1"
	case01Owner := buildOwnerReference("uid")

	case01Pod1 := buildPod(case01Ns, "p1", "", v1.PodPending, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task1 := NewTaskInfo(case01Pod1)
	case01Pod2 := buildPod(case01Ns, "p2", "n1", v1.PodRunning, BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task2 := NewTaskInfo(case01Pod2)
	case01Pod3 := buildPod(case01Ns, "p3", "n1", v1.PodPending, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task3 := NewTaskInfo(case01Pod3)
	case01Pod4 := buildPod(case01Ns, "p4", "n1", v1.PodPending, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task4 := NewTaskInfo(case01Pod4)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		expected *JobInfo
	}{
		{
			name: "add 1 pending owner pod, 1 running owner pod",
			uid:  case01UID,
			pods: []*v1.Pod{case01Pod1, case01Pod2, case01Pod3, case01Pod4},
			expected: &JobInfo{
				UID:          case01UID,
				Allocated:    buildResource("4000m", "4G", map[string]string{"pods": "3"}, 0),
				TotalRequest: buildResource("5000m", "5G", map[string]string{"pods": "4"}, 0),
				Tasks: tasksMap{
					case01Task1.UID: case01Task1,
					case01Task2.UID: case01Task2,
					case01Task3.UID: case01Task3,
					case01Task4.UID: case01Task4,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Running: {
						case01Task2.UID: case01Task2,
					},
					Pending: {
						case01Task1.UID: case01Task1,
					},
					Bound: {
						case01Task3.UID: case01Task3,
						case01Task4.UID: case01Task4,
					},
				},
				NodesFitErrors:   make(map[TaskID]*FitErrors),
				TaskMinAvailable: make(map[string]int32),
				Budget:           &DisruptionBudget{},
			},
		},
	}

	for i, test := range tests {
		ps := NewJobInfo(test.uid)
		ps.Budget = &DisruptionBudget{}

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}
		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}

func TestDeleteTaskInfo(t *testing.T) {
	// case1
	case01UID := JobID("owner1")
	case01Ns := "c1"
	case01Owner := buildOwnerReference(string(case01UID))
	case01Pod1 := buildPod(case01Ns, "p1", "", v1.PodPending, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task1 := NewTaskInfo(case01Pod1)
	case01Pod2 := buildPod(case01Ns, "p2", "n1", v1.PodRunning, BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Pod3 := buildPod(case01Ns, "p3", "n1", v1.PodRunning, BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case01Owner}, make(map[string]string))
	case01Task3 := NewTaskInfo(case01Pod3)

	// case2
	case02UID := JobID("owner2")
	case02Ns := "c2"
	case02Owner := buildOwnerReference(string(case02UID))
	case02Pod1 := buildPod(case02Ns, "p1", "", v1.PodPending, BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Task1 := NewTaskInfo(case02Pod1)
	case02Pod2 := buildPod(case02Ns, "p2", "n1", v1.PodPending, BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Pod3 := buildPod(case02Ns, "p3", "n1", v1.PodRunning, BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case02Owner}, make(map[string]string))
	case02Task3 := NewTaskInfo(case02Pod3)

	tests := []struct {
		name     string
		uid      JobID
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *JobInfo
	}{
		{
			name:   "add 1 pending owner pod, 2 running owner pod, remove 1 running owner pod",
			uid:    case01UID,
			pods:   []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			rmPods: []*v1.Pod{case01Pod2},
			expected: &JobInfo{
				Allocated:    buildResource("3000m", "3G", map[string]string{"pods": "1"}, 0),
				TotalRequest: buildResource("4000m", "4G", map[string]string{"pods": "2"}, 0),
				UID:          case01UID,
				Tasks: tasksMap{
					case01Task1.UID: case01Task1,
					case01Task3.UID: case01Task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {case01Task1.UID: case01Task1},
					Running: {case01Task3.UID: case01Task3},
				},
				NodesFitErrors:   make(map[TaskID]*FitErrors),
				TaskMinAvailable: make(map[string]int32),
				Budget:           &DisruptionBudget{},
			},
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02UID,
			pods:   []*v1.Pod{case02Pod1, case02Pod2, case02Pod3},
			rmPods: []*v1.Pod{case02Pod2},
			expected: &JobInfo{
				Allocated:    buildResource("3000m", "3G", map[string]string{"pods": "1"}, 0),
				TotalRequest: buildResource("4000m", "4G", map[string]string{"pods": "2"}, 0),
				UID:          case02UID,
				Tasks: tasksMap{
					case02Task1.UID: case02Task1,
					case02Task3.UID: case02Task3,
				},
				TaskStatusIndex: map[TaskStatus]tasksMap{
					Pending: {
						case02Task1.UID: case02Task1,
					},
					Running: {
						case02Task3.UID: case02Task3,
					},
				},
				NodesFitErrors:   make(map[TaskID]*FitErrors),
				TaskMinAvailable: make(map[string]int32),
				Budget:           &DisruptionBudget{},
			},
		},
	}

	for i, test := range tests {
		ps := NewJobInfo(test.uid)
		ps.Budget = &DisruptionBudget{}

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewTaskInfo(pod)
			ps.DeleteTaskInfo(pi)
		}

		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}

func TestTaskSchedulingReason(t *testing.T) {
	t1 := buildPod("ns1", "task-1", "", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))
	t2 := buildPod("ns1", "task-2", "", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))
	t3 := buildPod("ns1", "task-3", "node1", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))
	t4 := buildPod("ns1", "task-4", "node2", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))
	t5 := buildPod("ns1", "task-5", "node3", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))
	t6 := buildPod("ns1", "task-6", "", v1.PodPending, BuildResourceList("1", "1G"), nil, make(map[string]string))

	originReason1 := ". Origin reason is task-6: 0/3 nodes are unavailable: 1 node(s) pod number exceeded, 2 node(s) resource fit failed."

	tests := []struct {
		desc     string
		pods     []*v1.Pod
		jobid    JobID
		origin   string // origin reason string from first failed pod
		nodefes  map[TaskID]*FitErrors
		expected map[types.UID]string
	}{
		{
			desc:  "task3 ~ 5 are schedulable",
			pods:  []*v1.Pod{t1, t2, t3, t4, t5, t6},
			jobid: JobID("case1"),
			nodefes: map[TaskID]*FitErrors{
				TaskID(t6.UID): {
					nodes: map[string]*FitError{
						"node1": {Status: []*Status{{Reason: NodePodNumberExceeded}}},
						"node2": {Status: []*Status{{Reason: NodeResourceFitFailed}}},
						"node3": {Status: []*Status{{Reason: NodeResourceFitFailed}}},
					},
				},
			},
			expected: map[types.UID]string{
				"pg":   "pod group is not ready, 6 Pending, 6 minAvailable; Pending: 3 Schedulable, 3 Unschedulable" + originReason1,
				t1.UID: "pod group is not ready, 6 Pending, 6 minAvailable; Pending: 3 Schedulable, 3 Unschedulable" + originReason1,
				t2.UID: "pod group is not ready, 6 Pending, 6 minAvailable; Pending: 3 Schedulable, 3 Unschedulable" + originReason1,
				t3.UID: "Pod ns1/task-3 can possibly be assigned to node1",
				t4.UID: "Pod ns1/task-4 can possibly be assigned to node2",
				t5.UID: "Pod ns1/task-5 can possibly be assigned to node3",
				t6.UID: "0/3 nodes are unavailable: 1 node(s) pod number exceeded, 2 node(s) resource fit failed.",
			},
		},
	}

	for i, test := range tests {
		job := NewJobInfo(test.jobid)
		pg := scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1",
				Name:      "pg1",
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: int32(len(test.pods)),
			},
		}
		for _, pod := range test.pods {
			// set pod group
			pod.Annotations = map[string]string{
				schedulingv2.KubeGroupNameAnnotationKey: pg.Name,
			}

			// add TaskInfo
			ti := NewTaskInfo(pod)
			job.AddTaskInfo(ti)

			// pod is schedulable
			if len(pod.Spec.NodeName) > 0 {
				ti.LastTransaction = &TransactionContext{
					NodeName: pod.Spec.NodeName,
					Status:   Allocated,
				}
			}
		}
		// complete job
		job.SetPodGroup(&PodGroup{PodGroup: pg})
		job.NodesFitErrors = test.nodefes
		job.TaskStatusIndex = map[TaskStatus]tasksMap{Pending: {}}
		for _, task := range job.Tasks {
			task.Status = Pending
			job.TaskStatusIndex[Pending][task.UID] = task
		}
		job.JobFitErrors = job.FitError()

		// assert
		for uid, exp := range test.expected {
			msg := job.JobFitErrors
			if uid != "pg" {
				_, msg, _ = job.TaskSchedulingReason(TaskID(uid))
			}
			if msg != exp {
				t.Errorf("[x] case #%d, task %v\nwant: %s\n got: %s", i, uid, exp, msg)
			}
		}
	}
}

func TestJobInfo(t *testing.T) {
	newTaskFunc := func(uid, jobUid types.UID, status TaskStatus, resources *Resource) *TaskInfo {
		isBestEffort := resources.IsEmpty()
		return &TaskInfo{
			UID:  TaskID(uid),
			Job:  JobID(jobUid),
			Name: string(uid),
			TransactionContext: TransactionContext{
				Status: status,
			},
			Resreq:     resources,
			InitResreq: resources,
			BestEffort: isBestEffort,
			NumaInfo: &TopologyInfo{
				ResMap: map[int]v1.ResourceList{},
			},
		}
	}

	testCases := []struct {
		name                             string
		jobUID                           JobID
		jobMinAvailable                  int32
		tasks                            []*TaskInfo
		expectedPendingBestEffortTaskNum int32
		expectedIsReady                  bool
		expectedIsPipelined              bool
		expectedIsStarving               bool
	}{
		{
			name:            "starving job",
			jobUID:          "job-1",
			jobMinAvailable: 5,
			tasks: []*TaskInfo{
				newTaskFunc("pending-besteffort-task-1", "job-1", Pending, EmptyResource()),
				newTaskFunc("pipelined-besteffort-task-1", "job-1", Pipelined, EmptyResource()),
				newTaskFunc("running-besteffort-task-1", "job-1", Running, EmptyResource()),
				newTaskFunc("pending-unbesteffort-task-1", "job-1", Pending, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
				newTaskFunc("pipelined-unbesteffort-task-1", "job-1", Pipelined, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
				newTaskFunc("running-unbesteffort-task-1", "job-1", Running, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
			},
			expectedPendingBestEffortTaskNum: 1,
			expectedIsReady:                  false,
			expectedIsPipelined:              true,
			expectedIsStarving:               true,
		},

		{
			name:            "ready job",
			jobUID:          "job-1",
			jobMinAvailable: 3,
			tasks: []*TaskInfo{
				newTaskFunc("pending-besteffort-task-1", "job-1", Pending, EmptyResource()),
				newTaskFunc("pipelined-besteffort-task-1", "job-1", Pipelined, EmptyResource()),
				newTaskFunc("running-besteffort-task-1", "job-1", Running, EmptyResource()),
				newTaskFunc("pending-unbesteffort-task-1", "job-1", Pending, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
				newTaskFunc("pipelined-unbesteffort-task-1", "job-1", Pipelined, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
				newTaskFunc("running-unbesteffort-task-1", "job-1", Running, NewResource(v1.ResourceList{"cpu": resource.MustParse("100m")})),
			},
			expectedPendingBestEffortTaskNum: 1,
			expectedIsReady:                  true,
			expectedIsPipelined:              true,
			expectedIsStarving:               false,
		},
	}

	for _, tc := range testCases {
		jobInfo := NewJobInfo(tc.jobUID, tc.tasks...)
		jobInfo.MinAvailable = tc.jobMinAvailable
		actualPendingBestEffortTaskNum := jobInfo.PendingBestEffortTaskNum()
		actualIsReady := jobInfo.IsReady()
		actualIsPipelined := jobInfo.IsPipelined()
		actualIsStarving := jobInfo.IsStarving()

		if !assert.Equal(t, actualPendingBestEffortTaskNum, tc.expectedPendingBestEffortTaskNum) {
			t.Errorf("unexpected PendingBestEffortTaskNum; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedPendingBestEffortTaskNum, actualPendingBestEffortTaskNum)
		}
		if !assert.Equal(t, actualIsReady, tc.expectedIsReady) {
			t.Errorf("unexpected Ready; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedIsReady, actualIsReady)
		}
		if !assert.Equal(t, actualIsPipelined, tc.expectedIsPipelined) {
			t.Errorf("unexpected IsPipelined; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedIsPipelined, actualIsPipelined)
		}
		if !assert.Equal(t, actualIsStarving, tc.expectedIsStarving) {
			t.Errorf("unexpected IsStarving; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedIsStarving, actualIsStarving)
		}
	}
}

func TestGetElasticResources(t *testing.T) {
	resNoGPU := BuildResourceList("1", "1G")
	resWithGPU := BuildResourceListWithGPU("1", "1G", "1")
	wantNoGPU := BuildResourceList("1", "1G", []ScalarResource{{Name: "pods", Value: "1"}}...)
	wantWithGPU := BuildResourceListWithGPU("1", "1G", "1", []ScalarResource{{Name: "pods", Value: "1"}}...)
	tests := []struct {
		pods     []*v1.Pod
		podgroup scheduling.PodGroup
		want     *Resource
	}{
		{
			pods: []*v1.Pod{
				buildPod("ns1", "task-1", "node1", v1.PodRunning, resWithGPU, nil, make(map[string]string)),
			},
			podgroup: BuildPodgroup("pg1", "ns1", 1, wantWithGPU),
			want:     EmptyResource(),
		},
		{
			pods: []*v1.Pod{
				buildPod("ns1", "task-1", "node1", v1.PodRunning, resWithGPU, nil, make(map[string]string)),
				buildPod("ns1", "task-2", "node2", v1.PodRunning, resNoGPU, nil, make(map[string]string)),
			},
			podgroup: BuildPodgroup("pg1", "ns1", 1, wantWithGPU),
			want:     NewResource(wantNoGPU),
		},
		{
			pods: []*v1.Pod{
				buildPod("ns1", "task-1", "node1", v1.PodRunning, resNoGPU, nil, make(map[string]string)),
				buildPod("ns1", "task-2", "node2", v1.PodRunning, resNoGPU, nil, make(map[string]string)),
			},
			podgroup: BuildPodgroup("pg1", "ns1", 1, wantWithGPU),
			want:     NewResource(wantNoGPU),
		},
		{
			pods: []*v1.Pod{
				buildPod("ns1", "task-1", "node1", v1.PodRunning, resWithGPU, nil, make(map[string]string)),
				buildPod("ns1", "task-2", "node2", v1.PodRunning, resWithGPU, nil, make(map[string]string)),
			},
			podgroup: BuildPodgroup("pg1", "ns1", 1, wantWithGPU),
			want:     NewResource(wantWithGPU),
		},
	}

	for i, test := range tests {
		job := NewJobInfo("job")
		for _, pod := range test.pods {
			job.AddTaskInfo(NewTaskInfo(pod))
		}
		job.SetPodGroup(&PodGroup{PodGroup: test.podgroup})
		if elastic := job.GetElasticResources(); !elastic.Equal(test.want, Zero) {
			t.Fatalf("case %d:expected %+v, got %+v", i, test.want, elastic)
		}
	}
}

func TestHasTopologyHardConstrain(t *testing.T) {
	HighestTierAllowedTwo := 2
	HighestTierAllowedThree := 3
	tests := []struct {
		name            string
		jobInfo         *JobInfo
		expectedHasHard bool
		expectedTier    int
	}{
		{
			name: "Nil PodGroup",
			jobInfo: &JobInfo{
				PodGroup: nil,
			},
			expectedHasHard: false,
			expectedTier:    0,
		},
		{
			name: "Nil NetworkTopologies",
			jobInfo: &JobInfo{
				PodGroup: &PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							NetworkTopology: nil,
						},
					},
				},
			},
			expectedHasHard: false,
			expectedTier:    0,
		},
		{
			name: "Hard Mode",
			jobInfo: &JobInfo{
				PodGroup: &PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							NetworkTopology: &scheduling.NetworkTopologySpec{
								Mode:               scheduling.HardNetworkTopologyMode,
								HighestTierAllowed: &HighestTierAllowedTwo,
							},
						},
					},
				},
			},
			expectedHasHard: true,
			expectedTier:    2,
		},
		{
			name: "Soft Mode",
			jobInfo: &JobInfo{
				PodGroup: &PodGroup{
					PodGroup: scheduling.PodGroup{
						Spec: scheduling.PodGroupSpec{
							NetworkTopology: &scheduling.NetworkTopologySpec{
								Mode:               scheduling.SoftNetworkTopologyMode,
								HighestTierAllowed: &HighestTierAllowedThree,
							},
						},
					},
				},
			},
			expectedHasHard: false,
			expectedTier:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasHard, tier := tt.jobInfo.HasTopologyHardConstrain()
			assert.Equal(t, tt.expectedHasHard, hasHard)
			assert.Equal(t, tt.expectedTier, tier)
		})
	}
}
