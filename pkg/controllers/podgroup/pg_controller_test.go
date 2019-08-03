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

package podgroup

import (
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes/fake"

	scheduling "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	kubebatchclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"
)

func newFakeController() *Controller {
	KubeClientSet := kubeclient.NewSimpleClientset()
	KubeBatchClientSet := kubebatchclient.NewSimpleClientset()
	sharedInformers := informers.NewSharedInformerFactory(KubeClientSet, 0)

	controller := NewPodgroupController(KubeClientSet, KubeBatchClientSet, sharedInformers, "volcano")
	return controller
}

func TestAddPodgroup(t *testing.T) {
	namespace := "test"
	isController := true

	testCases := []struct {
		Name        string
		pod         *v1.Pod
		ExpectValue string
	}{
		{
			Name: "AddPodgroup- pod has ownerReferences",
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{UID: "p1", Controller: &isController},
					},
				},
			},
			ExpectValue: "podgroup-p1",
		},
		{
			Name: "AddPodgroup- pod has no ownerReferences",
			pod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					UID:       types.UID("p1-uid"),
				},
			},
			ExpectValue: "podgroup-p1-uid",
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()

		pod, _ := c.kubeClients.CoreV1().Pods(testcase.pod.Namespace).Create(testcase.pod)

		c.addPod(pod)
		c.createNormalPodPGIfNotExist(pod)

		podAnno := pod.Annotations[scheduling.GroupNameAnnotationKey]
		if testcase.ExpectValue != podAnno {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, podAnno)
		}
	}
}
