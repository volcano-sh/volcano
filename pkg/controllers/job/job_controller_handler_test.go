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

package job

import (
	"fmt"
	"testing"

	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func newController() *jobcontroller {
	kubeClientSet := kubeclientset.NewForConfigOrDie(&rest.Config{
		Host: "",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &v1.SchemeGroupVersion,
		},
	},
	)

	vcclient := vcclientset.NewForConfigOrDie(&rest.Config{
		Host: "",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &batch.SchemeGroupVersion,
		},
	})

	sharedInformers := informers.NewSharedInformerFactory(kubeClientSet, 0)
	vcSharedInformers := informerfactory.NewSharedInformerFactory(vcclient, 0)

	controller := &jobcontroller{}
	opt := &framework.ControllerOption{
		VolcanoClient:           vcclient,
		KubeClient:              kubeClientSet,
		SharedInformerFactory:   sharedInformers,
		VCSharedInformerFactory: vcSharedInformers,
		WorkerNum:               3,
	}

	controller.Initialize(opt)

	return controller
}

func buildPod(namespace, name string, p v1.PodPhase, labels map[string]string) *v1.Pod {
	boolValue := true
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", namespace, name)),
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			Annotations:     map[string]string{"foo": "bar"},
			ResourceVersion: string(uuid.NewUUID()),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: helpers.JobKind.GroupVersion().String(),
					Kind:       helpers.JobKind.Kind,
					Controller: &boolValue,
				},
			},
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "nginx",
					Image: "nginx:latest",
				},
			},
		},
	}
}

func addPodAnnotation(pod *v1.Pod, annotations map[string]string) *v1.Pod {
	podWithAnnotation := pod
	for key, value := range annotations {
		if podWithAnnotation.Annotations == nil {
			podWithAnnotation.Annotations = make(map[string]string)
		}
		podWithAnnotation.Annotations[key] = value
	}
	return podWithAnnotation
}

func TestAddCommandFunc(t *testing.T) {

	namespace := "test"

	testCases := []struct {
		Name        string
		command     interface{}
		ExpectValue int
	}{
		{
			Name: "AddCommand Success Case",
			command: &bus.Command{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Valid Command",
					Namespace: namespace,
				},
			},
			ExpectValue: 1,
		},
		{
			Name:        "AddCommand Failure Case",
			command:     "Command",
			ExpectValue: 0,
		},
	}

	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addCommand(testcase.command)
			len := controller.commandQueue.Len()
			if testcase.ExpectValue != len {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, len)
			}
		})
	}
}

func TestJobAddFunc(t *testing.T) {
	namespace := "test"

	testCases := []struct {
		Name        string
		job         *batch.Job
		ExpectValue int
	}{
		{
			Name: "AddJob Success",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Job1",
					Namespace: namespace,
				},
			},
			ExpectValue: 1,
		},
	}
	for i, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addJob(testcase.job)
			key := fmt.Sprintf("%s/%s", testcase.job.Namespace, testcase.job.Name)
			job, err := controller.cache.Get(key)
			if job == nil || err != nil {
				t.Errorf("Error while Adding Job in case %d with error %s", i, err)
			}
			queue := controller.getWorkerQueue(key)
			len := queue.Len()
			if testcase.ExpectValue != len {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, len)
			}
		})
	}
}

func TestUpdateJobFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name   string
		oldJob *batch.Job
		newJob *batch.Job
	}{
		{
			Name: "Job Update Success Case",
			oldJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "54467984",
				},
				Spec: batch.JobSpec{
					SchedulerName: "volcano",
					MinAvailable:  5,
				},
				Status: batch.JobStatus{
					State: batch.JobState{
						Phase: batch.Pending,
					},
				},
			},
			newJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "54469999",
				},
				Spec: batch.JobSpec{
					SchedulerName: "volcano",
					MinAvailable:  5,
				},
				Status: batch.JobStatus{
					State: batch.JobState{
						Phase: batch.Running,
					},
				},
			},
		},
		{
			Name: "Job Update Failure Case",
			oldJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "54469999",
				},
				Spec: batch.JobSpec{
					SchedulerName: "volcano",
					MinAvailable:  5,
				},
				Status: batch.JobStatus{
					State: batch.JobState{
						Phase: batch.Pending,
					},
				},
			},
			newJob: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "54469999",
				},
				Spec: batch.JobSpec{
					SchedulerName: "volcano",
					MinAvailable:  5,
				},
				Status: batch.JobStatus{
					State: batch.JobState{
						Phase: batch.Pending,
					},
				},
			},
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addJob(testcase.oldJob)
			controller.updateJob(testcase.oldJob, testcase.newJob)
			key := fmt.Sprintf("%s/%s", testcase.newJob.Namespace, testcase.newJob.Name)
			job, err := controller.cache.Get(key)

			if job == nil || job.Job == nil || err != nil {
				t.Errorf("Error while Updating Job in case %d with error %s", i, err)
			}

			if job.Job.Status.State.Phase != testcase.newJob.Status.State.Phase {
				t.Errorf("Error while Updating Job in case %d with error %s", i, err)
			}
		})
	}
}

func TestAddPodFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name          string
		Job           *batch.Job
		pods          []*v1.Pod
		Annotation    map[string]string
		ExpectedValue int
	}{
		{
			Name: "AddPod Success case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			pods: []*v1.Pod{
				buildPod(namespace, "pod1", v1.PodPending, nil),
			},
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: 1,
		},
		{
			Name: "AddPod Duplicate Pod case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			pods: []*v1.Pod{
				buildPod(namespace, "pod1", v1.PodPending, nil),
				buildPod(namespace, "pod1", v1.PodPending, nil),
			},
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: 1,
		},
		{
			// This case ensures a pod that is already terminating (has DeletionTimestamp)
			// is still added to the job cache correctly.
			Name: "AddPod Success when pod is terminating",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			pods: []*v1.Pod{
				func() *v1.Pod {
					p := buildPod(namespace, "pod1", v1.PodPending, nil)
					now := metav1.Now()
					p.DeletionTimestamp = &now
					return p
				}(),
			},
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: 1,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addJob(testcase.Job)
			for _, pod := range testcase.pods {
				addPodAnnotation(pod, testcase.Annotation)
				controller.addPod(pod)
			}

			key := fmt.Sprintf("%s/%s", testcase.Job.Namespace, testcase.Job.Name)
			job, err := controller.cache.Get(key)

			if job == nil || job.Pods == nil || err != nil {
				t.Errorf("Error while Getting Job in case %d with error %s", i, err)
			}

			var totalPods int
			for _, task := range job.Pods {
				totalPods = len(task)
			}
			if totalPods != testcase.ExpectedValue {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectedValue, totalPods)
			}
		})
	}
}

func TestUpdatePodFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name          string
		Job           *batch.Job
		oldPod        *v1.Pod
		newPod        *v1.Pod
		Annotation    map[string]string
		ExpectedValue v1.PodPhase
	}{
		{
			Name: "UpdatePod Success case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			oldPod: buildPod(namespace, "pod1", v1.PodPending, nil),
			newPod: buildPod(namespace, "pod1", v1.PodRunning, nil),
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: v1.PodRunning,
		},
		{
			Name: "UpdatePod Failed case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			oldPod: buildPod(namespace, "pod1", v1.PodPending, nil),
			newPod: buildPod(namespace, "pod1", v1.PodFailed, nil),
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: v1.PodFailed,
		},
		{
			// This case verifies that when the updated pod has a DeletionTimestamp
			// (terminating), it remains in the job cache instead of being removed.
			Name: "UpdatePod keeps terminating pod in cache",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			oldPod: buildPod(namespace, "pod1", v1.PodRunning, nil),
			newPod: func() *v1.Pod {
				p := buildPod(namespace, "pod1", v1.PodRunning, nil)
				now := metav1.Now()
				p.DeletionTimestamp = &now
				return p
			}(),
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: v1.PodRunning,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addJob(testcase.Job)
			addPodAnnotation(testcase.oldPod, testcase.Annotation)
			addPodAnnotation(testcase.newPod, testcase.Annotation)
			controller.addPod(testcase.oldPod)
			controller.updatePod(testcase.oldPod, testcase.newPod)

			key := fmt.Sprintf("%s/%s", testcase.Job.Namespace, testcase.Job.Name)
			job, err := controller.cache.Get(key)

			if job == nil || job.Pods == nil || err != nil {
				t.Errorf("Error while Getting Job in case %d with error %s", i, err)
			}

			pod := job.Pods[testcase.Annotation[batch.TaskSpecKey]][testcase.oldPod.Name]

			if pod.Status.Phase != testcase.ExpectedValue {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectedValue, pod.Status.Phase)
			}
		})
	}
}

func TestDeletePodFunc(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name          string
		Job           *batch.Job
		availablePods []*v1.Pod
		deletePod     *v1.Pod
		Annotation    map[string]string
		ExpectedValue int
	}{
		{
			Name: "DeletePod success case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			availablePods: []*v1.Pod{
				buildPod(namespace, "pod1", v1.PodRunning, nil),
				buildPod(namespace, "pod2", v1.PodRunning, nil),
			},
			deletePod: buildPod(namespace, "pod2", v1.PodRunning, nil),
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: 1,
		},
		{
			Name: "DeletePod Pod NotAvailable case",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			availablePods: []*v1.Pod{
				buildPod(namespace, "pod1", v1.PodRunning, nil),
				buildPod(namespace, "pod2", v1.PodRunning, nil),
			},
			deletePod: buildPod(namespace, "pod3", v1.PodRunning, nil),
			Annotation: map[string]string{
				batch.JobNameKey:  "job1",
				batch.JobVersion:  "0",
				batch.TaskSpecKey: "task1",
			},
			ExpectedValue: 2,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.addJob(testcase.Job)
			for _, pod := range testcase.availablePods {
				addPodAnnotation(pod, testcase.Annotation)
				controller.addPod(pod)
			}

			addPodAnnotation(testcase.deletePod, testcase.Annotation)
			controller.deletePod(testcase.deletePod)
			key := fmt.Sprintf("%s/%s", testcase.Job.Namespace, testcase.Job.Name)
			job, err := controller.cache.Get(key)

			if job == nil || job.Pods == nil || err != nil {
				t.Errorf("Error while Getting Job in case %d with error %s", i, err)
			}

			var totalPods int
			for _, task := range job.Pods {
				totalPods = len(task)
			}

			if totalPods != testcase.ExpectedValue {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectedValue, totalPods)
			}
		})
	}
}

func TestUpdatePodGroupFunc(t *testing.T) {

	namespace := "test"

	testCases := []struct {
		Name        string
		oldPodGroup *scheduling.PodGroup
		newPodGroup *scheduling.PodGroup
		ExpectValue int
	}{
		{
			Name: "AddCommand Success Case",
			oldPodGroup: &scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: scheduling.PodGroupSpec{
					MinMember: 3,
				},
				Status: scheduling.PodGroupStatus{
					Phase: scheduling.PodGroupPending,
				},
			},
			newPodGroup: &scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: scheduling.PodGroupSpec{
					MinMember: 3,
				},
				Status: scheduling.PodGroupStatus{
					Phase: scheduling.PodGroupRunning,
				},
			},
			ExpectValue: 1,
		},
	}

	for i, testcase := range testCases {

		t.Run(testcase.Name, func(t *testing.T) {
			controller := newController()
			controller.updatePodGroup(testcase.oldPodGroup, testcase.newPodGroup)
			key := fmt.Sprintf("%s/%s", testcase.oldPodGroup.Namespace, testcase.oldPodGroup.Name)
			queue := controller.getWorkerQueue(key)
			len := queue.Len()
			if testcase.ExpectValue != len {
				t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, len)
			}
		})
	}
}

func TestCleanPodDelayActionsKeepsDelayedActionForRecreatedPod(t *testing.T) {
	controller := newController()
	jobKey := "default/test-job"
	podName := "test-job-task1-0"

	t.Cleanup(func() {
		controller.delayActionMapLock.Lock()
		defer controller.delayActionMapLock.Unlock()
		if m, ok := controller.delayActionMap[jobKey]; ok {
			if delayAct, exists := m[podName]; exists && delayAct.cancel != nil {
				delayAct.cancel()
			}
			delete(m, podName)
		}
	})

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			UID:       "job-uid",
		},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name: "task1",
					Policies: []batch.LifecyclePolicy{
						{
							Event:   bus.PodFailedEvent,
							Action:  bus.RestartJobAction,
							Timeout: &metav1.Duration{Duration: time.Hour},
						},
					},
				},
			},
		},
		Status: batch.JobStatus{
			State: batch.JobState{Phase: batch.Running},
		},
	}
	controller.cache.Add(job)

	// 1. Simulate old pod failing
	reqFailed := apis.Request{
		Namespace: "default",
		JobName:   "test-job",
		TaskName:  "task1",
		PodName:   podName,
		PodUID:    "old-uid-A",
		Event:     bus.PodFailedEvent,
	}

	queue := controller.getWorkerQueue(jobKey)
	queue.Add(reqFailed)

	for i := uint32(0); i < controller.workers; i++ {
		if controller.queueList[i].Len() > 0 {
			controller.processNextReq(i)
		}
	}

	controller.delayActionMapLock.Lock()
	delayMap := controller.delayActionMap[jobKey]
	_, delayActionExists := delayMap[podName]
	controller.delayActionMapLock.Unlock()

	if !delayActionExists {
		t.Fatalf("Precondition failed: delayed action was not created")
	}

	// 2. Simulate new pod starting up
	reqRunning := apis.Request{
		Namespace: "default",
		JobName:   "test-job",
		TaskName:  "task1",
		PodName:   podName,
		// Same pod name but different UID to simulate pod recreation.
		PodUID: "new-uid-B",
		Event:  bus.PodRunningEvent,
	}

	queue.Add(reqRunning)

	for i := uint32(0); i < controller.workers; i++ {
		if controller.queueList[i].Len() > 0 {
			controller.processNextReq(i)
		}
	}

	// 3. Check if delay action was incorrectly deleted
	controller.delayActionMapLock.Lock()
	delayMap = controller.delayActionMap[jobKey]
	_, delayActionExists = delayMap[podName]
	controller.delayActionMapLock.Unlock()

	if !delayActionExists {
		t.Fatalf("expected delayed action to remain for recreated pod with a different UID")
	}
}
