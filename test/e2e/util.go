/*
Copyright 2017 The Kubernetes Authors.

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/gomega"

	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	arbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	arbapi "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

var oneMinute = 1 * time.Minute

var oneCPU = v1.ResourceList{"cpu": resource.MustParse("1000m")}
var twoCPU = v1.ResourceList{"cpu": resource.MustParse("2000m")}
var threeCPU = v1.ResourceList{"cpu": resource.MustParse("3000m")}

const (
	workerPriority = "worker-pri"
	masterPriority = "master-pri"
)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

type context struct {
	kubeclient *kubernetes.Clientset
	karclient  *versioned.Clientset

	namespace              string
	queues                 []string
	enableNamespaceAsQueue bool
}

func initTestContext() *context {
	enableNamespaceAsQueue, _ := strconv.ParseBool(os.Getenv("ENABLE_NAMESPACES_AS_QUEUE"))
	cxt := &context{
		namespace: "test",
		queues:    []string{"q1", "q2"},
	}

	home := homeDir()
	Expect(home).NotTo(Equal(""))

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	Expect(err).NotTo(HaveOccurred())

	cxt.karclient = versioned.NewForConfigOrDie(config)
	cxt.kubeclient = kubernetes.NewForConfigOrDie(config)

	cxt.enableNamespaceAsQueue = enableNamespaceAsQueue

	_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cxt.namespace,
		},
	})
	Expect(err).NotTo(HaveOccurred())

	createQueues(cxt)

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: masterPriority,
		},
		Value:         100,
		GlobalDefault: false,
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: workerPriority,
		},
		Value:         1,
		GlobalDefault: false,
	})
	Expect(err).NotTo(HaveOccurred())

	return cxt
}

func namespaceNotExist(ctx *context) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := ctx.kubeclient.CoreV1().Namespaces().Get(ctx.namespace, metav1.GetOptions{})
		if !(err != nil && errors.IsNotFound(err)) {
			return false, err
		}
		return true, nil
	}
}

func queueNotExist(ctx *context) wait.ConditionFunc {
	return func() (bool, error) {
		for _, q := range ctx.queues {
			var err error
			if ctx.enableNamespaceAsQueue {
				_, err = ctx.kubeclient.CoreV1().Namespaces().Get(q, metav1.GetOptions{})
			} else {
				_, err = ctx.karclient.Scheduling().Queues().Get(q, metav1.GetOptions{})
			}

			if !(err != nil && errors.IsNotFound(err)) {
				return false, err
			}
		}

		return true, nil
	}
}

func cleanupTestContext(cxt *context) {
	foreground := metav1.DeletePropagationForeground

	err := cxt.kubeclient.CoreV1().Namespaces().Delete(cxt.namespace, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred())

	deleteQueues(cxt)

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(masterPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred())

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(workerPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred())

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, oneMinute, namespaceNotExist(cxt))
	Expect(err).NotTo(HaveOccurred())

	// Wait for queues deleted
	err = wait.Poll(100*time.Millisecond, oneMinute, queueNotExist(cxt))
	Expect(err).NotTo(HaveOccurred())
}

func createQueues(cxt *context) {
	var err error

	for _, q := range cxt.queues {
		if cxt.enableNamespaceAsQueue {
			_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: q,
				},
			})
		} else {
			_, err = cxt.karclient.Scheduling().Queues().Create(&arbv1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: q,
				},
				Spec: arbv1.QueueSpec{
					Weight: 1,
				},
			})
		}

		Expect(err).NotTo(HaveOccurred())
	}

	if !cxt.enableNamespaceAsQueue {
		_, err := cxt.karclient.Scheduling().Queues().Create(&arbv1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: cxt.namespace,
			},
			Spec: arbv1.QueueSpec{
				Weight: 1,
			},
		})

		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteQueues(cxt *context) {
	foreground := metav1.DeletePropagationForeground

	for _, q := range cxt.queues {
		var err error

		if cxt.enableNamespaceAsQueue {
			err = cxt.kubeclient.CoreV1().Namespaces().Delete(q, &metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})
		} else {
			err = cxt.karclient.Scheduling().Queues().Delete(q, &metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})
		}

		Expect(err).NotTo(HaveOccurred())
	}

	if !cxt.enableNamespaceAsQueue {
		err := cxt.karclient.Scheduling().Queues().Delete(cxt.namespace, &metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})

		Expect(err).NotTo(HaveOccurred())
	}
}

type taskSpec struct {
	min, rep int32
	img      string
	hostport int32
	req      v1.ResourceList
	affinity *v1.Affinity
	labels   map[string]string
}

type jobSpec struct {
	name      string
	namespace string
	queue     string
	tasks     []taskSpec
}

func getNS(context *context, job *jobSpec) string {
	if len(job.namespace) != 0 {
		return job.namespace
	}

	if context.enableNamespaceAsQueue {
		if len(job.queue) != 0 {
			return job.queue
		}
	}

	return context.namespace
}

func createJobEx(context *context, job *jobSpec) ([]*batchv1.Job, *arbv1.PodGroup) {
	var jobs []*batchv1.Job
	var podgroup *arbv1.PodGroup
	var min int32

	ns := getNS(context, job)

	for i, task := range job.tasks {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", job.name, i),
				Namespace: ns,
			},
			Spec: batchv1.JobSpec{
				Parallelism: &task.rep,
				Completions: &task.rep,
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      task.labels,
						Annotations: map[string]string{arbv1.GroupNameAnnotationKey: job.name},
					},
					Spec: v1.PodSpec{
						SchedulerName: "kube-batch",
						RestartPolicy: v1.RestartPolicyOnFailure,
						Containers:    createContainers(task.img, task.req, task.hostport),
						Affinity:      task.affinity,
					},
				},
			},
		}

		job, err := context.kubeclient.BatchV1().Jobs(job.Namespace).Create(job)
		Expect(err).NotTo(HaveOccurred())
		jobs = append(jobs, job)

		min = min + task.min
	}

	pg := &arbv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.name,
			Namespace: ns,
		},
		Spec: arbv1.PodGroupSpec{
			MinMember: min,
			Queue:     job.queue,
		},
	}

	podgroup, err := context.karclient.Scheduling().PodGroups(pg.Namespace).Create(pg)
	Expect(err).NotTo(HaveOccurred())

	return jobs, podgroup
}

func taskPhase(ctx *context, pg *arbv1.PodGroup, phase []v1.PodPhase, taskNum int) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.karclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(pg.Namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if gn, found := pod.Annotations[arbv1.GroupNameAnnotationKey]; !found || gn != pg.Name {
				continue
			}

			for _, p := range phase {
				if pod.Status.Phase == p {
					readyTaskNum++
					break
				}
			}
		}

		return taskNum <= readyTaskNum, nil
	}
}

func podGroupUnschedulable(ctx *context, pg *arbv1.PodGroup, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.karclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		for _, event := range events.Items {
			target := event.InvolvedObject
			if target.Name == pg.Name && target.Namespace == pg.Namespace {
				if event.Reason == string(arbv1.UnschedulableEvent) && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func podGroupEvicted(ctx *context, pg *arbv1.PodGroup, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.karclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		for _, event := range events.Items {
			target := event.InvolvedObject
			if target.Name == pg.Name && target.Namespace == pg.Namespace {
				if event.Reason == string(arbv1.EvictEvent) && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func waitPodGroupReady(ctx *context, pg *arbv1.PodGroup) error {
	return waitTasksReadyEx(ctx, pg, int(pg.Spec.MinMember))
}

func waitPodGroupPending(ctx *context, pg *arbv1.PodGroup) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodPending}, int(pg.Spec.MinMember)))
}

func waitTasksReadyEx(ctx *context, pg *arbv1.PodGroup, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum))
}

func waitTasksPendingEx(ctx *context, pg *arbv1.PodGroup, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodPending}, taskNum))
}

func waitPodGroupUnschedulable(ctx *context, pg *arbv1.PodGroup) error {
	now := time.Now()
	return wait.Poll(10*time.Second, oneMinute, podGroupUnschedulable(ctx, pg, now))
}

func createContainers(img string, req v1.ResourceList, hostport int32) []v1.Container {
	container := v1.Container{
		Image:           img,
		Name:            img,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources: v1.ResourceRequirements{
			Requests: req,
		},
	}

	if hostport > 0 {
		container.Ports = []v1.ContainerPort{
			{
				ContainerPort: hostport,
				HostPort:      hostport,
			},
		}
	}

	return []v1.Container{container}
}

func createReplicaSet(context *context, name string, rep int32, img string, req v1.ResourceList) *appv1.ReplicaSet {
	deploymentName := "deployment.k8s.io"
	deployment := &appv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
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

	deployment, err := context.kubeclient.AppsV1().ReplicaSets(context.namespace).Create(deployment)
	Expect(err).NotTo(HaveOccurred())

	return deployment
}

func deleteReplicaSet(ctx *context, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.kubeclient.AppsV1().ReplicaSets(ctx.namespace).Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

func replicaSetReady(ctx *context, name string) wait.ConditionFunc {
	return func() (bool, error) {
		deployment, err := ctx.kubeclient.ExtensionsV1beta1().ReplicaSets(ctx.namespace).Get(name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(metav1.ListOptions{})
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

func waitReplicaSetReady(ctx *context, name string) error {
	return wait.Poll(100*time.Millisecond, oneMinute, replicaSetReady(ctx, name))
}

func clusterSize(ctx *context, req v1.ResourceList) int32 {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	used := map[string]*arbapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = arbapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := arbapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	res := int32(0)

	for _, node := range nodes.Items {
		// Skip node with taints
		if len(node.Spec.Taints) != 0 {
			continue
		}

		alloc := arbapi.NewResource(node.Status.Allocatable)
		slot := arbapi.NewResource(req)

		// Removed used resources.
		if res, found := used[node.Name]; found {
			alloc.Sub(res)
		}

		for slot.LessEqual(alloc) {
			alloc.Sub(slot)
			res++
		}
	}

	return res
}

func clusterNodeNumber(ctx *context) int {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
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

func computeNode(ctx *context, req v1.ResourceList) (string, int32) {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	used := map[string]*arbapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = arbapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := arbapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}

		res := int32(0)

		alloc := arbapi.NewResource(node.Status.Allocatable)
		slot := arbapi.NewResource(req)

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

func getPodOfPodGroup(ctx *context, pg *arbv1.PodGroup) []*v1.Pod {
	pg, err := ctx.karclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(pg.Namespace).List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	var qjpod []*v1.Pod

	for _, pod := range pods.Items {
		if gn, found := pod.Annotations[arbv1.GroupNameAnnotationKey]; !found || gn != pg.Name {
			continue
		}
		qjpod = append(qjpod, &pod)

	}

	return qjpod
}

func taintAllNodes(ctx *context, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
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

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, patchBytes)
		Expect(err).NotTo(HaveOccurred())
	}

	return nil
}

func removeTaintsFromAllNodes(ctx *context, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
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

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, patchBytes)
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
