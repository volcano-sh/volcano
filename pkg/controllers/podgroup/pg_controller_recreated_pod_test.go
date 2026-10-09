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

package podgroup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// The fake clientset applies metadata.uid from a patch instead of rejecting it, so emulate the apiserver's immutable-uid check.
func newRecreatedPodController() *pgcontroller {
	c := newFakeController()
	client := c.kubeClient.(*kubeclient.Clientset)
	client.PrependReactor("patch", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(k8stesting.PatchAction)
		var patched metav1.PartialObjectMetadata
		if err := json.Unmarshal(patchAction.GetPatch(), &patched); err != nil {
			return true, nil, err
		}
		if patched.UID == "" {
			return false, nil, nil
		}
		obj, err := client.Tracker().Get(patchAction.GetResource(), patchAction.GetNamespace(), patchAction.GetName())
		if err != nil {
			return true, nil, err
		}
		if obj.(metav1.Object).GetUID() != patched.UID {
			return true, nil, apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, patchAction.GetName(), field.ErrorList{
				field.Invalid(field.NewPath("metadata", "uid"), patched.UID, validation.FieldImmutableErrorMsg),
			})
		}
		return false, nil, nil
	})
	return c
}

func stsPod(uid, ownerUID types.UID) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sts-0",
			Namespace: "test",
			UID:       uid,
			Labels: map[string]string{
				"app":                          "sts",
				controllerRevisionHashLabelKey: "rev-1",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       "sts",
				UID:        ownerUID,
				Controller: ptr.To(true),
			}},
		},
		Spec: v1.PodSpec{
			SchedulerName: "volcano",
		},
	}
}

func assertBoundTo(t *testing.T, c *pgcontroller, ownerUID types.UID) {
	t.Helper()
	pod, err := c.kubeClient.CoreV1().Pods("test").Get(context.TODO(), "sts-0", metav1.GetOptions{})
	assert.NoError(t, err)
	pgName := vcbatch.PodgroupNamePrefix + string(ownerUID)
	assert.Equal(t, pgName, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
	pg, err := c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.TODO(), pgName, metav1.GetOptions{})
	if assert.NoError(t, err) {
		assert.Equal(t, ownerUID, metav1.GetControllerOf(pg).UID)
	}
}

func TestProcessNextReqWithRecreatedPod(t *testing.T) {
	oldPod := stsPod("old-pod", "old-sts")
	newPod := stsPod("new-pod", "new-sts")
	req := podRequest{podName: oldPod.Name, podNamespace: oldPod.Namespace, podUID: oldPod.UID}

	testCases := []struct {
		name          string
		cached        *v1.Pod   // old pod
		stored        *v1.Pod   // new pod
		patchErr      error     // throw error on the first patch action in retry scenario
		retry         bool      // whether need to check the logic of retry
		wantOwner     types.UID // owner podgroup of the old pod
		wantPodGroups int
		wantRequeues  int
	}{
		{
			name:          "uid matches",
			cached:        oldPod,
			stored:        oldPod,
			wantOwner:     "old-sts",
			wantPodGroups: 1,
		},
		{
			name:   "pod recreated and cache updated",
			cached: newPod,
			stored: newPod,
		},
		{
			name:          "pod recreated and cache stale",
			cached:        oldPod,
			stored:        newPod,
			wantPodGroups: 1,
			wantRequeues:  1,
		},
		{
			name:          "patch fails once",
			cached:        oldPod,
			stored:        oldPod,
			patchErr:      errors.New("patch failed"),
			retry:         true,
			wantOwner:     "old-sts",
			wantPodGroups: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRecreatedPodController()
			// The informer cache and the fake clientset are separate stores, so the cache can lag behind the "apiserver".
			assert.NoError(t, c.podInformer.Informer().GetIndexer().Add(tc.cached.DeepCopy()))
			_, err := c.kubeClient.CoreV1().Pods(tc.stored.Namespace).Create(context.TODO(), tc.stored.DeepCopy(), metav1.CreateOptions{})
			assert.NoError(t, err)
			if tc.patchErr != nil {
				failed := false
				c.kubeClient.(*kubeclient.Clientset).PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
					if failed {
						return false, nil, nil
					}
					failed = true
					return true, nil, tc.patchErr
				})
			}

			c.addPod(oldPod)
			c.processNextReq()
			if tc.retry {
				if !assert.Equal(t, 1, c.queue.NumRequeues(req)) {
					return
				}
				c.processNextReq()
			}

			assert.Equal(t, tc.wantRequeues, c.queue.NumRequeues(req))
			if tc.wantOwner == "" {
				pod, err := c.kubeClient.CoreV1().Pods(tc.stored.Namespace).Get(context.TODO(), tc.stored.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Empty(t, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
			} else {
				assertBoundTo(t, c, tc.wantOwner)
			}
			pgList, err := c.vcClient.SchedulingV1beta1().PodGroups(tc.stored.Namespace).List(context.TODO(), metav1.ListOptions{})
			assert.NoError(t, err)
			assert.Len(t, pgList.Items, tc.wantPodGroups)
		})
	}
}

func TestRecreatedPodIsBoundToNewOwnerPodGroup(t *testing.T) {
	testCases := []struct {
		name          string
		newOwner      types.UID
		wantPodGroups int
	}{
		{
			name:          "statefulset recreated",
			newOwner:      "new-sts",
			wantPodGroups: 2,
		},
		{
			name:          "pod recreated under the same statefulset",
			newOwner:      "old-sts",
			wantPodGroups: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRecreatedPodController()
			oldPod := stsPod("old-pod", "old-sts")
			newPod := stsPod("new-pod", tc.newOwner)
			oldReq := podRequest{podName: oldPod.Name, podNamespace: oldPod.Namespace, podUID: oldPod.UID}

			assert.NoError(t, c.podInformer.Informer().GetIndexer().Add(oldPod.DeepCopy()))
			_, err := c.kubeClient.CoreV1().Pods(newPod.Namespace).Create(context.TODO(), newPod.DeepCopy(), metav1.CreateOptions{})
			assert.NoError(t, err)

			c.addPod(oldPod)
			c.processNextReq()
			if !assert.Equal(t, 1, c.queue.NumRequeues(oldReq)) {
				return
			}
			pod, err := c.kubeClient.CoreV1().Pods(newPod.Namespace).Get(context.TODO(), newPod.Name, metav1.GetOptions{})
			assert.NoError(t, err)
			assert.Empty(t, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])

			assert.NoError(t, c.podInformer.Informer().GetIndexer().Add(newPod.DeepCopy()))
			c.processNextReq()
			assert.Equal(t, 1, c.queue.NumRequeues(oldReq))

			c.addPod(newPod)
			c.processNextReq()
			assertBoundTo(t, c, tc.newOwner)
			pgList, err := c.vcClient.SchedulingV1beta1().PodGroups(newPod.Namespace).List(context.TODO(), metav1.ListOptions{})
			assert.NoError(t, err)
			assert.Len(t, pgList.Items, tc.wantPodGroups)
		})
	}
}

func TestAddStatefulSetWithRecreatedPod(t *testing.T) {
	c := newRecreatedPodController()
	oldPod := stsPod("old-pod", "old-sts")
	newPod := stsPod("new-pod", "new-sts")
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sts",
			Namespace: "test",
			UID:       "new-sts",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "sts"},
			},
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "rev-1",
		},
	}

	assert.NoError(t, c.podInformer.Informer().GetIndexer().Add(oldPod.DeepCopy()))
	_, err := c.kubeClient.CoreV1().Pods(newPod.Namespace).Create(context.TODO(), newPod.DeepCopy(), metav1.CreateOptions{})
	assert.NoError(t, err)

	c.addStatefulSet(sts)
	pod, err := c.kubeClient.CoreV1().Pods(newPod.Namespace).Get(context.TODO(), newPod.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Empty(t, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])

	assert.NoError(t, c.podInformer.Informer().GetIndexer().Add(newPod.DeepCopy()))
	c.addPod(newPod)
	c.processNextReq()
	assertBoundTo(t, c, "new-sts")
}
