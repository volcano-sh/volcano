/*
Copyright 2026 The Volcano Authors.

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

package gate

import (
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func gatedTask(name string) *api.TaskInfo {
	return &api.TaskInfo{
		Name:      name,
		Namespace: "default",
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: v1.PodSpec{
				SchedulingGates: []v1.PodSchedulingGate{
					{Name: schedulingv1beta1.QueueAllocationGateKey},
				},
			},
		},
	}
}

// Run and Stop are both driven by the scheduler's stopCh, so a scheduling cycle
// still in flight can reach Enqueue after Stop has returned.
func TestEnqueueAfterStop(t *testing.T) {
	m := NewSchGateManager(fake.NewSimpleClientset(), 2)
	m.Start()
	m.Stop()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Enqueue after Stop panicked: %v", r)
		}
	}()
	m.Enqueue(gatedTask("after-stop"))
}

func TestConcurrentEnqueueDuringStop(t *testing.T) {
	m := NewSchGateManager(fake.NewSimpleClientset(), 2)
	m.Start()

	var wg sync.WaitGroup
	panics := make(chan any, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()
			m.Enqueue(gatedTask("concurrent"))
		}()
	}
	m.Stop()
	wg.Wait()
	close(panics)

	if r, ok := <-panics; ok {
		t.Fatalf("Enqueue panicked while Stop was running: %v", r)
	}
}
