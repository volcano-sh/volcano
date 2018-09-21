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

package test

import (
	"os"
	"path/filepath"
	"strings"
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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset/versioned"
	arbapi "github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"strconv"
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

	namespaces             []string
	queues                 []string
	enableNamespaceAsQueue bool
}

func splictJobName(cxt *context, jn string) (string, string, string) {
	nss := strings.Split(jn, "/")
	if len(nss) == 1 {
		return cxt.namespaces[0], nss[0], cxt.namespaces[0]
	}
	if !cxt.enableNamespaceAsQueue {
		return cxt.namespaces[0], nss[1], nss[0]
	}
	return nss[0], nss[1], nss[0]
}

func initTestContext() *context {
	enableNamespaceAsQueue, _ := strconv.ParseBool(os.Getenv("ENABLE_NAMESPACES_AS_QUEUE"))
	cxt := &context{}

	home := homeDir()
	Expect(home).NotTo(Equal(""))

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	Expect(err).NotTo(HaveOccurred())

	cxt.karclient = versioned.NewForConfigOrDie(config)
	cxt.kubeclient = kubernetes.NewForConfigOrDie(config)

	if enableNamespaceAsQueue {
		cxt.namespaces = []string{"test", "n1", "n2"}
	} else {
		cxt.namespaces = []string{"test"}
		cxt.queues = []string{"test", "n1", "n2"}
	}
	cxt.enableNamespaceAsQueue = enableNamespaceAsQueue

	for _, ns := range cxt.namespaces {
		_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ns,
				Namespace: ns,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	for _, q := range cxt.queues {
		_, err = cxt.karclient.Scheduling().Queues().Create(&arbv1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: q,
			},
			Spec: arbv1.QueueSpec{
				Weight: 1,
			},
		})

		Expect(err).NotTo(HaveOccurred())
	}

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
		for _, ns := range ctx.namespaces {
			_, err := ctx.kubeclient.CoreV1().Namespaces().Get(ns, metav1.GetOptions{})
			if !(err != nil && errors.IsNotFound(err)) {
				return false, err
			}
		}
		return true, nil
	}
}

func queueNotExist(ctx *context) wait.ConditionFunc {
	return func() (bool, error) {
		for _, q := range ctx.queues {
			_, err := ctx.karclient.Scheduling().Queues().Get(q, metav1.GetOptions{})
			if !(err != nil && errors.IsNotFound(err)) {
				return false, err
			}
		}

		return true, nil
	}
}

func cleanupTestContext(cxt *context) {
	foreground := metav1.DeletePropagationForeground

	for _, ns := range cxt.namespaces {
		err := cxt.kubeclient.CoreV1().Namespaces().Delete(ns, &metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	for _, q := range cxt.queues {
		err := cxt.karclient.Scheduling().Queues().Delete(q, &metav1.DeleteOptions{
			PropagationPolicy: &foreground,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	err := cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(masterPriority, &metav1.DeleteOptions{
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

type taskSpec struct {
	name string
	pri  string
	rep  int32
	img  string
	req  v1.ResourceList
}

func createJob(
	context *context,
	name string,
	min, rep int32,
	img string,
	req v1.ResourceList,
	affinity *v1.Affinity,
) *batchv1.Job {
	containers := createContainers(img, req, 0)
	return createJobWithOptions(context, "kube-batchd", name, min, rep, affinity, containers)
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

func createJobWithOptions(context *context,
	scheduler string,
	name string,
	min, rep int32,
	affinity *v1.Affinity,
	containers []v1.Container,
) *batchv1.Job {
	queueJobName := "queuejob.k8s.io"
	jns, jn, jq := splictJobName(context, name)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jn,
			Namespace: jns,
		},
		Spec: batchv1.JobSpec{
			Parallelism: &rep,
			Completions: &rep,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{queueJobName: jn},
					Annotations: map[string]string{arbv1.GroupNameAnnotationKey: jn},
				},
				Spec: v1.PodSpec{
					SchedulerName: scheduler,
					RestartPolicy: v1.RestartPolicyNever,
					Containers:    containers,
					Affinity:      affinity,
				},
			},
		},
	}

	pg := &arbv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jn,
			Namespace: jns,
		},
		Spec: arbv1.PodGroupSpec{
			NumMember: min,
			Queue:     jq,
		},
	}

	job, err := context.kubeclient.BatchV1().Jobs(job.Namespace).Create(job)
	Expect(err).NotTo(HaveOccurred())

	pg, err = context.karclient.Scheduling().PodGroups(job.Namespace).Create(pg)
	Expect(err).NotTo(HaveOccurred())

	return job
}

func createReplicaSet(context *context, name string, rep int32, img string, req v1.ResourceList) *appv1.ReplicaSet {
	deploymentName := "deployment.k8s.io"
	deployment := &appv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespaces[0],
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

	deployment, err := context.kubeclient.AppsV1().ReplicaSets(context.namespaces[0]).Create(deployment)
	Expect(err).NotTo(HaveOccurred())

	return deployment
}

func deleteReplicaSet(ctx *context, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.kubeclient.AppsV1().ReplicaSets(ctx.namespaces[0]).Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

func taskReady(ctx *context, jobName string, taskNum int) wait.ConditionFunc {
	jns, jn, _ := splictJobName(ctx, jobName)

	return func() (bool, error) {
		queueJob, err := ctx.kubeclient.BatchV1().Jobs(jns).Get(jn, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(jns).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		pg, err := ctx.karclient.Scheduling().PodGroups(jns).Get(jn, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		readyTaskNum := 0
		for _, pod := range pods.Items {
			labelSelector := labels.SelectorFromSet(queueJob.Spec.Selector.MatchLabels)
			if !labelSelector.Matches(labels.Set(pod.Labels)) ||
				!metav1.IsControlledBy(&pod, queueJob) {
				continue
			}
			if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
				readyTaskNum++
			}

		}

		if taskNum < 0 {
			taskNum = int(pg.Spec.NumMember)
		}

		return taskNum <= readyTaskNum, nil
	}
}

func taskNotReady(ctx *context, jobName string, taskNum int) wait.ConditionFunc {
	jns, jn, _ := splictJobName(ctx, jobName)

	return func() (bool, error) {
		queueJob, err := ctx.kubeclient.BatchV1().Jobs(jns).Get(jn, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(jns).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		notReadyTaskNum := 0
		for _, pod := range pods.Items {
			labelSelector := labels.SelectorFromSet(queueJob.Spec.Selector.MatchLabels)
			if !labelSelector.Matches(labels.Set(pod.Labels)) ||
				!metav1.IsControlledBy(&pod, queueJob) {
				continue
			}
			if pod.Status.Phase == v1.PodPending {
				notReadyTaskNum++
			}
		}

		return taskNum <= notReadyTaskNum, nil
	}
}

func waitJobReady(ctx *context, name string) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskReady(ctx, name, -1))
}

func waitTasksReady(ctx *context, name string, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskReady(ctx, name, taskNum))
}

func waitTasksNotReady(ctx *context, name string, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskNotReady(ctx, name, taskNum))
}

func jobNotReady(ctx *context, jobName string) wait.ConditionFunc {
	return func() (bool, error) {
		queueJob, err := ctx.kubeclient.BatchV1().Jobs(ctx.namespaces[0]).Get(jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespaces[0]).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		pendingTaskNum := int32(0)
		for _, pod := range pods.Items {
			labelSelector := labels.SelectorFromSet(queueJob.Spec.Selector.MatchLabels)
			if !labelSelector.Matches(labels.Set(pod.Labels)) ||
				!metav1.IsControlledBy(&pod, queueJob) {
				continue
			}
			if pod.Status.Phase == v1.PodPending && len(pod.Spec.NodeName) == 0 {
				pendingTaskNum++
			}
		}

		return pendingTaskNum == *queueJob.Spec.Parallelism, nil
	}
}

func waitJobNotReady(ctx *context, name string) error {
	return wait.Poll(10*time.Second, oneMinute, jobNotReady(ctx, name))
}

func replicaSetReady(ctx *context, name string) wait.ConditionFunc {
	return func() (bool, error) {
		deployment, err := ctx.kubeclient.ExtensionsV1beta1().ReplicaSets(ctx.namespaces[0]).Get(name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespaces[0]).List(metav1.ListOptions{})
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

	return len(nodes.Items)
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

func getPodOfJob(ctx *context, jobName string) []*v1.Pod {
	queueJob, err := ctx.kubeclient.BatchV1().Jobs(ctx.namespaces[0]).Get(jobName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespaces[0]).List(metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	var qjpod []*v1.Pod

	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, queueJob) {
			continue
		}
		qjpod = append(qjpod, &pod)
	}

	return qjpod
}
