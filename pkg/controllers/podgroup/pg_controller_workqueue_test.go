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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
)

func TestWorkloadQueueReadsCurrentReplicas(t *testing.T) {
	for _, kind := range []requestKind{replicaSetKind, statefulSetKind} {
		t.Run(string(kind), func(t *testing.T) {
			c := newFakeController()
			uid := types.UID("owner-uid")
			name := batchv1alpha1.PodgroupNamePrefix + string(uid)
			owner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: string(kind), Name: "owner", UID: uid, Controller: ptr.To(true)}
			_, err := c.vcClient.SchedulingV1beta1().PodGroups("test").Create(context.Background(), &scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: "pg-uid", OwnerReferences: []metav1.OwnerReference{owner}},
			}, metav1.CreateOptions{})
			require.NoError(t, err)

			if kind == replicaSetKind {
				current := &appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "test", UID: uid},
					Spec:       appsv1.ReplicaSetSpec{Replicas: ptr.To[int32](1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "owner"}}},
				}
				require.NoError(t, c.rsInformer.Informer().GetIndexer().Add(current))
				stale := current.DeepCopy()
				stale.Spec.Replicas = ptr.To[int32](0)
				c.addReplicaSet(stale)
				require.True(t, c.processNextReq())
				_, err = c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.Background(), name, metav1.GetOptions{})
				require.NoError(t, err, "stale event deleted the current PodGroup")
				zero := current.DeepCopy()
				zero.Spec.Replicas = ptr.To[int32](0)
				require.NoError(t, c.rsInformer.Informer().GetIndexer().Update(zero))
				c.updateReplicaSet(current, zero)
			} else {
				current := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "test", UID: uid},
					Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "owner"}}},
				}
				require.NoError(t, c.stsInformer.Informer().GetIndexer().Add(current))
				stale := current.DeepCopy()
				stale.Spec.Replicas = ptr.To[int32](0)
				c.addStatefulSet(stale)
				require.True(t, c.processNextReq())
				_, err = c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.Background(), name, metav1.GetOptions{})
				require.NoError(t, err, "stale event deleted the current PodGroup")
				zero := current.DeepCopy()
				zero.Spec.Replicas = ptr.To[int32](0)
				require.NoError(t, c.stsInformer.Informer().GetIndexer().Update(zero))
				c.updateStatefulSet(current, zero)
			}
			require.True(t, c.processNextReq())
			_, err = c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.Background(), name, metav1.GetOptions{})
			require.True(t, apierrors.IsNotFound(err), "current zero replicas must remove the owned PodGroup")
		})
	}
}

func TestOwnedPodAndStatefulSetShareQueueKey(t *testing.T) {
	c := newFakeController()
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "test", UID: "owner-uid"},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "owner"}}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "owner-0", Namespace: "test", UID: "pod-uid",
			Labels: map[string]string{"app": "owner"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true),
			}},
			Annotations: map[string]string{scheduling.KubeGroupNameAnnotationKey: batchv1alpha1.PodgroupNamePrefix + string(sts.UID)},
		},
		Spec: corev1.PodSpec{SchedulerName: "volcano"},
	}
	_, err := c.kubeClient.AppsV1().StatefulSets("test").Create(context.Background(), sts, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubeClient.CoreV1().Pods("test").Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.stsInformer.Informer().GetIndexer().Add(sts))
	require.NoError(t, c.podInformer.Informer().GetIndexer().Add(pod))

	c.addStatefulSet(sts)
	c.addPod(pod)
	require.Equal(t, 1, c.queue.Len(), "the same owner must have one queue key")
	require.True(t, c.processNextReq())
	group, err := c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.Background(),
		batchv1alpha1.PodgroupNamePrefix+string(sts.UID), metav1.GetOptions{})
	require.NoError(t, err, "a retained automatic annotation must not block PodGroup recovery")
	require.Equal(t, sts.UID, metav1.GetControllerOf(group).UID)

	// A user-selected PodGroup must remain outside this controller's write path.
	external := pod.DeepCopy()
	external.Annotations[scheduling.KubeGroupNameAnnotationKey] = "external-group"
	require.NoError(t, c.podInformer.Informer().GetIndexer().Update(external))
	require.NoError(t, c.vcClient.SchedulingV1beta1().PodGroups("test").Delete(context.Background(), group.Name, metav1.DeleteOptions{}))
	c.addPod(external)
	require.True(t, c.processNextReq())
	_, err = c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.Background(), group.Name, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
}

func TestOwnedPodEventDuringPodGroupCreate(t *testing.T) {
	c := newFakeController()
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "test", UID: "owner-uid"},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "owner"}}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "owner-0", Namespace: "test", UID: "pod-uid", Labels: map[string]string{"app": "owner"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{SchedulerName: "volcano"},
	}
	_, err := c.kubeClient.AppsV1().StatefulSets("test").Create(context.Background(), sts, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = c.kubeClient.CoreV1().Pods("test").Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.stsInformer.Informer().GetIndexer().Add(sts))
	require.NoError(t, c.podInformer.Informer().GetIndexer().Add(pod))

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c.vcClient.(*vcfake.Clientset).PrependReactor("create", "podgroups", func(k8stesting.Action) (bool, runtime.Object, error) {
		entered <- struct{}{}
		<-release
		return false, nil, nil
	})
	c.addStatefulSet(sts)
	done := make(chan struct{})
	go func() {
		c.processNextReq()
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("PodGroup create was not reached")
	}
	c.addPod(pod)
	c.updateStatefulSet(sts, sts)
	queuedWhileCreate := c.queue.Len()
	close(release)
	<-done
	require.Equal(t, 0, queuedWhileCreate, "the owner key stays in process during Pod Add")
	require.True(t, c.processNextReq()) // The dirty owner key is reconciled once more.
	creates := 0
	for _, action := range c.vcClient.(*vcfake.Clientset).Actions() {
		if action.Matches("create", "podgroups") {
			creates++
		}
	}
	require.Equal(t, 1, creates)
}
