/*
Copyright 2021 The Volcano Authors.

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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

type TaskSpec struct {
	Name                  string
	Min, Rep              int32
	Img                   string
	Command               string
	WorkingDir            string
	Hostport              int32
	Req                   v1.ResourceList
	Limit                 v1.ResourceList
	Affinity              *v1.Affinity
	Labels                map[string]string
	Policies              []batchv1alpha1.LifecyclePolicy
	RestartPolicy         v1.RestartPolicy
	Tolerations           []v1.Toleration
	DefaultGracefulPeriod *int64
	Taskpriority          string
	MaxRetry              int32
}

type JobSpec struct {
	Name      string
	Namespace string
	Queue     string
	Tasks     []TaskSpec
	Policies  []batchv1alpha1.LifecyclePolicy
	Min       int32
	Pri       string
	Plugins   map[string][]string
	Volumes   []batchv1alpha1.VolumeSpec
	NodeName  string
	// ttl seconds after job finished
	Ttl        *int32
	MinSuccess *int32
	// job max retry
	MaxRetry int32
}

func Namespace(context *TestContext, job *JobSpec) string {
	if len(job.Namespace) != 0 {
		return job.Namespace
	}

	return context.Namespace
}

func CreateJob(context *TestContext, jobSpec *JobSpec) *batchv1alpha1.Job {
	job, err := CreateJobInner(context, jobSpec)
	Expect(err).NotTo(HaveOccurred(), "failed to create job %s in namespace %s", jobSpec.Name, jobSpec.Namespace)
	return job
}

func CreateJobWithPodGroup(ctx *TestContext, jobSpec *JobSpec,
	pgName string, annotations map[string]string) *batchv1alpha1.Job {
	ns := Namespace(ctx, jobSpec)

	job := &batchv1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobSpec.Name,
			Namespace:   ns,
			Annotations: annotations,
		},
		Spec: batchv1alpha1.JobSpec{
			Policies:                jobSpec.Policies,
			Queue:                   jobSpec.Queue,
			Plugins:                 jobSpec.Plugins,
			TTLSecondsAfterFinished: jobSpec.Ttl,
		},
	}

	var min int32
	for i, task := range jobSpec.Tasks {
		name := task.Name
		if len(name) == 0 {
			name = fmt.Sprintf("%s-task-%d", jobSpec.Name, i)
		}

		restartPolicy := v1.RestartPolicyOnFailure
		if len(task.RestartPolicy) > 0 {
			restartPolicy = task.RestartPolicy
		}

		ts := batchv1alpha1.TaskSpec{
			Name:     name,
			Replicas: task.Rep,
			Policies: task.Policies,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: task.Labels,
				},
				Spec: v1.PodSpec{
					SchedulerName:     "volcano",
					RestartPolicy:     restartPolicy,
					Containers:        CreateContainers(task.Img, task.Command, task.WorkingDir, task.Req, task.Limit, task.Hostport),
					Affinity:          task.Affinity,
					Tolerations:       task.Tolerations,
					PriorityClassName: task.Taskpriority,
				},
			},
		}

		if pgName != "" {
			ts.Template.ObjectMeta.Annotations = map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName}
		}

		if task.DefaultGracefulPeriod != nil {
			ts.Template.Spec.TerminationGracePeriodSeconds = task.DefaultGracefulPeriod
		} else {
			// NOTE: TerminationGracePeriodSeconds is set to 3 in default in case of timeout when restarting tasks in test.
			var defaultPeriod int64 = 3
			ts.Template.Spec.TerminationGracePeriodSeconds = &defaultPeriod
		}

		job.Spec.Tasks = append(job.Spec.Tasks, ts)

		min += task.Min
	}

	if jobSpec.Min > 0 {
		job.Spec.MinAvailable = jobSpec.Min
	} else {
		job.Spec.MinAvailable = min
	}

	if jobSpec.Pri != "" {
		job.Spec.PriorityClassName = jobSpec.Pri
	}

	job.Spec.Volumes = jobSpec.Volumes

	jobCreated, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create job %s in namespace %s", job.Name, job.Namespace)

	return jobCreated
}

func UpdateJob(ctx *TestContext, job *batchv1alpha1.Job) error {
	spec, err := json.Marshal(job.Spec)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec", "value":%s}]`, spec)
	patchBytes := []byte(patch)
	_, err = ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Patch(context.TODO(),
		job.Name, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func CreateJobInner(ctx *TestContext, jobSpec *JobSpec) (*batchv1alpha1.Job, error) {
	ns := Namespace(ctx, jobSpec)

	job := &batchv1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobSpec.Name,
			Namespace: ns,
		},
		Spec: batchv1alpha1.JobSpec{
			SchedulerName:           "volcano",
			Policies:                jobSpec.Policies,
			Queue:                   jobSpec.Queue,
			Plugins:                 jobSpec.Plugins,
			TTLSecondsAfterFinished: jobSpec.Ttl,
			MinSuccess:              jobSpec.MinSuccess,
			MaxRetry:                jobSpec.MaxRetry,
		},
	}

	var min int32
	for i, task := range jobSpec.Tasks {
		name := task.Name
		if len(name) == 0 {
			name = fmt.Sprintf("%s-task-%d", jobSpec.Name, i)
		}

		restartPolicy := v1.RestartPolicyOnFailure
		if len(task.RestartPolicy) > 0 {
			restartPolicy = task.RestartPolicy
		}

		maxRetry := task.MaxRetry
		if maxRetry == 0 {
			maxRetry = -1
		}

		ts := batchv1alpha1.TaskSpec{
			Name:     name,
			Replicas: task.Rep,
			Policies: task.Policies,
			MaxRetry: maxRetry,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: task.Labels,
				},
				Spec: v1.PodSpec{
					RestartPolicy:     restartPolicy,
					Containers:        CreateContainers(task.Img, task.Command, task.WorkingDir, task.Req, task.Limit, task.Hostport),
					Affinity:          task.Affinity,
					Tolerations:       task.Tolerations,
					PriorityClassName: task.Taskpriority,
				},
			},
		}
		if jobSpec.NodeName != "" {
			ts.Template.Spec.NodeName = jobSpec.NodeName
		}

		if task.DefaultGracefulPeriod != nil {
			ts.Template.Spec.TerminationGracePeriodSeconds = task.DefaultGracefulPeriod
		} else {
			// NOTE: TerminationGracePeriodSeconds is set to 3 in default in case of timeout when restarting tasks in test.
			var defaultPeriod int64 = 3
			ts.Template.Spec.TerminationGracePeriodSeconds = &defaultPeriod
		}

		job.Spec.Tasks = append(job.Spec.Tasks, ts)

		min += task.Min
	}

	if jobSpec.Min > 0 {
		job.Spec.MinAvailable = jobSpec.Min
	} else {
		job.Spec.MinAvailable = min
	}

	if jobSpec.Pri != "" {
		job.Spec.PriorityClassName = jobSpec.Pri
	}

	job.Spec.Volumes = jobSpec.Volumes

	return ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
}

func WaitTaskPhase(ctx *TestContext, job *batchv1alpha1.Job, phase []v1.PodPhase, taskNum int) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", job.Namespace)

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if !metav1.IsControlledBy(&pod, job) {
				continue
			}

			for _, p := range phase {
				if pod.Status.Phase == p {
					readyTaskNum++
					break
				}
			}
		}

		ready := taskNum <= readyTaskNum
		if !ready {
			additionalError = fmt.Errorf("expected job '%s' to have %d ready pods, actual got %d", job.Name,
				taskNum,
				readyTaskNum)
		}
		return ready, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func taskPhaseEx(ctx *TestContext, job *batchv1alpha1.Job, phase []v1.PodPhase, taskNum map[string]int) error {
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {

		pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", job.Namespace)

		readyTaskNum := map[string]int{}
		for _, pod := range pods.Items {
			if !metav1.IsControlledBy(&pod, job) {
				continue
			}

			for _, p := range phase {
				if pod.Status.Phase == p {
					readyTaskNum[pod.Spec.PriorityClassName]++
					break
				}
			}
		}

		for k, v := range taskNum {
			if v > readyTaskNum[k] {
				return false, nil
			}
		}

		return true, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]")
	}
	return err

}

func jobUnschedulable(ctx *TestContext, job *batchv1alpha1.Job, now time.Time) error {
	var additionalError error
	// TODO(k82cn): check Job's Condition instead of PodGroup's event.
	err := wait.Poll(10*time.Second, FiveMinute, func() (bool, error) {
		pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		if err != nil {
			additionalError = fmt.Errorf("expected to have job's podgroup %s created, actual got error %s",
				job.Name, err.Error())
			return false, nil
		}

		events, err := ctx.Kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			additionalError = fmt.Errorf("expected to have events for job %s, actual got error %s",
				job.Name, err.Error())
			return false, nil
		}
		for _, event := range events.Items {
			target := event.InvolvedObject
			if strings.HasPrefix(target.Name, pg.Name) && target.Namespace == pg.Namespace {
				if event.Reason == string("Unschedulable") || event.Reason == string("FailedScheduling") && event.LastTimestamp.After(now) {
					return true, nil
				}
			}
		}
		additionalError = fmt.Errorf(
			"expected to have 'Unschedulable' events for podgroup %s, actual got nothing", job.Name)
		return false, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func JobEvicted(ctx *TestContext, job *batchv1alpha1.Job, time time.Time) wait.ConditionFunc {
	// TODO(k82cn): check Job's conditions instead of PodGroup's event.
	return func() (bool, error) {
		pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get pod group of job %s in namespace %s", job.Name, job.Namespace)

		events, err := ctx.Kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list events in namespace %s", pg.Namespace)

		for _, event := range events.Items {
			target := event.InvolvedObject
			if target.Name == pg.Name && target.Namespace == pg.Namespace {
				if event.Reason == string("Evict") && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}
		return false, nil
	}
}

func WaitJobPhases(ctx *TestContext, job *batchv1alpha1.Job, phases []batchv1alpha1.JobPhase) error {
	w, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	defer w.Stop()

	var additionalError error
	total := int32(0)
	for _, task := range job.Spec.Tasks {
		total += task.Replicas
	}

	ch := w.ResultChan()
	index := 0
	timeout := time.After(FiveMinute)

	for index < len(phases) {
		select {
		case event, open := <-ch:
			if !open {
				return fmt.Errorf("watch channel should be always open")
			}

			newJob := event.Object.(*batchv1alpha1.Job)
			phase := phases[index]
			if newJob.Name != job.Name || newJob.Namespace != job.Namespace {
				continue
			}

			if newJob.Status.State.Phase != phase {
				additionalError = fmt.Errorf(
					"expected job '%s' to be in status %s, actual get %s",
					job.Name, phase, newJob.Status.State.Phase)
				continue
			}

			var flag bool
			switch phase {
			case batchv1alpha1.Pending:
				flag = (newJob.Status.Pending+newJob.Status.Succeeded+
					newJob.Status.Failed+newJob.Status.Running) == 0 ||
					(total-newJob.Status.Terminating >= newJob.Status.MinAvailable)
			case batchv1alpha1.Terminating, batchv1alpha1.Aborting, batchv1alpha1.Restarting, batchv1alpha1.Completing:
				flag = newJob.Status.Terminating > 0
			case batchv1alpha1.Terminated, batchv1alpha1.Aborted, batchv1alpha1.Completed:
				flag = newJob.Status.Pending == 0 &&
					newJob.Status.Running == 0 &&
					newJob.Status.Terminating == 0
			case batchv1alpha1.Running:
				flag = newJob.Status.Running >= newJob.Spec.MinAvailable
			default:
				return fmt.Errorf("unknown phase %s", phase)
			}

			if !flag {
				additionalError = fmt.Errorf(
					"expected job '%s' to be in status %s, actual detail status %s",
					job.Name, phase, getJobStatusDetail(newJob))
				continue
			}

			index++
			timeout = time.After(FiveMinute)

		case <-timeout:
			return fmt.Errorf("[Wait time out]: %s", additionalError)
		}
	}

	return nil
}

func WaitJobStates(ctx *TestContext, job *batchv1alpha1.Job, phases []batchv1alpha1.JobPhase, waitTime time.Duration) error {
	for _, phase := range phases {
		err := waitJobPhaseExpect(ctx, job, phase, waitTime)
		if err != nil {
			return err
		}
	}
	return nil
}

func getJobStatusDetail(job *batchv1alpha1.Job) string {
	return fmt.Sprintf("\nName: %s\n Phase: %s\nPending: %d"+
		"\nRunning: %d\nSucceeded: %d\nTerminating: %d\nFailed: %d\n ",
		job.Name, job.Status.State.Phase, job.Status.Pending, job.Status.Running,
		job.Status.Succeeded, job.Status.Terminating, job.Status.Failed)
}

// WaitJobReady waits for the Job to be ready
func WaitJobReady(ctx *TestContext, job *batchv1alpha1.Job) error {
	return WaitTasksReady(ctx, job, int(job.Spec.MinAvailable))
}

// WaitJobPending waits for the Job to be pending
func WaitJobPending(ctx *TestContext, job *batchv1alpha1.Job) error {
	return WaitTaskPhase(ctx, job, []v1.PodPhase{v1.PodPending}, int(job.Spec.MinAvailable))
}

// WaitTasksReady waits for the tasks of a Job to be ready
func WaitTasksReady(ctx *TestContext, job *batchv1alpha1.Job, taskNum int) error {
	return WaitTaskPhase(ctx, job, []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum)
}

func WaitTasksReadyEx(ctx *TestContext, job *batchv1alpha1.Job, taskNum map[string]int) error {
	return taskPhaseEx(ctx, job, []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum)
}

// WaitTasksPending waits for the tasks of a Job to be pending
func WaitTasksPending(ctx *TestContext, job *batchv1alpha1.Job, taskNum int) error {
	return WaitTaskPhase(ctx, job, []v1.PodPhase{v1.PodPending}, taskNum)
}

// WaitJobStateReady waits for the state of a Job to be ready
func WaitJobStateReady(ctx *TestContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Running, FiveMinute)
}

// WaitJobStatePending waits for the state of a Job to be pending
func WaitJobStatePending(ctx *TestContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Pending, FiveMinute)
}

// WaitJobStateAborted waits for the state of a Job to be aborted
func WaitJobStateAborted(ctx *TestContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Aborted, FiveMinute)
}

// WaitPodPhaseRunningMoreThanNum waits for the number of running pods to be more than specified number
func WaitPodPhaseRunningMoreThanNum(ctx *TestContext, namespace string, num int) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		clusterPods, err := ctx.Kubeclient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", namespace)

		runningPodNum := 0
		for _, pod := range clusterPods.Items {
			if pod.Status.Phase == "Running" {
				runningPodNum++
			}
		}

		expected := runningPodNum >= num
		if !expected {
			additionalError = fmt.Errorf("expected running pod is '%s', actual got %s", strconv.Itoa(runningPodNum), strconv.Itoa(num))
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func waitJobPhaseExpect(ctx *TestContext, job *batchv1alpha1.Job, state batchv1alpha1.JobPhase, waitTime time.Duration) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		job, err := ctx.Vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expected := job.Status.State.Phase == state
		if !expected {
			additionalError = fmt.Errorf("expected job '%s' phase in %s, actual got %s", job.Name,
				state, job.Status.State.Phase)
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func WaitJobPhaseReady(ctx *TestContext, job *batchv1.Job) error {
	var additionalError error

	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		job, err := ctx.Kubeclient.BatchV1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expected := job.Status.Active > 0
		if !expected {
			additionalError = fmt.Errorf("expected job '%s' active pod to be greater than 0, actual got %d", job.Name, job.Status.Active)
		}
		return expected, nil
	})

	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}

	return err
}

func WaitJobUnschedulable(ctx *TestContext, job *batchv1alpha1.Job) error {
	now := time.Now()
	return jobUnschedulable(ctx, job, now)
}

func CreateContainers(img, command, workingDir string, req, limit v1.ResourceList, hostport int32) []v1.Container {
	var imageRepo []string
	container := v1.Container{
		Image:           img,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources: v1.ResourceRequirements{
			Requests: req,
			Limits:   limit,
		},
	}
	if !strings.Contains(img, ":") {
		imageRepo = strings.Split(img, "/")
	} else {
		imageRepo = strings.Split(img[:strings.Index(img, ":")], "/")
	}
	container.Name = imageRepo[len(imageRepo)-1]

	if len(command) > 0 {
		container.Command = []string{"/bin/sh"}
		container.Args = []string{"-c", command}
	}

	if hostport > 0 {
		container.Ports = []v1.ContainerPort{
			{
				ContainerPort: hostport,
				HostPort:      hostport,
			},
		}
	}

	if len(workingDir) > 0 {
		container.WorkingDir = workingDir
	}

	return []v1.Container{container}
}

// WaitJobCleanedUp waits for the Job to be cleaned up
func WaitJobCleanedUp(ctx *TestContext, cleanupjob *batchv1alpha1.Job) error {
	var additionalError error

	pods := GetTasksOfJob(ctx, cleanupjob)

	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		job, err := ctx.Vcclient.BatchV1alpha1().Jobs(cleanupjob.Namespace).Get(context.TODO(), cleanupjob.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, nil
		}
		if len(job.Name) != 0 {
			additionalError = fmt.Errorf("job %s/%s still exist", job.Namespace, job.Name)
			return false, nil
		}

		pg, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(cleanupjob.Namespace).Get(context.TODO(), cleanupjob.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, nil
		}
		if len(pg.Name) != 0 {
			additionalError = fmt.Errorf("pdgroup %s/%s still exist", job.Namespace, job.Name)
			return false, nil
		}

		return true, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}

	for _, pod := range pods {
		err := WaitPodGone(ctx, pod.Name, pod.Namespace)
		if err != nil {
			return err
		}
	}

	return err
}

// GetTasksOfJob returns the tasks belongs to the job
func GetTasksOfJob(ctx *TestContext, job *batchv1alpha1.Job) []*v1.Pod {
	pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", job.Namespace)

	var tasks []*v1.Pod

	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, job) {
			continue
		}
		duplicatePod := pod.DeepCopy()
		tasks = append(tasks, duplicatePod)
	}

	return tasks
}

// WaitPodGone waits the Pod to be deleted when aborting a Job
func WaitPodGone(ctx *TestContext, podName, namespace string) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		_, err := ctx.Kubeclient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		expected := errors.IsNotFound(err)
		if !expected {
			additionalError = fmt.Errorf("job related pod should be deleted when aborting job")
		}

		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

// WaitJobTerminateAction waits for the Job to be terminated
func WaitJobTerminateAction(ctx *TestContext, pg *batchv1alpha1.Job) error {
	return wait.Poll(10*time.Second, FiveMinute, jobTerminateAction(ctx, pg, time.Now()))
}

func jobTerminateAction(ctx *TestContext, pg *batchv1alpha1.Job, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		events, err := ctx.Kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list events in namespace %s", pg.Namespace)

		for _, event := range events.Items {
			target := event.InvolvedObject
			if strings.HasPrefix(target.Name, pg.Name) && target.Namespace == pg.Namespace {
				if event.Reason == string(ExecuteAction) && strings.Contains(event.Message, "TerminateJob") && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

// WaitPodPhase waits for the Pod to be the specified phase
func WaitPodPhase(ctx *TestContext, pod *v1.Pod, phase []v1.PodPhase) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, FiveMinute, func() (bool, error) {
		pods, err := ctx.Kubeclient.CoreV1().Pods(pod.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", pod.Namespace)

		for _, p := range phase {
			for _, pod := range pods.Items {
				if pod.Status.Phase == p {
					return true, nil
				}
			}
		}

		additionalError = fmt.Errorf("expected pod '%s' to %v, actual got %s", pod.Name, phase, pod.Status.Phase)
		return false, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

// IsPodScheduled returns whether the Pod is scheduled
func IsPodScheduled(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

// WaitTasksCompleted waits for the tasks of a job to be completed
func WaitTasksCompleted(ctx *TestContext, job *batchv1alpha1.Job, successNum int32) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, TwoMinute, func() (bool, error) {
		pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to list pods in namespace %s", job.Namespace)

		var succeeded int32 = 0
		for _, pod := range pods.Items {
			if !metav1.IsControlledBy(&pod, job) {
				continue
			}

			if pod.Status.Phase == "Succeeded" {
				succeeded++
			}
		}

		ready := succeeded >= successNum
		if !ready {
			additionalError = fmt.Errorf("expected job '%s' to have %d succeeded pods, actual got %d", job.Name,
				successNum,
				succeeded)
		}
		return ready, nil
	})
	if err != nil && strings.Contains(err.Error(), TimeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}
