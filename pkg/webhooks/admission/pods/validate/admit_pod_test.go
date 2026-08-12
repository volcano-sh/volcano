/*
Copyright 2019 The Volcano Authors.

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

package validate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

func TestValidatePod(t *testing.T) {

	namespace := "test"

	testCases := []struct {
		Name           string
		Pod            v1.Pod
		ExpectErr      bool
		reviewResponse admissionv1.AdmissionResponse
		ret            string
		disabledPG     bool
		queueName      string
		queueState     vcschedulingv1.QueueState
	}{
		// validate normal pod with default-scheduler
		{
			Name: "validate default normal pod",
			Pod: v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "normal-pod-1",
				},
				Spec: v1.PodSpec{
					SchedulerName: "default-scheduler",
				},
			},

			reviewResponse: admissionv1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
	}

	for _, testCase := range testCases {

		pg := &vcschedulingv1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "podgroup-p1",
			},
			Spec: vcschedulingv1.PodGroupSpec{
				MinMember: 1,
				Queue:     testCase.queueName,
			},
			Status: vcschedulingv1.PodGroupStatus{
				Phase: vcschedulingv1.PodGroupPending,
			},
		}
		queue := vcschedulingv1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: testCase.queueName,
			},
			Spec: vcschedulingv1.QueueSpec{
				Weight: 1,
			},
			Status: vcschedulingv1.QueueStatus{
				State: testCase.queueState,
			},
		}

		// create fake volcano clientset
		config.VolcanoClient = vcclient.NewSimpleClientset()
		config.SchedulerNames = []string{"volcano"}

		if !testCase.disabledPG {
			_, err := config.VolcanoClient.SchedulingV1beta1().PodGroups(namespace).Create(context.TODO(), pg, metav1.CreateOptions{})
			if err != nil {
				t.Error("PG Creation Failed")
			}
		}

		if testCase.queueName != "" && testCase.queueState != "" {
			//create default queue
			_, err := config.VolcanoClient.SchedulingV1beta1().Queues().Create(context.TODO(), &queue, metav1.CreateOptions{})
			if err != nil {
				t.Error("Queue Creation Failed")
			}
		}

		ret := validatePod(&testCase.Pod, &testCase.reviewResponse)

		if testCase.ExpectErr == true && ret == "" {
			t.Errorf("%s: test case Expect error msg :%s, but got nil.", testCase.Name, testCase.ret)
		}
		if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
			t.Errorf("%s: test case Expect Allowed as false but got true.", testCase.Name)
		}
		if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
			t.Errorf("%s: test case Expect error msg :%s, but got diff error %v", testCase.Name, testCase.ret, ret)
		}

		if testCase.ExpectErr == false && ret != "" {
			t.Errorf("%s: test case Expect no error, but got error %v", testCase.Name, ret)
		}
		if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
			t.Errorf("%s: test case Expect Allowed as true but got false. %v", testCase.Name, testCase.reviewResponse)
		}
	}
}

func TestAdmitPodsRejectsNamespaceQueueAnnotationUpdate(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	oldSchedulerNames := config.SchedulerNames
	oldQueueLister := config.QueueLister
	oldNamespaceQueueLister := config.NamespaceQueueLister
	defer func() {
		config.SchedulerNames = oldSchedulerNames
		config.QueueLister = oldQueueLister
		config.NamespaceQueueLister = oldNamespaceQueueLister
	}()

	config.SchedulerNames = []string{"volcano"}
	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	config.QueueLister = schedulinglister.NewQueueLister(queueIndexer)
	namespaceQueueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(namespaceQueueIndexer)
	if err := namespaceQueueIndexer.Add(&vcschedulingv1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "training",
			Namespace:  "team-a",
			Generation: 1,
		},
		Status: vcschedulingv1.NamespaceQueueStatus{
			State: vcschedulingv1.QueueStateOpen,
			Conditions: []metav1.Condition{
				{Type: "Authorized", Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: "Ready", Status: metav1.ConditionFalse, ObservedGeneration: 1},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	oldPod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker",
			Namespace: "team-a",
			Annotations: map[string]string{
				vcschedulingv1.QueueNameAnnotationKey: "default",
			},
		},
		Spec: v1.PodSpec{SchedulerName: "volcano"},
	}
	newPod := oldPod.DeepCopy()
	newPod.Annotations[vcschedulingv1.QueueNameAnnotationKey] = "namespace/training"
	oldRaw, err := json.Marshal(oldPod)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, err := json.Marshal(newPod)
	if err != nil {
		t.Fatal(err)
	}

	response := AdmitPods(admissionv1.AdmissionReview{Request: &admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		Object:    runtime.RawExtension{Raw: newRaw}, OldObject: runtime.RawExtension{Raw: oldRaw},
	}})
	if response.Allowed || response.Result == nil ||
		!strings.Contains(response.Result.Message, "is not ready") {
		t.Fatalf("unexpected response: %#v", response)
	}
}
