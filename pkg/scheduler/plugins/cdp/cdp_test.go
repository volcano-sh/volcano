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

package cdp

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func makePod(labels map[string]string, annotations map[string]string, podScheduledTime time.Time) *v1.Pod {
	annotations[v1beta1.KubeGroupNameAnnotationKey] = "test-group"
	phase := v1.PodPending
	conditions := []v1.PodCondition{}
	if !podScheduledTime.IsZero() {
		phase = v1.PodRunning
		conditions = append(conditions, v1.PodCondition{
			Type:               v1.PodScheduled,
			LastTransitionTime: metav1.NewTime(podScheduledTime),
			Status:             v1.ConditionTrue,
		})
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   "default",
			Labels:      labels,
			Annotations: annotations,
		},
		Status: v1.PodStatus{
			Phase:      phase,
			Conditions: conditions,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{},
			},
		},
	}

}

func addPodCondition(pod *v1.Pod, condType v1.PodConditionType, status v1.ConditionStatus, transitionTime time.Time) *v1.Pod {
	pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
		Type:               condType,
		LastTransitionTime: metav1.NewTime(transitionTime),
		Status:             status,
	})
	return pod
}

func Test_CooldownTimePlugin_podCooldownStartTime(t *testing.T) {
	scheduledTime := time.Now().Add(time.Second * -800).Truncate(time.Second)
	readyTime := time.Now().Add(time.Second * -100).Truncate(time.Second)
	tests := []struct {
		name string
		pod  *v1.Pod
		want time.Time
	}{
		{
			name: "ready pod uses ready time",
			pod: addPodCondition(makePod(map[string]string{}, map[string]string{}, scheduledTime),
				v1.PodReady, v1.ConditionTrue, readyTime),
			want: readyTime,
		},
		{
			name: "ready condition before scheduled condition uses ready time",
			pod: &v1.Pod{Status: v1.PodStatus{Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue, LastTransitionTime: metav1.NewTime(readyTime)},
				{Type: v1.PodScheduled, Status: v1.ConditionTrue, LastTransitionTime: metav1.NewTime(scheduledTime)},
			}}},
			want: readyTime,
		},
		{
			name: "not ready pod falls back to scheduled time",
			pod: addPodCondition(makePod(map[string]string{}, map[string]string{}, scheduledTime),
				v1.PodReady, v1.ConditionFalse, readyTime),
			want: scheduledTime,
		},
		{
			name: "ready condition without transition time falls back to scheduled time",
			pod: addPodCondition(makePod(map[string]string{}, map[string]string{}, scheduledTime),
				v1.PodReady, v1.ConditionTrue, time.Time{}),
			want: scheduledTime,
		},
		{
			name: "pod without ready condition uses scheduled time",
			pod:  makePod(map[string]string{}, map[string]string{}, scheduledTime),
			want: scheduledTime,
		},
		{
			name: "pod without conditions",
			pod:  makePod(map[string]string{}, map[string]string{}, time.Time{}),
			want: time.Time{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &CooldownProtectionPlugin{}
			if got := sp.podCooldownStartTime(tt.pod); !got.Equal(tt.want) {
				t.Errorf("CooldownTimePlugin.podCooldownStartTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_CooldownTimePlugin_podPreemptStableTime(t *testing.T) {
	type args struct {
		pod *v1.Pod
	}
	plugin := &CooldownProtectionPlugin{}
	tests := []struct {
		name        string
		sp          *CooldownProtectionPlugin
		args        args
		wantEnabled bool
		wantValue   time.Duration
	}{
		{
			name: "normal",
			sp:   plugin,
			args: args{
				pod: makePod(map[string]string{},
					map[string]string{v1beta1.CooldownTime: "600s"},
					time.Now().Add(time.Second*-100)),
			},
			wantEnabled: true,
			wantValue:   time.Second * 600,
		},
		{
			name: "not-enabled",
			sp:   plugin,
			args: args{
				pod: makePod(map[string]string{},
					map[string]string{v1beta1.CooldownTime: "600abcde"},
					time.Now().Add(time.Second*-100)),
			},
			wantEnabled: false,
			wantValue:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &CooldownProtectionPlugin{}
			gotValue, gotEnabled := sp.podCooldownTime(tt.args.pod)
			if gotEnabled != tt.wantEnabled {
				t.Errorf("CooldownTimePlugin.podPreemptStableTime() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
			if gotValue != tt.wantValue {
				t.Errorf("CooldownTimePlugin.podPreemptStableTime() gotValue = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestPreemptableFn(t *testing.T) {
	plugin := &CooldownProtectionPlugin{}
	enabledPreemptable := true
	pluginOption := conf.PluginOption{
		Name:               PluginName,
		EnabledPreemptable: &enabledPreemptable,
	}
	schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{pluginOption},
		},
	},
		[]conf.Configuration{
			{
				Name: "preempt",
			},
		},
	)

	plugin.OnSessionOpen(ssn) // register preempt fn

	// prepare preemptor and preemptees
	// task1: should be filtered
	task1 := api.NewTaskInfo(
		makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600s"},
			time.Now().Add(time.Second*-100)),
	)
	// task2: invalid label, not enabled
	task2 := api.NewTaskInfo(
		makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600abcde"},
			time.Now().Add(time.Second*-100)),
	)
	// task3: after stable time, can be preempted
	task3 := api.NewTaskInfo(
		makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600s"},
			time.Now().Add(time.Second*-800)),
	)
	// task4: scheduled before stable time, but became ready within it (slow start-up), should be filtered
	task4 := api.NewTaskInfo(
		addPodCondition(makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600s"},
			time.Now().Add(time.Second*-800)),
			v1.PodReady, v1.ConditionTrue, time.Now().Add(time.Second*-100)),
	)
	// task5: scheduled before stable time and not ready, can be preempted
	task5 := api.NewTaskInfo(
		addPodCondition(makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600s"},
			time.Now().Add(time.Second*-800)),
			v1.PodReady, v1.ConditionFalse, time.Now().Add(time.Second*-100)),
	)
	// task6: ready after stable time, can be preempted
	task6 := api.NewTaskInfo(
		addPodCondition(makePod(map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "600s"},
			time.Now().Add(time.Second*-900)),
			v1.PodReady, v1.ConditionTrue, time.Now().Add(time.Second*-800)),
	)
	preemptees := []*api.TaskInfo{task1, task2, task3, task4, task5, task6}
	victims := ssn.Preemptable(&api.TaskInfo{}, preemptees)

	expectVictims := []*api.TaskInfo{task2, task3, task5, task6}
	if !equality.Semantic.DeepEqual(victims, expectVictims) {
		t.Errorf("stable preempt test not equal! expect victims %v, actual %v", expectVictims, victims)
	}
}
