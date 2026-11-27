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
	if labels == nil {
		labels = map[string]string{}
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
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

func openCDPSession(t *testing.T) *framework.Session {
	t.Helper()

	framework.RegisterPluginBuilder(PluginName, New)
	t.Cleanup(framework.CleanupPluginBuilders)

	enabled := true
	schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")

	return framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledPreemptable: &enabled,
					EnabledReclaimable: &enabled,
				},
			},
		},
	}, []conf.Configuration{
		{
			Name: "preempt",
		},
		{
			Name: "reclaim",
		},
	})
}

func TestNew(t *testing.T) {
	plugin := New(nil)
	cdpPlugin, ok := plugin.(*CooldownProtectionPlugin)
	if !ok {
		t.Fatalf("expected plugin type %T, got %T", &CooldownProtectionPlugin{}, plugin)
	}
	if cdpPlugin.Name() != PluginName {
		t.Fatalf("expected plugin name %q, got %q", PluginName, cdpPlugin.Name())
	}
}

func TestPodCooldownTime(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		wantEnabled bool
		wantValue   time.Duration
	}{
		{
			name: "annotation cooldown is parsed",
			pod: makePod(nil,
				map[string]string{v1beta1.CooldownTime: "600s"},
				time.Now().Add(-100*time.Second)),
			wantEnabled: true,
			wantValue:   600 * time.Second,
		},
		{
			name: "label cooldown is parsed",
			pod: makePod(
				map[string]string{v1beta1.CooldownTime: "300s"},
				nil,
				time.Now().Add(-100*time.Second)),
			wantEnabled: true,
			wantValue:   300 * time.Second,
		},
		{
			name: "invalid cooldown is ignored",
			pod: makePod(nil,
				map[string]string{v1beta1.CooldownTime: "600abcde"},
				time.Now().Add(-100*time.Second)),
			wantEnabled: false,
			wantValue:   0,
		},
		{
			name:        "missing cooldown is disabled",
			pod:         makePod(nil, nil, time.Now().Add(-100*time.Second)),
			wantEnabled: false,
			wantValue:   0,
		},
	}

	plugin := &CooldownProtectionPlugin{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotEnabled := plugin.podCooldownTime(tt.pod)
			if gotEnabled != tt.wantEnabled {
				t.Errorf("podCooldownTime() got enabled=%v, want %v", gotEnabled, tt.wantEnabled)
			}
			if gotValue != tt.wantValue {
				t.Errorf("podCooldownTime() got value=%v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestPreemptableAndReclaimable(t *testing.T) {
	plugin := &CooldownProtectionPlugin{}
	now := time.Now()
	taskCooldownNotExpired := api.NewTaskInfo(
		makePod(
			map[string]string{v1beta1.PodPreemptable: "true", v1beta1.CooldownTime: "10m"},
			nil,
			now.Add(-100*time.Second),
		),
	)
	taskCooldownExpired := api.NewTaskInfo(
		makePod(
			map[string]string{v1beta1.PodPreemptable: "true"},
			map[string]string{v1beta1.CooldownTime: "10m"},
			now.Add(-800*time.Second),
		),
	)
	taskNoCooldown := api.NewTaskInfo(
		makePod(
			map[string]string{v1beta1.PodPreemptable: "true"},
			nil,
			now.Add(-100*time.Second),
		),
	)

	tests := []struct {
		name          string
		preemptees    []*api.TaskInfo
		expectVictims []*api.TaskInfo
	}{
		{
			name:          "cooldown not expired filters victim",
			preemptees:    []*api.TaskInfo{taskCooldownNotExpired},
			expectVictims: nil,
		},
		{
			name:          "cooldown expired allows victim",
			preemptees:    []*api.TaskInfo{taskCooldownExpired},
			expectVictims: []*api.TaskInfo{taskCooldownExpired},
		},
		{
			name:          "no cooldown uses default preemptable behavior",
			preemptees:    []*api.TaskInfo{taskNoCooldown},
			expectVictims: []*api.TaskInfo{taskNoCooldown},
		},
		{
			name: "mixed victims keep only eligible tasks",
			preemptees: []*api.TaskInfo{
				taskCooldownNotExpired,
				taskCooldownExpired,
				taskNoCooldown,
			},
			expectVictims: []*api.TaskInfo{
				taskCooldownExpired,
				taskNoCooldown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := openCDPSession(t)
			defer framework.CloseSession(ssn)

			plugin.OnSessionOpen(ssn)

			preemptableVictims := ssn.Preemptable(&api.TaskInfo{}, tt.preemptees)
			if !equality.Semantic.DeepEqual(preemptableVictims, tt.expectVictims) {
				t.Errorf("Preemptable() got victims %v, want %v", preemptableVictims, tt.expectVictims)
			}

			reclaimableVictims := ssn.Reclaimable(&api.TaskInfo{}, tt.preemptees)
			if !equality.Semantic.DeepEqual(reclaimableVictims, tt.expectVictims) {
				t.Errorf("Reclaimable() got victims %v, want %v", reclaimableVictims, tt.expectVictims)
			}
		})
	}
}
