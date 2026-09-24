/*
Copyright 2025 The Volcano Authors.

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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/kubelet/types"
	"volcano.sh/volcano/pkg/agent/utils"
)

func TestEvictPod_V1(t *testing.T) {
	client := fakeclient.NewSimpleClientset()
	var received *policyv1.Eviction

	client.Fake.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		ev := action.(k8stesting.CreateAction).GetObject().(*policyv1.Eviction)
		received = ev
		return true, ev, nil
	})

	grace := int64(-10)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "myns"}}
	if err := evictPod(context.Background(), client, &grace, pod, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if grace != 0 {
		t.Errorf("grace not clamped, got %d want 0", grace)
	}
	if received == nil || received.Name != "mypod" || received.Namespace != "myns" {
		t.Errorf("unexpected eviction: %+v", received)
	}
}

func TestEvictPod_V1Beta1(t *testing.T) {
	client := fakeclient.NewSimpleClientset()
	var received *policyv1beta1.Eviction

	client.Fake.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		ev := action.(k8stesting.CreateAction).GetObject().(*policyv1beta1.Eviction)
		received = ev
		return true, ev, nil
	})

	grace := int64(15)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "ns2"}}
	if err := evictPod(context.Background(), client, &grace, pod, "v1beta1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received == nil || received.Name != "pod2" || received.Namespace != "ns2" {
		t.Errorf("unexpected eviction: %+v", received)
	}
}

func TestEvictPod_UnsupportedVersion(t *testing.T) {
	client := fakeclient.NewSimpleClientset()
	grace := int64(5)
	err := evictPod(context.Background(), client, &grace, &corev1.Pod{}, "bogus")
	if err == nil || err.Error() != "unsupported eviction version: bogus" {
		t.Errorf("expected unsupported-version error, got %v", err)
	}
}

func makePod(name string, ann map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: ann,
		},
	}
}

func TestEviction_Evict_CriticalPod(t *testing.T) {
	e := &eviction{
		kubeClient: fakeclient.NewSimpleClientset(),
		killPodFunc: func(context.Context, clientset.Interface, *int64, *corev1.Pod, string) error {
			t.Fatal("should not call killPodFunc")
			return nil
		},
	}

	rec := record.NewFakeRecorder(1)
	ok := e.Evict(context.Background(), makePod("crit", map[string]string{
		types.ConfigMirrorAnnotationKey: "true",
	}), rec, 10, "msg")
	if ok {
		t.Error("expected Evict=false for critical pod")
	}
}

func TestEviction_Evict_Success(t *testing.T) {
	called := false
	kpf := func(_ context.Context, _ clientset.Interface, gp *int64, _ *corev1.Pod, _ string) error {
		called = true
		*gp = 999
		return nil
	}
	e := &eviction{
		kubeClient:      fakeclient.NewSimpleClientset(),
		killPodFunc:     kpf,
		evictionVersion: "v1",
	}

	rec := record.NewFakeRecorder(1)
	ok := e.Evict(context.Background(), makePod("p", nil), rec, 7, "reason")
	if !ok {
		t.Fatal("expected Evict=true")
	}
	if !called {
		t.Fatal("killPodFunc not called")
	}
	select {
	case ev := <-rec.Events:
		want := fmt.Sprintf("Warning %s reason", Reason)
		if ev != want {
			t.Errorf("got event %q, want %q", ev, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestEviction_Evict_Failure(t *testing.T) {
	kpf := func(_ context.Context, _ clientset.Interface, _ *int64, _ *corev1.Pod, _ string) error {
		return errors.New("fail")
	}
	e := &eviction{
		kubeClient:      fakeclient.NewSimpleClientset(),
		killPodFunc:     kpf,
		evictionVersion: "v1beta1",
	}

	rec := record.NewFakeRecorder(1)
	ok := e.Evict(context.Background(), makePod("p", nil), rec, 8, "fail")
	if ok {
		t.Fatal("expected Evict=false on failure")
	}
	select {
	case ev := <-rec.Events:
		t.Fatalf("unexpected event: %q", ev)
	default:
	}
}

func TestNewEviction_SetsVersion(t *testing.T) {
	fc := fakeclient.NewSimpleClientset()
	want, _ := utils.GetEvictionVersion(fc)

	iface := NewEviction(fc, "nodeX")
	ev, ok := iface.(*eviction)
	if !ok {
		t.Fatal("NewEviction returned wrong type")
	}
	if ev.evictionVersion != want {
		t.Errorf("version %q != want %q", ev.evictionVersion, want)
	}
}
