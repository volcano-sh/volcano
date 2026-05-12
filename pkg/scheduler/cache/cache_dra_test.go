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
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"

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
				"gpu.com": {Count: 1},
				"nic.com": {Count: 2},
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
			name: "Unresolved ResourceClaimTemplate returns error",
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

func pointerString(s string) *string {
	return &s
}
