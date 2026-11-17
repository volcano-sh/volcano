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

package eviction

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/apis/scheduling"
	"k8s.io/utils/ptr"
)

const evictMsg = "node memory pressure"

// evictionRequest is the eviction the client was asked to create.
type evictionRequest struct {
	version            string
	gracePeriodSeconds *int64
}

// newClient returns a fake client with the pod that advertises the eviction
// subresource of pods in the given policy version, as the API server does in
// its core v1 discovery. An empty version advertises no eviction subresource.
func newClient(pod *corev1.Pod, evictionVersion string) *fake.Clientset {
	client := fake.NewSimpleClientset(pod)
	coreResources := &metav1.APIResourceList{GroupVersion: "v1"}
	if evictionVersion != "" {
		coreResources.APIResources = []metav1.APIResource{
			{Name: "pods/eviction", Group: "policy", Version: evictionVersion, Kind: "Eviction"},
		}
	}
	client.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{coreResources}
	return client
}

// evictionRequests returns the evictions the client was asked to create.
func evictionRequests(client *fake.Clientset) []evictionRequest {
	var requests []evictionRequest
	for _, action := range client.Actions() {
		if action.GetVerb() != "create" || action.GetSubresource() != "eviction" {
			continue
		}
		switch e := action.(k8stesting.CreateAction).GetObject().(type) {
		case *policyv1.Eviction:
			requests = append(requests, evictionRequest{version: "v1", gracePeriodSeconds: e.DeleteOptions.GracePeriodSeconds})
		case *policyv1beta1.Eviction:
			requests = append(requests, evictionRequest{version: "v1beta1", gracePeriodSeconds: e.DeleteOptions.GracePeriodSeconds})
		}
	}
	return requests
}

func TestEvict(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "be-pod", Namespace: "default"}}
	criticalPod := pod.DeepCopy()
	criticalPod.Spec.Priority = ptr.To(scheduling.SystemCriticalPriority)

	tests := []struct {
		name               string
		pod                *corev1.Pod
		evictionVersion    string
		gracePeriodSeconds int64
		evictErr           error
		wantEvicted        bool
		wantRequests       []evictionRequest
		wantEvents         []string
	}{
		{
			name:               "evicts the pod with the policy/v1 eviction API",
			pod:                pod,
			evictionVersion:    "v1",
			gracePeriodSeconds: 30,
			wantEvicted:        true,
			wantRequests:       []evictionRequest{{version: "v1", gracePeriodSeconds: ptr.To[int64](30)}},
			wantEvents:         []string{"Warning Evicted " + evictMsg},
		},
		{
			name:               "evicts the pod with the policy/v1beta1 eviction API",
			pod:                pod,
			evictionVersion:    "v1beta1",
			gracePeriodSeconds: 30,
			wantEvicted:        true,
			wantRequests:       []evictionRequest{{version: "v1beta1", gracePeriodSeconds: ptr.To[int64](30)}},
			wantEvents:         []string{"Warning Evicted " + evictMsg},
		},
		{
			name:               "uses a zero grace period instead of a negative one",
			pod:                pod,
			evictionVersion:    "v1",
			gracePeriodSeconds: -1,
			wantEvicted:        true,
			wantRequests:       []evictionRequest{{version: "v1", gracePeriodSeconds: ptr.To[int64](0)}},
			wantEvents:         []string{"Warning Evicted " + evictMsg},
		},
		{
			name:               "does not evict a critical pod",
			pod:                criticalPod,
			evictionVersion:    "v1",
			gracePeriodSeconds: 30,
		},
		{
			name:               "does not evict when the API server has no eviction subresource",
			pod:                pod,
			gracePeriodSeconds: 30,
		},
		{
			name:               "does not record an event when the eviction is refused",
			pod:                pod,
			evictionVersion:    "v1",
			gracePeriodSeconds: 30,
			evictErr:           errors.New("cannot evict pod as it would violate the pod's disruption budget"),
			wantRequests:       []evictionRequest{{version: "v1", gracePeriodSeconds: ptr.To[int64](30)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newClient(tt.pod, tt.evictionVersion)
			if tt.evictErr != nil {
				client.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
					return action.GetSubresource() == "eviction", nil, tt.evictErr
				})
			}
			recorder := record.NewFakeRecorder(len(tt.wantEvents) + 1)

			evicted := NewEviction(client, "node-1").Evict(context.Background(), tt.pod, recorder, tt.gracePeriodSeconds, evictMsg)

			assert.Equal(t, tt.wantEvicted, evicted)
			assert.Equal(t, tt.wantRequests, evictionRequests(client))
			close(recorder.Events)
			var events []string
			for event := range recorder.Events {
				events = append(events, event)
			}
			require.Len(t, events, len(tt.wantEvents))
			assert.Equal(t, tt.wantEvents, events)
		})
	}
}
