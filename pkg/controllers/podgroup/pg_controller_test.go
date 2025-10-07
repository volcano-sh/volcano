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
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
	controllerutil "volcano.sh/volcano/pkg/controllers/util"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func newFakeController() *pgcontroller {
	kubeClient := kubeclient.NewSimpleClientset()
	vcClient := vcclient.NewSimpleClientset()
	sharedInformers := informers.NewSharedInformerFactory(kubeClient, 0)
	vcSharedInformers := informerfactory.NewSharedInformerFactory(vcClient, 0)

	controller := &pgcontroller{}
	opt := &framework.ControllerOption{
		KubeClient:              kubeClient,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   sharedInformers,
		VCSharedInformerFactory: vcSharedInformers,
		SchedulerNames:          []string{"volcano"},
		InheritOwnerAnnotations: true,
	}

	controller.Initialize(opt)

	return controller
}

func TestAddPodGroup(t *testing.T) {
	namespace := "test"
	isController := true
	blockOwnerDeletion := true
	replicas := int32(2)
	gpuKey := v1.ResourceName("nvidia.com/gpu")

	testCases := []struct {
		name             string
		rs               *appsv1.ReplicaSet
		pods             []*v1.Pod
		expectedPodGroup *scheduling.PodGroup
	}{
		{
			name: "AddPodGroup: pod has ownerReferences and priorityClassName",
			pods: []*v1.Pod{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "app/v1",
								Kind:       "ReplicaSet",
								Name:       "rs1",
								UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
								Controller: &isController,
							},
						},
					},
					Spec: v1.PodSpec{
						PriorityClassName: "test-pc",
					},
				},
			},
			expectedPodGroup: &scheduling.PodGroup{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.volcano.sh/v1beta1",
					Kind:       "PodGroup",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "app/v1",
							Kind:       "ReplicaSet",
							Name:       "rs1",
							UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
							Controller: &isController,
						},
					},
				},
				Spec: scheduling.PodGroupSpec{
					MinMember:         1,
					PriorityClassName: "test-pc",
				},
			},
		},
		{
			name: "AddPodGroup: pod has no ownerReferences or priorityClassName",
			pods: []*v1.Pod{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						UID:       types.UID("7a09885b-b753-4924-9fba-77c0836bac20"),
					},
				},
			},
			expectedPodGroup: &scheduling.PodGroup{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.volcano.sh/v1beta1",
					Kind:       "PodGroup",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1",
							Kind:               "Pod",
							Name:               "pod1",
							UID:                "7a09885b-b753-4924-9fba-77c0836bac20",
							Controller:         &isController,
							BlockOwnerDeletion: &blockOwnerDeletion,
						},
					},
				},
				Spec: scheduling.PodGroupSpec{
					MinMember: 1,
				},
			},
		},
		{
			name: "AddPodGroup: pod owners with group-min-member annotation",
			rs: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rs1",
					Namespace: namespace,
					UID:       "7a09885b-b753-4924-9fba-77c0836bac20",
					Annotations: map[string]string{
						scheduling.VolcanoGroupMinMemberAnnotationKey: "2",
					},
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ReplicaSet",
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "rs1",
						},
					},
					Replicas: &replicas,
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "container1",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											gpuKey: resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			pods: []*v1.Pod{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Labels: map[string]string{
							"app": "rs1",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "app/v1",
								Kind:       "ReplicaSet",
								Name:       "rs1",
								UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
								Controller: &isController,
							},
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										gpuKey: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: namespace,
						Labels: map[string]string{
							"app": "rs1",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "app/v1",
								Kind:       "ReplicaSet",
								Name:       "rs1",
								UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
								Controller: &isController,
							},
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										gpuKey: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expectedPodGroup: &scheduling.PodGroup{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.volcano.sh/v1beta1",
					Kind:       "PodGroup",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "app/v1",
							Kind:       "ReplicaSet",
							Name:       "rs1",
							UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
							Controller: &isController,
						},
					},
				},
				Spec: scheduling.PodGroupSpec{
					MinMember: 2,
					MinResources: &v1.ResourceList{
						gpuKey: resource.MustParse("2"),
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		c := newFakeController()

		if testCase.rs != nil {
			rs, err := c.kubeClient.AppsV1().ReplicaSets(namespace).Create(context.TODO(), testCase.rs, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Case %s failed when creating replicaSet for %v", testCase.name, err)
			}

			c.addReplicaSet(rs)
		}

		for i := range testCase.pods {
			pod, err := c.kubeClient.CoreV1().Pods(testCase.pods[i].Namespace).Create(context.TODO(), testCase.pods[i], metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Case %s failed when creating pod for %v", testCase.name, err)
			}

			c.addPod(pod)
			c.createNormalPodPGIfNotExist(pod)

			pg, err := c.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Get(context.TODO(),
				testCase.expectedPodGroup.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Errorf("Case %s failed when getting podGroup for %v", testCase.name, err)
			}

			if false == equality.Semantic.DeepEqual(pg.OwnerReferences, testCase.expectedPodGroup.OwnerReferences) {
				t.Errorf("Case %s failed, expect %v, got %v", testCase.name, testCase.expectedPodGroup, pg)
			}

			newpod, err := c.kubeClient.CoreV1().Pods(testCase.pods[i].Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Case %s failed when creating pod for %v", testCase.name, err)
			}

			podAnnotation := newpod.Annotations[scheduling.KubeGroupNameAnnotationKey]
			if testCase.expectedPodGroup.Name != podAnnotation {
				t.Errorf("Case %s failed, expect %v, got %v", testCase.name,
					testCase.expectedPodGroup.Name, podAnnotation)
			}

			if testCase.expectedPodGroup.Spec.PriorityClassName != pod.Spec.PriorityClassName {
				t.Errorf("Case %s failed, expect %v, got %v", testCase.name,
					testCase.expectedPodGroup.Spec.PriorityClassName, pod.Spec.PriorityClassName)
			}

			if pg.Spec.MinMember != testCase.expectedPodGroup.Spec.MinMember {
				t.Errorf("Case %s failed, expect %v, got %v", testCase.name, testCase.expectedPodGroup.Spec.MinMember, pg.Spec.MinMember)
			}

			if testCase.expectedPodGroup.Spec.MinResources != nil && false == equality.Semantic.DeepEqual(pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI), testCase.expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI)) {
				t.Errorf("Case %s failed, expect %v, got %v", testCase.name, testCase.expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI), pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI))
			}
		}
	}
}

func TestAddStatefulSet(t *testing.T) {
	namespace := "test"
	stsName := "sts-test"
	podName := "sts-test-0"
	defaultSchedulerPod := util.BuildPod(namespace, podName, "", v1.PodPending, api.BuildResourceList("1", "2Gi"), "", map[string]string{"app": stsName}, nil)
	defaultSchedulerPod.Spec.SchedulerName = "default-scheduler"
	volcanoSchedulerPod := util.BuildPod(namespace, podName, "", v1.PodPending, api.BuildResourceList("1", "2Gi"), "", map[string]string{"app": stsName, controllerRevisionHashLabelKey: "test"}, nil)
	volcanoSchedulerPod.Spec.SchedulerName = "volcano"
	existedPodWithPG := util.BuildPod(namespace, podName, "", v1.PodPending, api.BuildResourceList("1", "2Gi"), "lws-1-revision", map[string]string{"app": stsName}, nil)
	existedPodWithPG.Spec.SchedulerName = "volcano"

	testCases := []struct {
		name            string
		sts             *appsv1.StatefulSet
		existingPods    []*v1.Pod
		expectPGCreated bool
		expectPG        *scheduling.PodGroup
	}{
		{
			name: "StatefulSet with replicas > 0 and no existing pods",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": stsName},
					},
				},
			},
			existingPods:    nil,
			expectPGCreated: false,
		},
		{
			name: "StatefulSet with replicas > 0 and existing pod without scheduler name",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": stsName},
					},
				},
			},
			existingPods:    []*v1.Pod{defaultSchedulerPod},
			expectPGCreated: false,
		},
		{
			name: "StatefulSet with replicas > 0 and existing pod with volcano scheduler",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": stsName},
					},
				},
				Status: appsv1.StatefulSetStatus{
					UpdateRevision: "test",
				},
			},
			existingPods:    []*v1.Pod{volcanoSchedulerPod},
			expectPGCreated: true,
			expectPG: &scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vcbatch.PodgroupNamePrefix + fmt.Sprintf("%s-%s", namespace, podName),
					Namespace:       namespace,
					OwnerReferences: newPGOwnerReferences(volcanoSchedulerPod),
					Labels:          map[string]string{},
					Annotations:     map[string]string{},
				},
				Spec: scheduling.PodGroupSpec{
					MinMember:    1,
					MinResources: ptr.To(controllerutil.CalTaskRequests(volcanoSchedulerPod, 1)),
				},
				Status: scheduling.PodGroupStatus{
					Phase: scheduling.PodGroupPending,
				},
			},
		},
		{
			name: "StatefulSet with existing pod already associated with podgroup",
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": stsName},
					},
				},
			},
			existingPods:    []*v1.Pod{existedPodWithPG},
			expectPGCreated: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newFakeController()

			for _, pod := range testCase.existingPods {
				_, err := c.kubeClient.CoreV1().Pods("test").Create(context.TODO(), pod, metav1.CreateOptions{})
				assert.NoError(t, err)
				c.podInformer.Informer().GetIndexer().Add(pod)
			}

			c.addStatefulSet(testCase.sts)
			expectedPGName := vcbatch.PodgroupNamePrefix + fmt.Sprintf("%s-%s", namespace, podName)
			pg, err := c.vcClient.SchedulingV1beta1().PodGroups("test").Get(context.TODO(), expectedPGName, metav1.GetOptions{})
			if testCase.expectPGCreated {
				assert.NoError(t, err)
				assert.Equal(t, pg, testCase.expectPG)
			} else {
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func Test_createOrUpdateNormalPodPG(t *testing.T) {
	namespace := "test"
	replicas := int32(2)
	isController := true
	gpuKey := v1.ResourceName("nvidia.com/gpu")

	t.Run("Scenario 1: Create normal pod group", func(t *testing.T) {
		c := newFakeController()

		pod := &v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespace,
				Labels: map[string]string{
					"app": "sts1",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "app/v1",
						Kind:       "StatefulSet",
						Name:       "sts1",
						UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
						Controller: &isController,
					},
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "container1",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								gpuKey: resource.MustParse("1"),
							},
						},
					},
				},
			},
		}

		pod, err := c.kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Case 1 failed when creating pod for %v", err)
		}

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sts1",
				Namespace: namespace,
				UID:       "7a09885b-b753-4924-9fba-77c0836bac20",
				Annotations: map[string]string{
					scheduling.VolcanoGroupMinMemberAnnotationKey: "2",
				},
			},
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "StatefulSet",
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "sts1",
					},
				},
				Replicas: &replicas,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										gpuKey: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
		}

		sts, err = c.kubeClient.AppsV1().StatefulSets(namespace).Create(context.TODO(), sts, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Case 1 failed when creating sts for %v", err)
		}

		expectedPodGroup := &scheduling.PodGroup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "scheduling.volcano.sh/v1beta1",
				Kind:       "PodGroup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "app/v1",
						Kind:       "StatefulSet",
						Name:       "sts1",
						UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
						Controller: &isController,
					},
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 2,
				MinResources: &v1.ResourceList{
					gpuKey: resource.MustParse("2"),
				},
			},
		}

		c.vcInformerFactory.Scheduling().V1beta1().PodGroups().Informer()
		c.vcInformerFactory.Start(context.TODO().Done())
		cache.WaitForNamedCacheSync("", context.TODO().Done(), c.vcInformerFactory.Scheduling().V1beta1().PodGroups().Informer().HasSynced)

		c.createOrUpdateNormalPodPG(pod)
		pg, err := c.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Get(context.TODO(), "podgroup-7a09885b-b753-4924-9fba-77c0836bac20", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Case 1 failed when getting podGroup for %v", err)
		}

		if false == equality.Semantic.DeepEqual(pg.OwnerReferences, expectedPodGroup.OwnerReferences) {
			t.Errorf("Case 1 failed, expect %v, got %v", expectedPodGroup, pg)
		}

		if expectedPodGroup.Spec.PriorityClassName != pod.Spec.PriorityClassName {
			t.Errorf("Case 1 failed, expect %v, got %v",
				expectedPodGroup.Spec.PriorityClassName, pod.Spec.PriorityClassName)
		}

		if pg.Spec.MinMember != expectedPodGroup.Spec.MinMember {
			t.Errorf("Case 1 failed, expect %v, got %v", expectedPodGroup.Spec.MinMember, pg.Spec.MinMember)
		}

		if expectedPodGroup.Spec.MinResources != nil && false == equality.Semantic.DeepEqual(pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI), expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI)) {
			t.Errorf("Case 1 failed, expect %v, got %v", expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI), pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI))
		}
	})

	t.Run("Scenario 2: Update normal pod group", func(t *testing.T) {
		c := newFakeController()

		pod := &v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespace,
				Labels: map[string]string{
					"app": "sts1",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "app/v1",
						Kind:       "StatefulSet",
						Name:       "sts1",
						UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
						Controller: &isController,
					},
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "container1",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								gpuKey: resource.MustParse("1"),
							},
						},
					},
				},
			},
		}

		pod, err := c.kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Case 2 failed when creating pod for %v", err)
		}

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sts1",
				Namespace: namespace,
				UID:       "7a09885b-b753-4924-9fba-77c0836bac20",
				Annotations: map[string]string{
					scheduling.VolcanoGroupMinMemberAnnotationKey: "2",
				},
			},
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "StatefulSet",
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "sts1",
					},
				},
				Replicas: &replicas,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										gpuKey: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
		}

		sts, err = c.kubeClient.AppsV1().StatefulSets(namespace).Create(context.TODO(), sts, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Case 2 failed when creating sts for %v", err)
		}

		existingPodGroup := &scheduling.PodGroup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "scheduling.volcano.sh/v1beta1",
				Kind:       "PodGroup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "app/v1",
						Kind:       "StatefulSet",
						Name:       "sts1",
						UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
						Controller: &isController,
					},
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 3,
				MinResources: &v1.ResourceList{
					gpuKey: resource.MustParse("10"),
				},
			},
		}

		existingPodGroup, err = c.vcClient.SchedulingV1beta1().PodGroups(namespace).Create(context.TODO(), existingPodGroup, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Case 2 failed when creating podGroup for %v", err)
		}

		expectedPodGroup := &scheduling.PodGroup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "scheduling.volcano.sh/v1beta1",
				Kind:       "PodGroup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "podgroup-7a09885b-b753-4924-9fba-77c0836bac20",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "app/v1",
						Kind:       "StatefulSet",
						Name:       "sts1",
						UID:        "7a09885b-b753-4924-9fba-77c0836bac20",
						Controller: &isController,
					},
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 2,
				MinResources: &v1.ResourceList{
					gpuKey: resource.MustParse("2"),
				},
			},
		}

		c.vcInformerFactory.Scheduling().V1beta1().PodGroups().Informer()
		c.vcInformerFactory.Start(context.TODO().Done())
		cache.WaitForNamedCacheSync("", context.TODO().Done(), c.vcInformerFactory.Scheduling().V1beta1().PodGroups().Informer().HasSynced)

		c.createOrUpdateNormalPodPG(pod)
		pg, err := c.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Get(context.TODO(), "podgroup-7a09885b-b753-4924-9fba-77c0836bac20", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Case 2 failed when getting podGroup for %v", err)
		}

		if false == equality.Semantic.DeepEqual(pg.OwnerReferences, expectedPodGroup.OwnerReferences) {
			t.Errorf("Case 2 failed, expect %v, got %v", expectedPodGroup, pg)
		}

		if expectedPodGroup.Spec.PriorityClassName != pod.Spec.PriorityClassName {
			t.Errorf("Case 2 failed, expect %v, got %v",
				expectedPodGroup.Spec.PriorityClassName, pod.Spec.PriorityClassName)
		}

		if pg.Spec.MinMember != expectedPodGroup.Spec.MinMember {
			t.Errorf("Case 2 failed, expect %v, got %v", expectedPodGroup.Spec.MinMember, pg.Spec.MinMember)
		}

		if expectedPodGroup.Spec.MinResources != nil && false == equality.Semantic.DeepEqual(pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI), expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI)) {
			t.Errorf("Case 2 failed, expect %v, got %v", expectedPodGroup.Spec.MinResources.Name(gpuKey, resource.DecimalSI), pg.Spec.MinResources.Name(gpuKey, resource.DecimalSI))
		}
	})
}

func Test_pgcontroller_buildPodGroupFromPod(t *testing.T) {
	// Common test data
	podName := "test-pod"
	podNamespace := "test-ns"
	pgName := "test-pg"
	podUID := types.UID("test-pod-uid")
	isController := true
	replicas := int32(3) // For scenario 2

	// Scenario 1: Do not inherit upper resource annotations
	t.Run("Scenario 1: Do not inherit upper resource annotations", func(t *testing.T) {
		c := newFakeController()
		c.inheritOwnerAnnotations = false

		// Create test pod with annotations and labels
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: podNamespace,
				UID:       podUID,
				Annotations: map[string]string{
					scheduling.QueueNameAnnotationKey: "test-queue",
					scheduling.PodPreemptable:         "true",
				},
				Labels: map[string]string{
					scheduling.CooldownTime: "60s",
				},
			},
			Spec: v1.PodSpec{
				PriorityClassName: "high-priority",
				Containers: []v1.Container{{
					Name: "test-container",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("1"),
						},
					},
				}},
			},
		}

		// Execute test function
		resultPG := c.buildPodGroupFromPod(pod, pgName)

		// Verify basic fields
		if resultPG == nil {
			t.Fatal("resultPG should not be nil")
		}
		if resultPG.Name != pgName {
			t.Errorf("expected PG name %s, got %s", pgName, resultPG.Name)
		}
		if resultPG.Namespace != podNamespace {
			t.Errorf("expected PG namespace %s, got %s", podNamespace, resultPG.Namespace)
		}

		// Verify Spec fields
		if resultPG.Spec.MinMember != 1 {
			t.Errorf("expected MinMember 1, got %d", resultPG.Spec.MinMember)
		}
		if resultPG.Spec.PriorityClassName != "high-priority" {
			t.Errorf("expected PriorityClassName 'high-priority', got %s", resultPG.Spec.PriorityClassName)
		}
		if resultPG.Spec.Queue != "test-queue" {
			t.Errorf("expected Queue 'test-queue', got %s", resultPG.Spec.Queue)
		}

		// Verify MinResources (use reflect.DeepEqual for complex comparison)
		expectedResources := v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}
		if !reflect.DeepEqual(resultPG.Spec.MinResources.Cpu().AsApproximateFloat64(), expectedResources.Cpu().AsApproximateFloat64()) {
			t.Errorf("expected MinResources %v, got %v", expectedResources, *resultPG.Spec.MinResources)
		}

		// Verify annotations and labels
		if resultPG.Annotations[scheduling.PodPreemptable] != "true" {
			t.Errorf("expected annotation %s: 'true', got %s", scheduling.PodPreemptable, resultPG.Annotations[scheduling.PodPreemptable])
		}
		if resultPG.Labels[scheduling.CooldownTime] != "60s" {
			t.Errorf("expected label %s: '60s', got %s", scheduling.CooldownTime, resultPG.Labels[scheduling.CooldownTime])
		}

		// Verify OwnerReferences
		if len(resultPG.OwnerReferences) != 1 {
			t.Fatalf("expected 1 OwnerReference, got %d", len(resultPG.OwnerReferences))
		}
		ownerRef := resultPG.OwnerReferences[0]
		if ownerRef.UID != podUID || ownerRef.Controller == nil || !*ownerRef.Controller {
			t.Errorf("invalid OwnerReference: expected controller %s, got %+v", podUID, ownerRef)
		}
	})

	// Scenario 2: Inherit upper resource annotations
	t.Run("Scenario 2: Inherit upper resource annotations", func(t *testing.T) {
		c := newFakeController()
		c.inheritOwnerAnnotations = true
		ownerUID := types.UID("owner-uid")

		// Create upper ReplicaSet with annotations
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rs-owner",
				Namespace: podNamespace,
				UID:       ownerUID,
				Annotations: map[string]string{
					scheduling.VolcanoGroupMinMemberAnnotationKey: "3",
					scheduling.AnnotationPrefix + "custom-key":    "custom-value",
					scheduling.JDBMinAvailable:                    "2",
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{Containers: []v1.Container{{
						Name: "test-container",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						},
					}}},
				},
			},
		}
		// Add ReplicaSet to fake client
		if _, err := c.kubeClient.AppsV1().ReplicaSets(podNamespace).Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to create ReplicaSet: %v", err)
		}

		// Create pod with OwnerReference to ReplicaSet
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: podNamespace,
				UID:       podUID,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "rs-owner",
					UID:        ownerUID,
					Controller: &isController,
				}},
				Annotations: map[string]string{scheduling.JDBMinAvailable: "1"}, // Should overwrite upper annotation
			},
			Spec: v1.PodSpec{
				PriorityClassName: "low-priority",
				Containers: []v1.Container{{
					Name: "test-container",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					},
				}},
			},
		}

		// Execute test function
		resultPG := c.buildPodGroupFromPod(pod, pgName)

		// Verify inherited annotations and MinMember
		if resultPG.Spec.MinMember != 3 {
			t.Errorf("expected MinMember 3, got %d", resultPG.Spec.MinMember)
		}
		if resultPG.Annotations[scheduling.AnnotationPrefix+"custom-key"] != "custom-value" {
			t.Errorf("expected annotation %s: 'custom-value', got %s", scheduling.AnnotationPrefix+"custom-key", resultPG.Annotations[scheduling.AnnotationPrefix+"custom-key"])
		}
		if resultPG.Annotations[scheduling.JDBMinAvailable] != "1" {
			t.Errorf("expected annotation %s: '1', got %s", scheduling.JDBMinAvailable, resultPG.Annotations[scheduling.JDBMinAvailable])
		}
		if _, ok := resultPG.Annotations[scheduling.JDBMaxUnavailable]; ok {
			t.Error("unexpected JDBMaxUnavailable annotation (should be overwritten by JDBMinAvailable)")
		}

		// Verify MinResources (3 replicas * 1 CPU = 3 CPU)
		expectedResources := v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")}
		if !reflect.DeepEqual(resultPG.Spec.MinResources.Cpu().AsApproximateFloat64(), expectedResources.Cpu().AsApproximateFloat64()) {
			t.Errorf("expected MinResources %v, got %v", expectedResources, *resultPG.Spec.MinResources)
		}
	})

	// Scenario 3: Inherit upper annotations with JDBMaxUnavailable
	t.Run("Scenario 3: Inherit upper annotations with JDBMaxUnavailable", func(t *testing.T) {
		c := newFakeController()
		c.inheritOwnerAnnotations = true
		ownerUID := types.UID("owner-uid-3")

		// Create upper ReplicaSet with JDBMaxUnavailable annotation
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rs-owner-3",
				Namespace: podNamespace,
				UID:       ownerUID,
				Annotations: map[string]string{
					scheduling.VolcanoGroupMinMemberAnnotationKey: "3",
					scheduling.JDBMaxUnavailable:                  "20%",
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test3"}},
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{Containers: []v1.Container{{
						Name: "test-container",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						},
					}}},
				},
			},
		}
		if _, err := c.kubeClient.AppsV1().ReplicaSets(podNamespace).Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to create ReplicaSet: %v", err)
		}

		// Create pod with OwnerReference to ReplicaSet
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName + "-3",
				Namespace: podNamespace,
				UID:       types.UID("pod-uid-3"),
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "rs-owner-3",
					UID:        ownerUID,
					Controller: &isController,
				}},
			},
			Spec: v1.PodSpec{Containers: []v1.Container{{
				Name: "test-container",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				},
			}}},
		}

		// Execute test function
		resultPG := c.buildPodGroupFromPod(pod, pgName)

		// Verify JDBMaxUnavailable is inherited
		if resultPG.Annotations[scheduling.JDBMaxUnavailable] != "20%" {
			t.Errorf("expected JDBMaxUnavailable '20%%', got %s", resultPG.Annotations[scheduling.JDBMaxUnavailable])
		}
		// Verify no JDBMinAvailable annotation
		if _, ok := resultPG.Annotations[scheduling.JDBMinAvailable]; ok {
			t.Error("unexpected JDBMinAvailable annotation")
		}
	})
}

func Test_pgcontroller_updateExistingPodGroup(t *testing.T) {
	// Common test data
	podName := "test-pod"
	podNamespace := "test-ns"
	pgName := "test-pg"
	podUID := types.UID("test-pod-uid")

	t.Run("Sts spec/annotations/labels updated", func(t *testing.T) {
		c := newFakeController()
		c.inheritOwnerAnnotations = false

		// Create test pod with annotations and labels
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: podNamespace,
				UID:       podUID,
				Annotations: map[string]string{
					scheduling.QueueNameAnnotationKey: "test-queue",
					scheduling.PodPreemptable:         "true",
				},
				Labels: map[string]string{
					scheduling.CooldownTime: "60s",
				},
			},
			Spec: v1.PodSpec{
				PriorityClassName: "high-priority",
				Containers: []v1.Container{{
					Name: "test-container",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("1"),
						},
					},
				}},
			},
		}

		// Execute test function
		currentPG := c.buildPodGroupFromPod(pod, pgName)

		// Spec changed
		currentPG.Spec.MinMember = 2
		isUpdated := c.shouldUpdateExistingPodGroup(currentPG, pod)
		if !isUpdated {
			t.Error("expected isUpdated true, got false")
		}

		// Labels changed
		currentPG.Labels[scheduling.CooldownTime] = "120s"
		isUpdated = c.shouldUpdateExistingPodGroup(currentPG, pod)
		if !isUpdated {
			t.Error("expected isUpdated true, got false")
		}

		// Annotations changed
		currentPG.Annotations[scheduling.PodPreemptable] = "false"
		isUpdated = c.shouldUpdateExistingPodGroup(currentPG, pod)
		if !isUpdated {
			t.Error("expected isUpdated true, got false")
		}
	})
}

func TestBuildPodGroupFromPodWithNetworkTopology(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		name                    string
		pod                     *v1.Pod
		expectedNetworkTopology *scheduling.NetworkTopologySpec
	}{
		{
			name: "Pod with NetworkTopology annotations - hard mode with tier",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: namespace,
					Annotations: map[string]string{
						topologyv1alpha1.NetworkTopologyModeAnnotationKey:        "hard",
						topologyv1alpha1.NetworkTopologyHighestTierAnnotationKey: "2",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			expectedNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(2),
			},
		},
		{
			name: "Pod with NetworkTopology annotations - soft mode only",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: namespace,
					Annotations: map[string]string{
						topologyv1alpha1.NetworkTopologyModeAnnotationKey: "soft",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			expectedNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.SoftNetworkTopologyMode,
				HighestTierAllowed: nil,
			},
		},
		{
			name: "Pod with tier annotation only - should default to hard mode",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: namespace,
					Annotations: map[string]string{
						topologyv1alpha1.NetworkTopologyHighestTierAnnotationKey: "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			expectedNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(1),
			},
		},
		{
			name: "Pod without NetworkTopology annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: namespace,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			expectedNetworkTopology: nil,
		},
		{
			name: "Pod with invalid mode annotation - should default to hard",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: namespace,
					Annotations: map[string]string{
						topologyv1alpha1.NetworkTopologyModeAnnotationKey:        "invalid",
						topologyv1alpha1.NetworkTopologyHighestTierAnnotationKey: "3",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			expectedNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(3),
			},
		},
	}

	controller := newFakeController()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pgName := "test-podgroup"
			podGroup := controller.buildPodGroupFromPod(testCase.pod, pgName)

			assert.NotNil(t, podGroup)
			assert.Equal(t, pgName, podGroup.Name)
			assert.Equal(t, namespace, podGroup.Namespace)
			assert.Equal(t, testCase.expectedNetworkTopology, podGroup.Spec.NetworkTopology)
		})
	}
}
