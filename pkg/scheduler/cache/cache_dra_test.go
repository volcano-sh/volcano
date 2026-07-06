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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestBuildTaskDRAResreq(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, kubefeatures.DynamicResourceAllocation, true)
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, kubefeatures.DRAConsumableCapacity, true)

	tests := []struct {
		name           string
		pod            *v1.Pod
		claims         []*resourcev1.ResourceClaim
		expectedResreq map[string]*schedulingapi.DRAResource
		expectErr      bool
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
				"gpu.com": {Count: 1, Capacity: map[string]resource.Quantity{}},
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
				"gpu.com": {Count: 5, Capacity: map[string]resource.Quantity{}},
			},
		},
		{
			name: "Count multiplies consumable capacity",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-capacity", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimName: pointerString("claim-capacity")},
					},
				},
			},
			claims: []*resourcev1.ResourceClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "claim-capacity", Namespace: "default"},
					Spec: resourcev1.ResourceClaimSpec{
						Devices: resourcev1.DeviceClaim{
							Requests: []resourcev1.DeviceRequest{
								{
									Name: "req1",
									Exactly: &resourcev1.ExactDeviceRequest{
										DeviceClassName: "gpu.com",
										Count:           2,
										Capacity: &resourcev1.CapacityRequirements{
											Requests: map[resourcev1.QualifiedName]resource.Quantity{
												"memory": resource.MustParse("8Gi"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedResreq: map[string]*schedulingapi.DRAResource{
				"gpu.com": {
					Count: 2,
					Capacity: map[string]resource.Quantity{
						"memory": resource.MustParse("16Gi"),
					},
				},
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
				"gpu.com": {Count: 1, Capacity: map[string]resource.Quantity{}},
				"nic.com": {Count: 2, Capacity: map[string]resource.Quantity{}},
			},
		},
		{
			name: "Missing ResourceClaim returns error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-missing", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimName: pointerString("missing-claim")},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "Unresolved ResourceClaimTemplate returns pending error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-template", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "claim1", ResourceClaimTemplateName: pointerString("claim-template")},
					},
				},
			},
			expectErr: true,
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

			resreq, claimResreq, claimKeys, err := sc.buildTaskDRAInfo(tt.pod)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, resreq)
				assert.Nil(t, claimResreq)
				assert.Nil(t, claimKeys)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResreq, resreq)
			if tt.expectedResreq == nil {
				assert.Nil(t, claimResreq)
				assert.Nil(t, claimKeys)
			}
		})
	}
}

func TestAddPodWithUnresolvedResourceClaimTemplateCachesTaskForResync(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, kubefeatures.DynamicResourceAllocation, true)

	sc := newMockSchedulerCache("volcano")
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-template",
			Namespace: "default",
			UID:       types.UID("pod-template-uid"),
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: "pg-template",
			},
		},
		Spec: v1.PodSpec{
			SchedulerName: "volcano",
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "claim1", ResourceClaimTemplateName: pointerString("claim-template")},
			},
		},
	}

	err := sc.addPod(pod)
	assert.NoError(t, err)

	job, found := sc.Jobs[schedulingapi.JobID("default/pg-template")]
	assert.True(t, found)
	if assert.NotNil(t, job) {
		_, found = job.Tasks[schedulingapi.TaskID("pod-template-uid")]
		assert.True(t, found)
	}
	assert.Equal(t, 1, sc.errTasks.Len())
}

func pointerString(s string) *string {
	return &s
}

// TestAddDRAResourceNilCapacityOnly does not panic on a single call with
// capacity=nil, since `for range nil map` is safe in Go.
func TestAddDRAResourceNilCapacityOnly(t *testing.T) {
	dst := make(map[string]*schedulingapi.DRAResource)
	assert.NotPanics(t, func() {
		addDRAResource(dst, "gpu.example.com", 5, nil)
	})
	if got := dst["gpu.example.com"]; got == nil || got.Count != 5 {
		t.Fatalf("expected Count=5, got %+v", got)
	}
}

// TestAddDRAResourceNilCapacityThenNonEmpty verifies that addDRAResource does not
// panic when the same deviceClass is first added with capacity=nil  and then
// added again with a non-empty capacity map.
func TestAddDRAResourceNilCapacityThenNonEmpty(t *testing.T) {
	dst := make(map[string]*schedulingapi.DRAResource)

	// Same deviceClass and set capacity nil
	addDRAResource(dst, "gpu.example.com", 2, nil)

	first, ok := dst["gpu.example.com"]
	if !ok {
		t.Fatalf("expected entry for gpu.example.com after first call")
	}
	if first.Count != 2 {
		t.Fatalf("expected Count=2 after first call, got %d", first.Count)
	}

	// Same deviceClass and set capacity 4Gi
	memQty := resource.MustParse("4Gi")
	secondCapacity := map[string]resource.Quantity{"memory": memQty}

	assert.NotPanics(t, func() {
		addDRAResource(dst, "gpu.example.com", 1, secondCapacity)
	}, "addDRAResource must not panic when same deviceClass is added with nil capacity then non-nil capacity")

	got, ok := dst["gpu.example.com"]
	if !ok {
		t.Fatalf("expected entry for gpu.example.com after second call")
	}
	if got.Count != 3 {
		t.Errorf("expected Count=3 (2+1), got %d", got.Count)
	}
	// After both calls, Capacity must be non-nil and hold the second call's value.
	if got.Capacity == nil {
		t.Fatalf("expected Capacity map to be initialized after non-nil capacity call, got nil")
	}
	mem, ok := got.Capacity["memory"]
	if !ok {
		t.Fatalf("expected Capacity[\"memory\"] entry, got %v", got.Capacity)
	}
	if mem.Cmp(memQty) != 0 {
		t.Errorf("expected memory=%s, got %s", memQty.String(), mem.String())
	}
}

// TestBuildTaskDRAInfoMixedCapacityForSameDeviceClass verifies that the real
// call path through buildTaskDRAInfo does not panic when a single
// ResourceClaim has two DeviceRequests using the same deviceClass but with
// different capacity settings (only one of them declares capacity).
func TestBuildTaskDRAInfoMixedCapacityForSameDeviceClass(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, kubefeatures.DynamicResourceAllocation, true)
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, kubefeatures.DRAConsumableCapacity, true)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-mixed", Namespace: "default"},
		Spec: v1.PodSpec{
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "claim1", ResourceClaimName: pointerString("claim1-obj")},
			},
		},
	}

	// First request had no capacity
	// Second request had capacityy
	claims := []*resourcev1.ResourceClaim{
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
						{
							Name: "req2",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: "gpu.com",
								Count:           1,
								Capacity: &resourcev1.CapacityRequirements{
									Requests: map[resourcev1.QualifiedName]resource.Quantity{
										"memory": resource.MustParse("4Gi"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	claimInformer := informerFactory.Resource().V1().ResourceClaims()

	sc := &SchedulerCache{
		resourceClaimCache: assumecache.NewAssumeCache(klog.Background(), claimInformer.Informer(), "ResourceClaim", "", nil),
	}
	for _, claim := range claims {
		_, err := fakeClient.ResourceV1().ResourceClaims(claim.Namespace).Create(context.Background(), claim, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	err := wait.Poll(100*time.Millisecond, 2*time.Second, func() (bool, error) {
		for _, claim := range claims {
			if _, err := sc.resourceClaimCache.Get(claim.Namespace + "/" + claim.Name); err != nil {
				return false, nil
			}
		}
		return true, nil
	})
	assert.NoError(t, err, "failed to wait for resource claim cache sync")

	var resreq map[string]*schedulingapi.DRAResource
	assert.NotPanics(t, func() {
		resreq, _, _, _ = sc.buildTaskDRAInfo(pod)
	}, "buildTaskDRAInfo must not panic when same deviceClass has mixed capacity declarations")

	if resreq == nil {
		t.Fatalf("expected resreq to be non-nil")
	}
	got, ok := resreq["gpu.com"]
	if !ok {
		t.Fatalf("expected resreq[\"gpu.com\"], got %+v", resreq)
	}
	if got.Count != 3 {
		t.Errorf("expected Count=3 (2+1), got %d", got.Count)
	}
	if got.Capacity == nil {
		t.Fatalf("expected Capacity map to be initialized, got nil")
	}
	mem, ok := got.Capacity["memory"]
	if !ok {
		t.Fatalf("expected Capacity[\"memory\"] entry, got %v", got.Capacity)
	}
	want := resource.MustParse("4Gi")
	if mem.Cmp(want) != 0 {
		t.Errorf("expected memory=%s, got %s", want.String(), mem.String())
	}
}
