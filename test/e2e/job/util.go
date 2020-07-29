/*
Copyright 2018 The Volcano Authors.

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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	lagencyerror "errors"

	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	batchv1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/volcano/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

var (
	oneMinute = 1 * time.Minute
	twoMinute = 2 * time.Minute
	oneCPU    = v1.ResourceList{"cpu": resource.MustParse("1000m")}
	twoCPU    = v1.ResourceList{"cpu": resource.MustParse("2000m")}
	threeCPU  = v1.ResourceList{"cpu": resource.MustParse("3000m")}
	thirtyCPU = v1.ResourceList{"cpu": resource.MustParse("30000m")}
	halfCPU   = v1.ResourceList{"cpu": resource.MustParse("500m")}
	CPU1Mem1  = v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")}
	CPU2Mem2  = v1.ResourceList{"cpu": resource.MustParse("2000m"), "memory": resource.MustParse("2048Mi")}
	CPU4Mem4  = v1.ResourceList{"cpu": resource.MustParse("4000m"), "memory": resource.MustParse("4096Mi")}
)

const (
	timeOutMessage               = "timed out waiting for the condition"
	workerPriority               = "worker-pri"
	workerPriorityValue          = -50
	masterPriority               = "master-pri"
	masterPriorityValue          = 100
	defaultNginxImage            = "nginx:1.14"
	nodeFieldSelectorKeyNodeName = "metadata.name"
	defaultBusyBoxImage          = "busybox:1.24"
	defaultMPIImage              = "volcanosh/example-mpi:0.0.1"
	schedulerName                = "volcano"
	executeAction                = "ExecuteAction"
	defaultTFImage               = "volcanosh/dist-mnist-tf-example:0.0.1"
	defaultQueue                 = "default"
)

func cpuResource(request string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(request)}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func masterURL() string {
	if m := os.Getenv("MASTER"); m != "" {
		return m
	}
	return ""
}

func kubeconfigPath(home string) string {
	if m := os.Getenv("KUBECONFIG"); m != "" {
		return m
	}
	return filepath.Join(home, ".kube", "config") // default kubeconfig path is $HOME/.kube/config
}

// VolcanoCliBinary function gets the volcano cli binary.
func VolcanoCliBinary() string {
	if bin := os.Getenv("VC_BIN"); bin != "" {
		return filepath.Join(bin, "vcctl")
	}
	return ""
}

type testContext struct {
	kubeclient *kubernetes.Clientset
	vcclient   *vcclient.Clientset

	namespace        string
	queues           []string
	priorityClasses  map[string]int32
	usingPlaceHolder bool
}

type options struct {
	namespace          string
	queues             []string
	priorityClasses    map[string]int32
	nodesNumLimit      int
	nodesResourceLimit v1.ResourceList
}

var vcClient *vcclient.Clientset
var kubeClient *kubernetes.Clientset

func initTestContext(o options) *testContext {
	if o.namespace == "" {
		o.namespace = helpers.GenRandomStr(8)
	}
	ctx := &testContext{
		namespace:        o.namespace,
		queues:           o.queues,
		priorityClasses:  o.priorityClasses,
		vcclient:         vcClient,
		kubeclient:       kubeClient,
		usingPlaceHolder: false,
	}

	_, err := ctx.kubeclient.CoreV1().Namespaces().Create(context.TODO(),
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ctx.namespace,
			},
		},
		metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred())

	createQueues(ctx)
	createPriorityClasses(ctx)

	if o.nodesNumLimit != 0 && o.nodesResourceLimit != nil {
		setPlaceHolderForSchedulerTesting(ctx, o.nodesResourceLimit, o.nodesNumLimit)
		ctx.usingPlaceHolder = true
	}

	return ctx
}

func namespaceNotExist(ctx *testContext) wait.ConditionFunc {
	return namespaceNotExistWithName(ctx, ctx.namespace)
}

func namespaceNotExistWithName(ctx *testContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := ctx.kubeclient.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	}
}

func queueClosed(ctx *testContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		queue, err := ctx.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if queue.Status.State != schedulingv1beta1.QueueStateClosed {
			return false, nil
		}

		return true, nil
	}
}

func fileExist(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func cleanupTestContext(ctx *testContext) {
	foreground := metav1.DeletePropagationForeground
	err := ctx.kubeclient.CoreV1().Namespaces().Delete(context.TODO(), ctx.namespace, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred(), "delete namespace failed")

	deleteQueues(ctx)

	deletePriorityClasses(ctx)

	if ctx.usingPlaceHolder {
		deletePlaceHolder(ctx)
	}

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, twoMinute, namespaceNotExist(ctx))
	Expect(err).NotTo(HaveOccurred())
}

func setQueueReclaimable(cxt *testContext, queues []string, reclaimable bool) {
	for _, q := range queues {
		queue, err := cxt.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Queue get failed.")
		queue.Spec.Reclaimable = &reclaimable
		_, err = cxt.vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Queue reclaimed failed.")
	}
}

func createQueues(cxt *testContext) {
	for _, q := range cxt.queues {

		_, err := cxt.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})

		//TODO: Better not found error
		if err != nil {
			_, err = cxt.vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: q,
				},
				Spec: schedulingv1beta1.QueueSpec{
					Weight: 1,
				},
			}, metav1.CreateOptions{})
		}

		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteQueues(cxt *testContext) {
	foreground := metav1.DeletePropagationForeground

	for _, q := range cxt.queues {
		queue, err := cxt.vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		queue.Status.State = schedulingv1beta1.QueueStateClosed
		_, err = cxt.vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		err = wait.Poll(100*time.Millisecond, oneMinute, queueClosed(cxt, q))
		Expect(err).NotTo(HaveOccurred())

		err = cxt.vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q,
			metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})

		Expect(err).NotTo(HaveOccurred())
	}
}

func createPriorityClasses(cxt *testContext) {
	for name, value := range cxt.priorityClasses {
		_, err := cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(context.TODO(),
			&schedv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Value:         value,
				GlobalDefault: false,
			},
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
}

func deletePriorityClasses(cxt *testContext) {
	for name := range cxt.priorityClasses {
		err := cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(context.TODO(), name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
}

type taskSpec struct {
	name                  string
	min, rep              int32
	img                   string
	command               string
	workingDir            string
	hostport              int32
	req                   v1.ResourceList
	limit                 v1.ResourceList
	affinity              *v1.Affinity
	labels                map[string]string
	policies              []batchv1alpha1.LifecyclePolicy
	restartPolicy         v1.RestartPolicy
	tolerations           []v1.Toleration
	defaultGracefulPeriod *int64
	taskpriority          string
}

type jobSpec struct {
	name      string
	namespace string
	queue     string
	tasks     []taskSpec
	policies  []batchv1alpha1.LifecyclePolicy
	min       int32
	pri       string
	plugins   map[string][]string
	volumes   []batchv1alpha1.VolumeSpec
	nodeName  string
	// ttl seconds after job finished
	ttl *int32
}

func getNS(context *testContext, job *jobSpec) string {
	if len(job.namespace) != 0 {
		return job.namespace
	}

	return context.namespace
}

func createJob(context *testContext, jobSpec *jobSpec) *batchv1alpha1.Job {

	job, err := createJobInner(context, jobSpec)
	Expect(err).NotTo(HaveOccurred(), "create job")

	return job
}

func createJobWithPodGroup(ctx *testContext, jobSpec *jobSpec, pgName string) *batchv1alpha1.Job {
	ns := getNS(ctx, jobSpec)

	job := &batchv1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobSpec.name,
			Namespace: ns,
		},
		Spec: batchv1alpha1.JobSpec{
			Policies:                jobSpec.policies,
			Queue:                   jobSpec.queue,
			Plugins:                 jobSpec.plugins,
			TTLSecondsAfterFinished: jobSpec.ttl,
		},
	}

	var min int32
	for i, task := range jobSpec.tasks {
		name := task.name
		if len(name) == 0 {
			name = fmt.Sprintf("%s-task-%d", jobSpec.name, i)
		}

		restartPolicy := v1.RestartPolicyOnFailure
		if len(task.restartPolicy) > 0 {
			restartPolicy = task.restartPolicy
		}

		ts := batchv1alpha1.TaskSpec{
			Name:     name,
			Replicas: task.rep,
			Policies: task.policies,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: task.labels,
				},
				Spec: v1.PodSpec{
					SchedulerName:     "volcano",
					RestartPolicy:     restartPolicy,
					Containers:        createContainers(task.img, task.command, task.workingDir, task.req, task.limit, task.hostport),
					Affinity:          task.affinity,
					Tolerations:       task.tolerations,
					PriorityClassName: task.taskpriority,
				},
			},
		}

		if pgName != "" {
			ts.Template.ObjectMeta.Annotations = map[string]string{schedulingv1beta1.KubeGroupNameAnnotationKey: pgName}
		}

		if task.defaultGracefulPeriod != nil {
			ts.Template.Spec.TerminationGracePeriodSeconds = task.defaultGracefulPeriod
		} else {
			//NOTE: TerminationGracePeriodSeconds is set to 3 in default in case of timeout when restarting tasks in test.
			var defaultPeriod int64 = 3
			ts.Template.Spec.TerminationGracePeriodSeconds = &defaultPeriod
		}

		job.Spec.Tasks = append(job.Spec.Tasks, ts)

		min += task.min
	}

	if jobSpec.min > 0 {
		job.Spec.MinAvailable = jobSpec.min
	} else {
		job.Spec.MinAvailable = min
	}

	if jobSpec.pri != "" {
		job.Spec.PriorityClassName = jobSpec.pri
	}

	job.Spec.Volumes = jobSpec.volumes

	jobCreated, err := ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "create job")

	return jobCreated
}

func createJobInner(ctx *testContext, jobSpec *jobSpec) (*batchv1alpha1.Job, error) {
	ns := getNS(ctx, jobSpec)
	job := &batchv1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobSpec.name,
			Namespace: ns,
		},
		Spec: batchv1alpha1.JobSpec{
			SchedulerName:           "volcano",
			Policies:                jobSpec.policies,
			Queue:                   jobSpec.queue,
			Plugins:                 jobSpec.plugins,
			TTLSecondsAfterFinished: jobSpec.ttl,
		},
	}

	var min int32
	for i, task := range jobSpec.tasks {
		name := task.name
		if len(name) == 0 {
			name = fmt.Sprintf("%s-task-%d", jobSpec.name, i)
		}

		restartPolicy := v1.RestartPolicyOnFailure
		if len(task.restartPolicy) > 0 {
			restartPolicy = task.restartPolicy
		}

		ts := batchv1alpha1.TaskSpec{
			Name:     name,
			Replicas: task.rep,
			Policies: task.policies,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: task.labels,
				},
				Spec: v1.PodSpec{
					RestartPolicy:     restartPolicy,
					Containers:        createContainers(task.img, task.command, task.workingDir, task.req, task.limit, task.hostport),
					Affinity:          task.affinity,
					Tolerations:       task.tolerations,
					PriorityClassName: task.taskpriority,
				},
			},
		}
		if jobSpec.nodeName != "" {
			ts.Template.Spec.NodeName = jobSpec.nodeName
		}

		if task.defaultGracefulPeriod != nil {
			ts.Template.Spec.TerminationGracePeriodSeconds = task.defaultGracefulPeriod
		} else {
			//NOTE: TerminationGracePeriodSeconds is set to 3 in default in case of timeout when restarting tasks in test.
			var defaultPeriod int64 = 3
			ts.Template.Spec.TerminationGracePeriodSeconds = &defaultPeriod
		}

		job.Spec.Tasks = append(job.Spec.Tasks, ts)

		min += task.min
	}

	if jobSpec.min > 0 {
		job.Spec.MinAvailable = jobSpec.min
	} else {
		job.Spec.MinAvailable = min
	}

	if jobSpec.pri != "" {
		job.Spec.PriorityClassName = jobSpec.pri
	}

	job.Spec.Volumes = jobSpec.volumes

	return ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
}

func updateJob(ctx *testContext, job *batchv1alpha1.Job) error {
	spec, err := json.Marshal(job.Spec)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec", "value":%s}]`, spec)
	patchBytes := []byte(patch)
	_, err = ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Patch(context.TODO(),
		job.Name, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func waitTaskPhase(ctx *testContext, job *batchv1alpha1.Job, phase []v1.PodPhase, taskNum int) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		pods, err := ctx.kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

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
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func taskPhaseEx(ctx *testContext, job *batchv1alpha1.Job, phase []v1.PodPhase, taskNum map[string]int) error {
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {

		pods, err := ctx.kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

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
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]")
	}
	return err

}

func jobUnschedulable(ctx *testContext, job *batchv1alpha1.Job, now time.Time) error {
	var additionalError error
	// TODO(k82cn): check Job's Condition instead of PodGroup's event.
	err := wait.Poll(10*time.Second, oneMinute, func() (bool, error) {
		pg, err := ctx.vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		if err != nil {
			additionalError = fmt.Errorf("expected to have job's podgroup %s created, actual got error %s",
				job.Name, err.Error())
			return false, nil
		}

		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
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
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func jobEvicted(ctx *testContext, job *batchv1alpha1.Job, time time.Time) wait.ConditionFunc {
	// TODO(k82cn): check Job's conditions instead of PodGroup's event.
	return func() (bool, error) {
		pg, err := ctx.vcclient.SchedulingV1beta1().PodGroups(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

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

func waitJobPhases(ctx *testContext, job *batchv1alpha1.Job, phases []batchv1alpha1.JobPhase) error {
	w, err := ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Watch(context.TODO(), metav1.ListOptions{})
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
	timeout := time.After(oneMinute)

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
			timeout = time.After(oneMinute)

		case <-timeout:
			return fmt.Errorf("[Wait time out]: %s", additionalError)
		}
	}

	return nil
}

func waitJobStates(ctx *testContext, job *batchv1alpha1.Job, phases []batchv1alpha1.JobPhase, waitTime time.Duration) error {
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

func waitJobReady(ctx *testContext, job *batchv1alpha1.Job) error {
	return waitTasksReady(ctx, job, int(job.Spec.MinAvailable))
}

func waitJobPending(ctx *testContext, job *batchv1alpha1.Job) error {
	return waitTaskPhase(ctx, job, []v1.PodPhase{v1.PodPending}, int(job.Spec.MinAvailable))
}

func waitTasksReady(ctx *testContext, job *batchv1alpha1.Job, taskNum int) error {
	return waitTaskPhase(ctx, job, []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum)
}

func waitTasksReadyEx(ctx *testContext, job *batchv1alpha1.Job, taskNum map[string]int) error {
	return taskPhaseEx(ctx, job, []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum)
}

func waitTasksPending(ctx *testContext, job *batchv1alpha1.Job, taskNum int) error {
	return waitTaskPhase(ctx, job, []v1.PodPhase{v1.PodPending}, taskNum)
}

func waitJobStateReady(ctx *testContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Running, oneMinute)
}

func waitJobStatePending(ctx *testContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Pending, oneMinute)
}

func waitJobStateAborted(ctx *testContext, job *batchv1alpha1.Job) error {
	return waitJobPhaseExpect(ctx, job, batchv1alpha1.Aborted, oneMinute)
}

func waitPodPhaseRunningMoreThanNum(ctx *testContext, namespace string, num int) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		clusterPods, err := ctx.kubeclient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to get cluster pod")
		runningPodNum := 0
		for _, pod := range clusterPods.Items {
			if pod.Status.Phase == "Running" {
				runningPodNum = runningPodNum + 1
			}
		}

		expected := runningPodNum >= num
		if !expected {
			additionalError = fmt.Errorf("expected running pod is '%s', actual got %s", strconv.Itoa(runningPodNum), strconv.Itoa(num))
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func waitPodGroupPhase(ctx *testContext, podGroup *schedulingv1beta1.PodGroup, state schedulingv1beta1.PodGroupPhase) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		podGroup, err := ctx.vcclient.SchedulingV1beta1().PodGroups(podGroup.Namespace).Get(context.TODO(), podGroup.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expected := podGroup.Status.Phase == state
		if !expected {
			additionalError = fmt.Errorf("expected podGroup '%s' phase in %s, actual got %s", podGroup.Name,
				state, podGroup.Status.Phase)
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func waitJobPhaseExpect(ctx *testContext, job *batchv1alpha1.Job, state batchv1alpha1.JobPhase, waitTime time.Duration) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		job, err := ctx.vcclient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expected := job.Status.State.Phase == state
		if !expected {
			additionalError = fmt.Errorf("expected job '%s' phase in %s, actual got %s", job.Name,
				state, job.Status.State.Phase)
		}
		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func waitJobPhaseReady(ctx *testContext, job *batchv1.Job) error {
	var additionalError error

	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		job, err := ctx.kubeclient.BatchV1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expected := job.Status.Active > 0
		if !expected {
			additionalError = fmt.Errorf("expected job '%s' active pod to be greater than 0, actual got %d", job.Name, job.Status.Active)
		}
		return expected, nil
	})

	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}

	return err
}

func waitJobUnschedulable(ctx *testContext, job *batchv1alpha1.Job) error {
	now := time.Now()
	return jobUnschedulable(ctx, job, now)
}

func waitQueueStatus(condition func() (bool, error)) error {
	return wait.Poll(100*time.Millisecond, oneMinute, condition)
}

func createContainers(img, command, workingDir string, req, limit v1.ResourceList, hostport int32) []v1.Container {
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

func createReplicaSet(ctx *testContext, name string, rep int32, img string, req v1.ResourceList) *appv1.ReplicaSet {
	deploymentName := "deployment.k8s.io"
	deployment := &appv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ctx.namespace,
		},
		Spec: appv1.ReplicaSetSpec{
			Replicas: &rep,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					deploymentName: name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{deploymentName: name},
				},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyAlways,
					Containers: []v1.Container{
						{
							Image:           img,
							Name:            name,
							ImagePullPolicy: v1.PullIfNotPresent,
							Resources: v1.ResourceRequirements{
								Requests: req,
							},
						},
					},
				},
			},
		},
	}

	deployment, err := ctx.kubeclient.AppsV1().ReplicaSets(ctx.namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	return deployment
}

func waitJobCleanedUp(ctx *testContext, cleanupjob *batchv1alpha1.Job) error {
	var additionalError error

	pods := getTasksOfJob(ctx, cleanupjob)

	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		job, err := ctx.vcclient.BatchV1alpha1().Jobs(cleanupjob.Namespace).Get(context.TODO(), cleanupjob.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, nil
		}
		if len(job.Name) != 0 {
			additionalError = fmt.Errorf("job %s/%s still exist", job.Namespace, job.Name)
			return false, nil
		}

		pg, err := ctx.vcclient.SchedulingV1beta1().PodGroups(cleanupjob.Namespace).Get(context.TODO(), cleanupjob.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, nil
		}
		if len(pg.Name) != 0 {
			additionalError = fmt.Errorf("pdgroup %s/%s still exist", job.Namespace, job.Name)
			return false, nil
		}

		return true, nil
	})
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}

	for _, pod := range pods {
		err := waitPodGone(ctx, pod.Name, pod.Namespace)
		if err != nil {
			return err
		}
	}

	return err
}

func deleteReplicaSet(ctx *testContext, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.kubeclient.AppsV1().ReplicaSets(ctx.namespace).Delete(context.TODO(), name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

func replicaSetReady(ctx *testContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		deployment, err := ctx.kubeclient.AppsV1().ReplicaSets(ctx.namespace).Get(context.TODO(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		labelSelector := labels.SelectorFromSet(deployment.Spec.Selector.MatchLabels)

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if !labelSelector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
				readyTaskNum++
			}
		}

		return *(deployment.Spec.Replicas) == int32(readyTaskNum), nil
	}
}

func waitReplicaSetReady(ctx *testContext, name string) error {
	return wait.Poll(100*time.Millisecond, oneMinute, replicaSetReady(ctx, name))
}

func satisifyMinNodesRequirements(ctx *testContext, num int) bool {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "Failed when get nodes num")
	taintsNodes := 0
	for _, node := range nodes.Items {
		// Skip node with taints
		if len(node.Spec.Taints) != 0 {
			taintsNodes = taintsNodes + 1
		}
	}
	if num <= len(nodes.Items)-taintsNodes {
		return true
	}
	return false
}

func setPlaceHolderForSchedulerTesting(ctx *testContext, req v1.ResourceList, reqNum int) (bool, error) {

	if !satisifyMinNodesRequirements(ctx, reqNum) {
		return false, lagencyerror.New("Failed to setup environment, you need to have at least " + strconv.Itoa(reqNum) + " worker node.")
	}

	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	used := map[string]*schedulerapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = schedulerapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			resource := schedulerapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(resource)
		}
	}

	//var minCPU, minMemory
	minCPU := req.Cpu()
	minMemory := req.Memory()
	resourceRichNode := 0

	//init placeholders
	placeHolders := map[string]v1.ResourceList{}

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}
		minCPUMilli := float64(minCPU.MilliValue())
		minMemoryValue := float64(minMemory.Value())
		currentAllocatable := schedulerapi.NewResource(node.Status.Allocatable)

		if res, found := used[node.Name]; found {
			currentAllocatable.Sub(res)
		}

		phCPU := currentAllocatable.MilliCPU
		phMemory := currentAllocatable.Memory

		if minCPUMilli <= currentAllocatable.MilliCPU && minMemoryValue <= currentAllocatable.Memory {
			resourceRichNode = resourceRichNode + 1
			if resourceRichNode <= reqNum {
				phCPU = currentAllocatable.MilliCPU - minCPUMilli
				phMemory = currentAllocatable.Memory - minMemoryValue
			}
		}

		phCPUQuantity := resource.NewMilliQuantity(int64(phCPU), resource.BinarySI)
		phMemoryQuantity := resource.NewQuantity(int64(phMemory), resource.BinarySI)
		placeHolders[node.Name] = v1.ResourceList{"cpu": *phCPUQuantity, "memory": *phMemoryQuantity}
	}

	if resourceRichNode < reqNum {
		return false, lagencyerror.New("Failed to setup environment, you need to have at least " + strconv.Itoa(len(req)) + " worker node.")
	}

	for nodeName, res := range placeHolders {
		err := createPlaceHolder(ctx, res, nodeName)
		Expect(err).NotTo(HaveOccurred())
	}

	return true, nil
}

func createPlaceHolder(ctx *testContext, phr v1.ResourceList, nodeName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName + "-placeholder",
			Namespace: ctx.namespace,
			Labels: map[string]string{
				"role": "placeholder",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "placeholder",
					Resources: corev1.ResourceRequirements{
						Requests: phr,
						Limits:   phr,
					},
					Image: defaultNginxImage,
				},
			},
			NodeName: nodeName,
		},
	}
	_, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func deletePlaceHolder(ctx *testContext) {

	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(map[string]string{"role": "placeholder"}).String(),
	}
	podList, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(context.TODO(), listOptions)

	Expect(err).NotTo(HaveOccurred(), "Failed to get pod list")

	for _, pod := range podList.Items {
		err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
}

func clusterSize(ctx *testContext, req v1.ResourceList) int32 {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	used := map[string]*schedulerapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = schedulerapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := schedulerapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	res := int32(0)

	for _, node := range nodes.Items {
		// Skip node with taints
		if len(node.Spec.Taints) != 0 {
			continue
		}

		alloc := schedulerapi.NewResource(node.Status.Allocatable)
		slot := schedulerapi.NewResource(req)

		// Removed used resources.
		if res, found := used[node.Name]; found {
			alloc.Sub(res)
		}

		for slot.LessEqual(alloc) {
			alloc.Sub(slot)
			res++
		}
	}
	Expect(res).Should(BeNumerically(">=", 1),
		"Current cluster does not have enough resource for request")
	return res
}

func clusterNodeNumber(ctx *testContext) int {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	nn := 0
	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}
		nn++
	}

	return nn
}

func computeNode(ctx *testContext, req v1.ResourceList) (string, int32) {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	used := map[string]*schedulerapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = schedulerapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := schedulerapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}

		res := int32(0)

		alloc := schedulerapi.NewResource(node.Status.Allocatable)
		slot := schedulerapi.NewResource(req)

		// Removed used resources.
		if res, found := used[node.Name]; found {
			alloc.Sub(res)
		}

		for slot.LessEqual(alloc) {
			alloc.Sub(slot)
			res++
		}

		if res > 0 {
			return node.Name, res
		}
	}

	return "", 0
}

func getTasksOfJob(ctx *testContext, job *batchv1alpha1.Job) []*v1.Pod {
	pods, err := ctx.kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

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

func taintAllNodes(ctx *testContext, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	for _, node := range nodes.Items {
		newNode := node.DeepCopy()

		newTaints := newNode.Spec.Taints
		for _, t := range taints {
			found := false
			for _, nt := range newTaints {
				if nt.Key == t.Key {
					found = true
					break
				}
			}

			if !found {
				newTaints = append(newTaints, t)
			}
		}

		newNode.Spec.Taints = newTaints

		patchBytes, err := preparePatchBytesforNode(node.Name, &node, newNode)
		Expect(err).NotTo(HaveOccurred())

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	return nil
}

func removeTaintsFromAllNodes(ctx *testContext, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) == 0 {
			continue
		}

		newNode := node.DeepCopy()

		var newTaints []v1.Taint
		for _, nt := range newNode.Spec.Taints {
			found := false
			for _, t := range taints {
				if nt.Key == t.Key {
					found = true
					break
				}
			}

			if !found {
				newTaints = append(newTaints, nt)
			}
		}
		newNode.Spec.Taints = newTaints

		patchBytes, err := preparePatchBytesforNode(node.Name, &node, newNode)
		Expect(err).NotTo(HaveOccurred())

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	return nil
}

func preparePatchBytesforNode(nodeName string, oldNode *v1.Node, newNode *v1.Node) ([]byte, error) {
	oldData, err := json.Marshal(oldNode)
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal oldData for node %q: %v", nodeName, err)
	}

	newData, err := json.Marshal(newNode)
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal newData for node %q: %v", nodeName, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Node{})
	if err != nil {
		return nil, fmt.Errorf("failed to CreateTwoWayMergePatch for node %q: %v", nodeName, err)
	}

	return patchBytes, nil
}

// IsNodeReady function returns the node ready status.
func IsNodeReady(node *v1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == v1.NodeReady {
			return c.Status == v1.ConditionTrue
		}
	}
	return false
}

func waitPodGone(ctx *testContext, podName, namespace string) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		_, err := ctx.kubeclient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		expected := errors.IsNotFound(err)
		if !expected {
			additionalError = fmt.Errorf("job related pod should be deleted when aborting job")
		}

		return expected, nil
	})
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func waitJobTerminateAction(ctx *testContext, pg *batchv1alpha1.Job) error {
	return wait.Poll(10*time.Second, oneMinute, jobTerminateAction(ctx, pg, time.Now()))
}

func jobTerminateAction(ctx *testContext, pg *batchv1alpha1.Job, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		for _, event := range events.Items {
			target := event.InvolvedObject
			if strings.HasPrefix(target.Name, pg.Name) && target.Namespace == pg.Namespace {
				if event.Reason == string(executeAction) && strings.Contains(event.Message, "TerminateJob") && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func waitPodPhase(ctx *testContext, pod *v1.Pod, phase []v1.PodPhase) error {
	var additionalError error
	err := wait.Poll(100*time.Millisecond, oneMinute, func() (bool, error) {
		pods, err := ctx.kubeclient.CoreV1().Pods(pod.Namespace).List(context.TODO(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

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
	if err != nil && strings.Contains(err.Error(), timeOutMessage) {
		return fmt.Errorf("[Wait time out]: %s", additionalError)
	}
	return err
}

func pgIsReady(ctx *testContext, namespace string) (bool, error) {
	pgs, err := ctx.vcclient.SchedulingV1beta1().PodGroups(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	if pgs != nil && len(pgs.Items) == 0 {
		return false, fmt.Errorf("podgroup is not found")
	}

	for _, pg := range pgs.Items {
		if pg.Status.Phase != schedulingv1beta1.PodGroupPending {
			return true, nil
		}
	}

	return false, fmt.Errorf("podgroup phase is Pending")
}

func isPodScheduled(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
