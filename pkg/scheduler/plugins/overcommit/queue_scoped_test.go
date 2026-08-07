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

package overcommit

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestQueueOvercommitFactor(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		factor      float64
		configured  bool
		wantError   bool
	}{
		{
			name: "valid factor",
			annotations: map[string]string{
				schedulingv1beta1.QueueOvercommitFactorAnnotationKey: "1.5",
			},
			factor:     1.5,
			configured: true,
		},
		{
			name:        "annotation omitted",
			annotations: map[string]string{},
		},
		{
			name: "reject factor below one",
			annotations: map[string]string{
				schedulingv1beta1.QueueOvercommitFactorAnnotationKey: "0.5",
			},
			configured: true,
			wantError:  true,
		},
		{
			name: "reject non-finite factor",
			annotations: map[string]string{
				schedulingv1beta1.QueueOvercommitFactorAnnotationKey: "NaN",
			},
			configured: true,
			wantError:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := &scheduling.Queue{ObjectMeta: metav1.ObjectMeta{
				Name:        "queue",
				Annotations: test.annotations,
			}}
			factor, configured, err := queueOvercommitFactor(api.NewQueueInfo(queue))
			if test.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if factor != test.factor || configured != test.configured {
				t.Fatalf("got factor=%v configured=%t, want factor=%v configured=%t", factor, configured, test.factor, test.configured)
			}
		})
	}
}

func TestEffectiveDeserved(t *testing.T) {
	expectedMemory := resource.MustParse("1Gi")
	queue := &scheduling.Queue{
		Spec: scheduling.QueueSpec{
			Deserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Guarantee: scheduling.Guarantee{Resource: v1.ResourceList{
				v1.ResourceCPU:                     resource.MustParse("3"),
				v1.ResourceName("example.com/gpu"): resource.MustParse("1"),
			}},
		},
	}

	effective := effectiveDeserved(queue)
	if effective.MilliCPU != 3000 {
		t.Fatalf("got CPU %v, want 3000m", effective.MilliCPU)
	}
	if effective.Memory != float64(expectedMemory.Value()) {
		t.Fatalf("got memory %v, want 1Gi", effective.Memory)
	}
	if effective.Get(v1.ResourceName("example.com/gpu")) != 0 {
		t.Fatal("guarantee-only GPU dimension must not create queue-scoped admission")
	}
}

func TestQueueScopedRequest(t *testing.T) {
	request := api.NewResource(v1.ResourceList{
		v1.ResourceCPU:                     resource.MustParse("4"),
		v1.ResourceMemory:                  resource.MustParse("8Gi"),
		v1.ResourceName("example.com/gpu"): resource.MustParse("2"),
	})
	deserved := api.NewResource(v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("1"),
	})

	filtered := queueScopedRequest(request, deserved)
	if filtered.MilliCPU != 4000 {
		t.Fatalf("got CPU %v, want 4000m", filtered.MilliCPU)
	}
	if filtered.Memory != 0 {
		t.Fatalf("got memory %v, want 0", filtered.Memory)
	}
	if filtered.Get(v1.ResourceName("example.com/gpu")) != 0 {
		t.Fatal("omitted desired GPU dimension must not be checked")
	}
}

func TestQueueInqueueResourcePropagatesToAncestors(t *testing.T) {
	rootID := api.QueueID(rootQueueName)
	leafID := api.QueueID("leaf")
	op := &overcommitPlugin{queueStates: map[api.QueueID]*queueAdmissionState{
		rootID: {inqueue: api.EmptyResource()},
		leafID: {
			ancestors: []api.QueueID{rootID},
			inqueue:   api.EmptyResource(),
		},
	}}
	job := &api.JobInfo{Queue: leafID}
	inqueueResource := api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")})

	op.addQueueInqueueResource(job, inqueueResource)
	if op.queueStates[leafID].inqueue.MilliCPU != 2000 {
		t.Fatalf("leaf inqueue CPU is %v, want 2000m", op.queueStates[leafID].inqueue.MilliCPU)
	}
	if op.queueStates[rootID].inqueue.MilliCPU != 2000 {
		t.Fatalf("ancestor inqueue CPU is %v, want 2000m", op.queueStates[rootID].inqueue.MilliCPU)
	}
}
