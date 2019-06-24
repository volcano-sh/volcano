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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"k8s.io/kubernetes/pkg/util/file"

	. "github.com/onsi/gomega"

	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
	"k8s.io/client-go/tools/clientcmd"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"

	"sync"

	. "github.com/onsi/ginkgo"
)

const currentApiCallMetricsVersion = "v1"

var oneMinute = 1 * time.Minute
var tenMinute = 10 * time.Minute

var halfCPU = v1.ResourceList{"cpu": resource.MustParse("500m")}
var oneCPU = v1.ResourceList{"cpu": resource.MustParse("1000m")}
var twoCPU = v1.ResourceList{"cpu": resource.MustParse("2000m")}
var smallCPU = v1.ResourceList{"cpu": resource.MustParse("2m")}
var oneGigaByteMem = v1.ResourceList{"memory": resource.MustParse("1Gi")}

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
	kbclient   *kbver.Clientset

	namespace string
	queues    []string
}

func initTestContext() *context {
	cxt := &context{
		namespace: "test",
		queues:    []string{"q1", "q2"},
	}

	home := homeDir()
	Expect(home).NotTo(Equal(""))

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	checkError(cxt, err)

	cxt.kbclient = kbver.NewForConfigOrDie(config)
	cxt.kubeclient = kubernetes.NewForConfigOrDie(config)

	_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cxt.namespace,
		},
	})
	checkError(cxt, err)

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: masterPriority,
		},
		Value:         100,
		GlobalDefault: false,
	})
	checkError(cxt, err)

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: workerPriority,
		},
		Value:         1,
		GlobalDefault: false,
	})
	checkError(cxt, err)

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
			_, err := ctx.kbclient.SchedulingV1alpha1().Queues().Get(q, metav1.GetOptions{})
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
	checkError(cxt, err)

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(masterPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	checkError(cxt, err)

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(workerPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	checkError(cxt, err)

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, tenMinute, namespaceNotExist(cxt))
	checkError(cxt, err)
}

func clusterInfo(cxt *context) string {
	// Nodes
	nodes, err := cxt.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list nodes: %v", err)
	}

	pods, err := cxt.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list pods: %v", err)
	}

	clusterInfo := `
Cluster Info:
  Nodes:
{{range .nodes}}
    Name: {{ .Name }}
    Allocatable:
      Pods: {{ .Status.Allocatable.Pods }}
      CPU: {{ .Status.Allocatable.Cpu }}
      Memory: {{ .Status.Allocatable.Memory }}
    Conditions: {{range .Status.Conditions }}
      {{ .Type }}={{ .Status }}{{end}}
    Taints: {{range .Spec.Taints }}
      {{ .Key }} {{ .Value }} {{ .Effect }}{{end}}
{{end}}

  Pods:
{{range .pods}}    {{printf "%-45v%-15v%-10v%-10v" .Name .Namespace .Status.Phase .Spec.NodeName}}
{{end}}
`

	datas := map[string]interface{}{
		"nodes": nodes.Items,
		"pods":  pods.Items,
	}

	// Create a new template and parse the letter into it.
	t := template.Must(template.New("clusterInfo").Parse(clusterInfo))

	output := bytes.NewBuffer(nil)
	err = t.Execute(output, datas)

	if err != nil {
		return fmt.Sprintf("Failed to exeucte template: %v", err)
	}

	return output.String()
}

func checkError(cxt *context, err error) {
	if err != nil {
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), clusterInfo(cxt))
	}
}

func createQueues(cxt *context) {
	var err error

	for _, q := range cxt.queues {
		_, err = cxt.kbclient.SchedulingV1alpha1().Queues().Create(&kbv1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: q,
			},
			Spec: kbv1.QueueSpec{
				Weight: 1,
			},
		})

		checkError(cxt, err)
	}
}

func deleteQueues(cxt *context) {
	foreground := metav1.DeletePropagationForeground

	for _, q := range cxt.queues {
		err := cxt.kbclient.SchedulingV1alpha1().Queues().Delete(q, &metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})

		checkError(cxt, err)
	}

	// Wait for queues deleted
	err := wait.Poll(100*time.Millisecond, oneMinute, queueNotExist(cxt))
	checkError(cxt, err)
}

type taskSpec struct {
	min, rep int32
	img      string
	pri      string
	hostport int32
	req      v1.ResourceList
	affinity *v1.Affinity
	labels   map[string]string
}

type jobSpec struct {
	name      string
	namespace string
	queue     string
	pri       string
	tasks     []taskSpec
	minMember *int32
}

func getNS(context *context, job *jobSpec) string {
	if len(job.namespace) != 0 {
		return job.namespace
	}

	return context.namespace
}

func createJob(context *context, job *jobSpec) ([]*batchv1.Job, *kbv1.PodGroup) {
	var jobs []*batchv1.Job
	var podgroup *kbv1.PodGroup
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
						Annotations: map[string]string{kbv1.GroupNameAnnotationKey: job.name},
					},
					Spec: v1.PodSpec{
						SchedulerName: "volcano",
						RestartPolicy: v1.RestartPolicyOnFailure,
						Containers:    createContainers(task.img, task.req, task.hostport),
						Affinity:      task.affinity,
					},
				},
			},
		}

		if len(task.pri) != 0 {
			job.Spec.Template.Spec.PriorityClassName = task.pri
		}

		job, err := context.kubeclient.BatchV1().Jobs(job.Namespace).Create(job)
		checkError(context, err)
		jobs = append(jobs, job)

		min = min + task.min
	}

	pg := &kbv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.name,
			Namespace: ns,
		},
		Spec: kbv1.PodGroupSpec{
			MinMember:         min,
			Queue:             job.queue,
			PriorityClassName: job.pri,
		},
	}

	if job.minMember != nil {
		pg.Spec.MinMember = *job.minMember
	}

	podgroup, err := context.kbclient.SchedulingV1alpha1().PodGroups(pg.Namespace).Create(pg)
	checkError(context, err)

	return jobs, podgroup
}

func taskPhase(ctx *context, pg *kbv1.PodGroup, phase []v1.PodPhase, taskNum int) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.kbclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		checkError(ctx, err)

		pods, err := ctx.kubeclient.CoreV1().Pods(pg.Namespace).List(metav1.ListOptions{})
		checkError(ctx, err)

		readyTaskNum := 0
		for _, pod := range pods.Items {
			if gn, found := pod.Annotations[kbv1.GroupNameAnnotationKey]; !found || gn != pg.Name {
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

func taskPhaseEx(ctx *context, pg *kbv1.PodGroup, phase []v1.PodPhase, taskNum map[string]int) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.kbclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		checkError(ctx, err)

		pods, err := ctx.kubeclient.CoreV1().Pods(pg.Namespace).List(metav1.ListOptions{})
		checkError(ctx, err)

		readyTaskNum := map[string]int{}
		for _, pod := range pods.Items {
			if gn, found := pod.Annotations[kbv1.GroupNameAnnotationKey]; !found || gn != pg.Name {
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
	}
}

func podGroupUnschedulable(ctx *context, pg *kbv1.PodGroup, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(metav1.ListOptions{})
		checkError(ctx, err)

		for _, event := range events.Items {
			target := event.InvolvedObject
			if target.Name == pg.Name && target.Namespace == pg.Namespace {
				if event.Reason == string(kbv1.PodGroupUnschedulableType) || event.Reason == string("FailedScheduling") &&
					event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

func podGroupEvicted(ctx *context, pg *kbv1.PodGroup, time time.Time) wait.ConditionFunc {
	return func() (bool, error) {
		pg, err := ctx.kbclient.SchedulingV1alpha1().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
		checkError(ctx, err)

		events, err := ctx.kubeclient.CoreV1().Events(pg.Namespace).List(metav1.ListOptions{})
		checkError(ctx, err)

		for _, event := range events.Items {
			target := event.InvolvedObject
			if target.Name == pg.Name && target.Namespace == pg.Namespace {
				if event.Reason == "Evict" && event.LastTimestamp.After(time) {
					return true, nil
				}
			}
		}

		return false, nil
	}
}

// waits the 'timeout' specified duration to check if the PodGroup is ready
func waitTimeoutPodGroupReady(ctx *context, pg *kbv1.PodGroup, timeout time.Duration) error {
	return waitTimeoutTasksReady(ctx, pg, int(pg.Spec.MinMember), timeout)
}

// waits the 'timeout' specified duration to check if the tasks are ready
func waitTimeoutTasksReady(ctx *context, pg *kbv1.PodGroup, taskNum int, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum))
}

func waitPodGroupReady(ctx *context, pg *kbv1.PodGroup) error {
	return waitTasksReady(ctx, pg, int(pg.Spec.MinMember))
}

func waitPodGroupPending(ctx *context, pg *kbv1.PodGroup) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodPending}, int(pg.Spec.MinMember)))
}

func waitTasksReady(ctx *context, pg *kbv1.PodGroup, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum))
}

func waitTasksReadyEx(ctx *context, pg *kbv1.PodGroup, taskNum map[string]int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhaseEx(ctx, pg,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum))
}

func waitTasksPending(ctx *context, pg *kbv1.PodGroup, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodPending}, taskNum))
}

func waitPodGroupUnschedulable(ctx *context, pg *kbv1.PodGroup) error {
	return wait.Poll(10*time.Second, oneMinute, podGroupUnschedulable(ctx, pg, time.Now()))
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
					SchedulerName: "volcano",
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
	checkError(context, err)

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
		checkError(ctx, err)

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(metav1.ListOptions{})
		checkError(ctx, err)

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
	checkError(ctx, err)

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	checkError(ctx, err)

	used := map[string]*kbapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = kbapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := kbapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	res := int32(0)

	for _, node := range nodes.Items {
		// Skip node with taints
		if len(node.Spec.Taints) != 0 {
			continue
		}

		alloc := kbapi.NewResource(node.Status.Allocatable)
		slot := kbapi.NewResource(req)

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
	checkError(ctx, err)

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
	checkError(ctx, err)

	pods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	checkError(ctx, err)

	used := map[string]*kbapi.Resource{}

	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if len(nodeName) == 0 || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		if _, found := used[nodeName]; !found {
			used[nodeName] = kbapi.EmptyResource()
		}

		for _, c := range pod.Spec.Containers {
			req := kbapi.NewResource(c.Resources.Requests)
			used[nodeName].Add(req)
		}
	}

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}

		res := int32(0)

		alloc := kbapi.NewResource(node.Status.Allocatable)
		slot := kbapi.NewResource(req)

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

func getPodOfPodGroup(ctx *context, pg *kbv1.PodGroup) []*v1.Pod {
	pg, err := ctx.kbclient.Scheduling().PodGroups(pg.Namespace).Get(pg.Name, metav1.GetOptions{})
	checkError(ctx, err)

	pods, err := ctx.kubeclient.CoreV1().Pods(pg.Namespace).List(metav1.ListOptions{})
	checkError(ctx, err)

	var qjpod []*v1.Pod

	for _, pod := range pods.Items {
		if gn, found := pod.Annotations[kbv1.GroupNameAnnotationKey]; !found || gn != pg.Name {
			continue
		}
		qjpod = append(qjpod, &pod)

	}

	return qjpod
}

func taintAllNodes(ctx *context, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	checkError(ctx, err)

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
		checkError(ctx, err)

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, patchBytes)
		checkError(ctx, err)
	}

	return nil
}

func removeTaintsFromAllNodes(ctx *context, taints []v1.Taint) error {
	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	checkError(ctx, err)

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
		checkError(ctx, err)

		_, err = ctx.kubeclient.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, patchBytes)
		checkError(ctx, err)
	}

	return nil
}

func getAllWorkerNodes(ctx *context) []string {
	nodeNames := make([]string, 0)

	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	checkError(ctx, err)

	for _, node := range nodes.Items {
		if len(node.Spec.Taints) != 0 {
			continue
		}
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames
}

// this method will return only worker nodes that are ready ignoring all other node including the master node
func getAllWorkerNodesObject(ctx *context) []*v1.Node {
	workernodes := make([]*v1.Node, 0)

	nodes, err := ctx.kubeclient.CoreV1().Nodes().List(metav1.ListOptions{})
	checkError(ctx, err)

	for i, node := range nodes.Items {
		nodeReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
				nodeReady = true
				break
			}
		}
		if IsMasterNode(&node) {
			continue
		}
		// Skip the unready node.
		if !nodeReady {
			continue
		}
		workernodes = append(workernodes, &nodes.Items[i])
	}
	return workernodes
}

// this method will return only worker nodes names that are ready ignoring all other node including the master node
func getAllWorkerNodeNames(ctx *context) []string {

	nodeNames := make([]string, 0)
	nodes := getAllWorkerNodesObject(ctx)

	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames
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

func getkubemarkConfigPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	//TODO: Please update this path as well once the whole tests are being moved into root test folder.
	configPath := filepath.Join(wd, "../../kubemark/kubeconfig.kubemark")
	exist, err := file.FileExists(configPath)
	if err != nil || !exist {
		return ""
	}
	return configPath
}

func initKubemarkDensityTestContext() *context {
	cxt := &context{
		namespace: "test",
		queues:    []string{"q1", "q2"},
	}

	configPath := getkubemarkConfigPath()
	Expect(configPath).NotTo(Equal(""))
	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	checkError(cxt, err)

	cxt.kbclient = kbver.NewForConfigOrDie(config)
	cxt.kubeclient = kubernetes.NewForConfigOrDie(config)

	_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cxt.namespace,
		},
	})
	checkError(cxt, err)

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: masterPriority,
		},
		Value:         100,
		GlobalDefault: false,
	})
	checkError(cxt, err)

	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: workerPriority,
		},
		Value:         1,
		GlobalDefault: false,
	})
	checkError(cxt, err)

	return cxt
}

func cleanupDensityTestContext(cxt *context) {
	foreground := metav1.DeletePropagationForeground

	err := cxt.kubeclient.CoreV1().Namespaces().Delete(cxt.namespace, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	checkError(cxt, err)

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(masterPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	checkError(cxt, err)

	err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(workerPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	checkError(cxt, err)

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, tenMinute, namespaceNotExist(cxt))
	checkError(cxt, err)
}

func createDensityJob(context *context, job *jobSpec) ([]*batchv1.Job, *kbv1.PodGroup) {
	var jobs []*batchv1.Job
	var podgroup *kbv1.PodGroup
	var min int32

	ns := getNS(context, job)

	pg := &kbv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.name,
			Namespace: ns,
		},
		Spec: kbv1.PodGroupSpec{
			MinMember:         min,
			Queue:             job.queue,
			PriorityClassName: job.pri,
		},
	}

	if job.minMember != nil {
		pg.Spec.MinMember = *job.minMember
	}

	podgroup, err := context.kbclient.SchedulingV1alpha1().PodGroups(pg.Namespace).Create(pg)
	checkError(context, err)

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
						Annotations: map[string]string{kbv1.GroupNameAnnotationKey: job.name},
					},
					Spec: v1.PodSpec{
						SchedulerName: "volcano",
						RestartPolicy: v1.RestartPolicyOnFailure,
						Containers:    createContainers(task.img, task.req, task.hostport),
						Affinity:      task.affinity,
					},
				},
			},
		}

		if len(task.pri) != 0 {
			job.Spec.Template.Spec.PriorityClassName = task.pri
		}

		job, err := context.kubeclient.BatchV1().Jobs(job.Namespace).Create(job)
		checkError(context, err)
		jobs = append(jobs, job)

		min = min + task.min
	}

	return jobs, podgroup
}

func waitDensityTasksReady(ctx *context, pg *kbv1.PodGroup, taskNum int) error {
	return wait.Poll(100*time.Millisecond, tenMinute, taskPhase(ctx, pg,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum))
}

func createRunningPodFromRC(wg *sync.WaitGroup, context *context, name, image, podType string, cpuRequest, memRequest resource.Quantity) {
	defer GinkgoRecover()
	defer wg.Done()
	labels := map[string]string{
		"type": podType,
		"name": name,
	}
	rc := &v1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: v1.ReplicationControllerSpec{
			Replicas: func(i int) *int32 { x := int32(i); return &x }(1),
			Selector: labels,
			Template: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: map[string]string{"scheduling.k8s.io/group-name": "qj-1"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  name,
							Image: "nginx",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    cpuRequest,
									v1.ResourceMemory: memRequest,
								},
							},
						},
					},
					DNSPolicy:     v1.DNSDefault,
					SchedulerName: "volcano",
				},
			},
		},
	}

	_, err := context.kubeclient.CoreV1().ReplicationControllers(context.namespace).Create(rc)
	checkError(context, err)

}

func deleteReplicationController(ctx *context, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.kubeclient.CoreV1().ReplicationControllers(ctx.namespace).Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

// IsMasterNode returns true if its a master node or false otherwise.
func IsMasterNode(node *v1.Node) bool {

	for _, taint := range node.Spec.Taints {
		if taint.Key == "node-role.kubernetes.io/master" {
			return true
		}
	}
	return false
}

// getScheduledPodsOfNode will return a list of pods
// which are in running condition
// and ignore the pods which have completed/failed/succeeded
// If a pod status shows scheduled but the node-name is not assigned wait for the
// pod to get scheduled
// we consider only running pods

func getScheduledPodsOfNode(ctx *context, nodeName string) int {
	allPods, err := ctx.kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	// API server returns also Pods that succeeded. We need to filter them out.
	currentPods := make([]v1.Pod, 0, len(allPods.Items))
	for _, pod := range allPods.Items {
		if pod.Status.Phase != v1.PodSucceeded && pod.Status.Phase != v1.PodFailed && pod.Spec.NodeName == nodeName {
			currentPods = append(currentPods, pod)
		}
	}
	allPods.Items = currentPods
	scheduledPods := GetPodsScheduled(allPods, nodeName)

	return len(scheduledPods)
}

// GetPodsScheduled returns a number of currently scheduled Pods.
func GetPodsScheduled(pods *v1.PodList, nodeName string) (scheduledPods []v1.Pod) {
	for _, pod := range pods.Items {
		_, scheduledCondition := GetPodCondition(&pod.Status, v1.PodScheduled)
		Expect(scheduledCondition != nil).To(Equal(true))
		Expect(scheduledCondition.Status).To(Equal(v1.ConditionTrue))
		scheduledPods = append(scheduledPods, pod)
	}

	return
}

// GetPodCondition extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodCondition(status *v1.PodStatus, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if status == nil || status.Conditions == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}

func getRequestedCPU(pod v1.Pod) int64 {
	var result int64
	for _, container := range pod.Spec.Containers {
		result += container.Resources.Requests.Cpu().MilliValue()
	}
	return result
}

func getRequestedMemory(pod v1.Pod) int64 {
	var result int64
	for _, container := range pod.Spec.Containers {
		result += container.Resources.Requests.Memory().Value()
	}
	return result
}
