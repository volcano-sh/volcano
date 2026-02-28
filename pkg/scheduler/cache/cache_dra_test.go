/*
Copyright 2024 The Volcano Authors.

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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8sfeature "k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestBuildTaskDRAResreq(t *testing.T) {
	// Enable DRA feature gate
	originalFeatureGate := utilfeature.DefaultFeatureGate
	defer func() { utilfeature.DefaultFeatureGate = originalFeatureGate }()

	// We need to set the feature gate. Since we cannot easily modify the global default feature gate in some environments,
	// we will try to set it if possible, or assume it is enabled if we can't.
	// However, usually in tests we can replace the feature gate map or use a mutable one.
	// For this test, we will skip if we can't enable it, or try to set it.
	// Note: component-base/featuregate is tricky to mutate in tests if initialized.
	// We will try to set it via command line flags or existing map if mutable.
	// A simpler way often used in k8s tests:
	featureGate := utilfeature.DefaultFeatureGate.(k8sfeature.MutableFeatureGate)
	if err := featureGate.SetFromMap(map[string]bool{string(kubefeatures.DynamicResourceAllocation): true}); err != nil {
		t.Logf("Failed to enable DynamicResourceAllocation feature gate: %v", err)
	}

	tests := []struct {
		name           string
		pod            *v1.Pod
		claims         []*resourcev1.ResourceClaim
		expectedResreq map[string]*schedulingapi.DRAResource
	}{
		{
			name: "No ResourceClaims",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
				Spec:       v1.PodSpec{},
			},
			claims:         nil,
			expectedResreq: nil,
		},
		{
			name: "Single ResourceClaim with one request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimName: pointerString("claim1-obj")},
					},
				},
			},
			claims: []*resourcev1.ResourceClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "claim1-obj", Namespace: "default"},
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{
								{
									Name: "req1",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "gpu.com",
										Count:           1,
									},
								},
							},
						},
					},
				},
			},
			expectedResreq: map[string]*schedulingapi.DRAResource{
				"gpu.com": {Count: 1},
			},
		},
		{
			name: "Multiple ResourceClaims aggregating same device class",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod3", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimName: pointerString("claim1-obj")},
						{Name: "claim2", ResourceClaimName: pointerString("claim2-obj")},
					},
				},
			},
			claims: []*resourcev1.ResourceClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "claim1-obj", Namespace: "default"},
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{
								{
									Name: "req1",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "gpu.com",
										Count:           2,
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "claim2-obj", Namespace: "default"},
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{
								{
									Name: "req1",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "gpu.com",
										Count:           3,
									},
								},
							},
						},
					},
				},
			},
			expectedResreq: map[string]*schedulingapi.DRAResource{
				"gpu.com": {Count: 5},
			},
		},
		{
			name: "Mixed device classes",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod4", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimName: pointerString("claim1-obj")},
					},
				},
			},
			claims: []*resourcev1.ResourceClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "claim1-obj", Namespace: "default"},
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{
								{
									Name: "req1",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "gpu.com",
										Count:           1,
									},
								},
								{
									Name: "req2",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "nic.com",
										Count:           2,
									},
								},
							},
						},
					},
				},
			},
			expectedResreq: map[string]*schedulingapi.DRAResource{
				"gpu.com": {Count: 1},
				"nic.com": {Count: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
			claimInformer := informerFactory.Resource().V1().ResourceClaims()

			sc := &SchedulerCache{
				resourceClaimCache: assumecache.NewAssumeCache(klog.Background(), claimInformer.Informer(), "ResourceClaim", "", nil),
			}

			// Add claims to fake client
			if tt.claims != nil {
				for _, claim := range tt.claims {
					_, err := fakeClient.ResourceV1().ResourceClaims(claim.Namespace).Create(context.Background(), claim, metav1.CreateOptions{})
					assert.NoError(t, err)
					// Manually add to store to skip wait for informer sync
					// However, assumecache relies on informer store.
					// We can just start informer and wait.
				}
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			informerFactory.Start(ctx.Done())
			informerFactory.WaitForCacheSync(ctx.Done())

			// Wait for resourceClaimCache to populate
			err := wait.Poll(100*time.Millisecond, 2*time.Second, func() (bool, error) {
				for _, claim := range tt.claims {
					_, err := sc.resourceClaimCache.Get(claim.Namespace + "/" + claim.Name)
					if err != nil {
						return false, nil
					}
				}
				return true, nil
			})
			assert.NoError(t, err, "failed to wait for resource claim cache sync")

			resreq := sc.buildTaskDRAResreq(tt.pod)
			assert.Equal(t, tt.expectedResreq, resreq)
		})
	}
}

func pointerString(s string) *string {
	return &s
}
