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
	"time"

	. "github.com/onsi/gomega"

	"k8s.io/api/core/v1"
	appv1 "k8s.io/api/extensions/v1beta1"
	schedv1 "k8s.io/api/scheduling/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1alpha1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
	arbapi "github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
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
	karclient  *clientset.Clientset

	namespace string
}

func initTestContext() *context {
	cxt := &context{
		namespace: "test",
	}

	home := homeDir()
	Expect(home).NotTo(Equal(""))

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	Expect(err).NotTo(HaveOccurred())

	cxt.karclient = clientset.NewForConfigOrDie(config)
	cxt.kubeclient = kubernetes.NewForConfigOrDie(config)

	_, err = cxt.kubeclient.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cxt.namespace,
			Namespace: cxt.namespace,
		},
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = cxt.kubeclient.SchedulingV1alpha1().PriorityClasses().Create(&schedv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: masterPriority,
		},
		Value:         100,
		GlobalDefault: false,
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = cxt.kubeclient.SchedulingV1alpha1().PriorityClasses().Create(&schedv1.PriorityClass{
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
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
}

func cleanupTestContext(cxt *context) {
	err := cxt.kubeclient.CoreV1().Namespaces().Delete(cxt.namespace, &metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred())

	foreground := metav1.DeletePropagationForeground
	err = cxt.kubeclient.SchedulingV1alpha1().PriorityClasses().Delete(masterPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred())

	err = cxt.kubeclient.SchedulingV1alpha1().PriorityClasses().Delete(workerPriority, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred())

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, oneMinute, namespaceNotExist(cxt))
	Expect(err).NotTo(HaveOccurred())
}

type taskSpec struct {
	name string
	pri  string
	rep  int32
	img  string
	req  v1.ResourceList
}

func createQueueJobEx(context *context, name string, min int32, tss []taskSpec) *arbv1.QueueJob {
	taskName := "task.queuejob.k8s.io"

	var taskSpecs []arbv1.TaskSpec

	for _, ts := range tss {
		taskSpecs = append(taskSpecs, arbv1.TaskSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					taskName: ts.name,
				},
			},
			Replicas: ts.rep,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   ts.name,
					Labels: map[string]string{taskName: ts.name},
				},
				Spec: v1.PodSpec{
					SchedulerName:     "kar-scheduler",
					PriorityClassName: ts.pri,
					RestartPolicy:     v1.RestartPolicyNever,
					Containers: []v1.Container{
						{
							Image:           ts.img,
							Name:            ts.name,
							ImagePullPolicy: v1.PullIfNotPresent,
							Resources: v1.ResourceRequirements{
								Requests: ts.req,
							},
						},
					},
				},
			},
		})
	}

	queueJob := &arbv1.QueueJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.QueueJobSpec{
			SchedSpec: arbv1.SchedulingSpecTemplate{
				MinAvailable: int(min),
			},
			TaskSpecs: taskSpecs,
		},
	}

	queueJob, err := context.karclient.ArbV1().QueueJobs(context.namespace).Create(queueJob)
	Expect(err).NotTo(HaveOccurred())

	return queueJob
}

func createQueueJob(context *context, name string, min, rep int32, img string, req v1.ResourceList) *arbv1.QueueJob {
	queueJobName := "queuejob.k8s.io"

	queueJob := &arbv1.QueueJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.QueueJobSpec{
			SchedSpec: arbv1.SchedulingSpecTemplate{
				MinAvailable: int(min),
			},
			TaskSpecs: []arbv1.TaskSpec{
				{

					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							queueJobName: name,
						},
					},
					Replicas: rep,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{queueJobName: name},
						},
						Spec: v1.PodSpec{
							SchedulerName: "kar-scheduler",
							RestartPolicy: v1.RestartPolicyNever,
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
			},
		},
	}

	queueJob, err := context.karclient.ArbV1().QueueJobs(context.namespace).Create(queueJob)
	Expect(err).NotTo(HaveOccurred())

	return queueJob
}

func createQueueJobWithScheduler(context *context, scheduler string, name string, min, rep int32, img string, req v1.ResourceList) *arbv1.QueueJob {
	queueJobName := "queuejob.k8s.io"

	queueJob := &arbv1.QueueJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.QueueJobSpec{
			SchedulerName: scheduler,
			SchedSpec: arbv1.SchedulingSpecTemplate{
				MinAvailable: int(min),
			},
			TaskSpecs: []arbv1.TaskSpec{
				{

					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							queueJobName: name,
						},
					},
					Replicas: rep,
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{queueJobName: name},
						},
						Spec: v1.PodSpec{
							RestartPolicy: v1.RestartPolicyNever,
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
			},
		},
	}

	queueJob, err := context.karclient.ArbV1().QueueJobs(context.namespace).Create(queueJob)
	Expect(err).NotTo(HaveOccurred())

	return queueJob
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

	deployment, err := context.kubeclient.ExtensionsV1beta1().ReplicaSets(context.namespace).Create(deployment)
	Expect(err).NotTo(HaveOccurred())

	return deployment
}

func deleteReplicaSet(ctx *context, name string) error {
	foreground := metav1.DeletePropagationForeground
	return ctx.kubeclient.ExtensionsV1beta1().ReplicaSets(ctx.namespace).Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
}

func taskReady(ctx *context, jobName string, taskNum int) wait.ConditionFunc {
	return func() (bool, error) {
		queueJob, err := ctx.karclient.ArbV1().QueueJobs(ctx.namespace).Get(jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		readyTaskNum := 0
		for _, pod := range pods.Items {
			for _, ts := range queueJob.Spec.TaskSpecs {
				labelSelector := labels.SelectorFromSet(ts.Selector.MatchLabels)
				if !labelSelector.Matches(labels.Set(pod.Labels)) ||
					!metav1.IsControlledBy(&pod, queueJob) {
					continue
				}
				if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
					readyTaskNum++
				}
			}
		}

		if taskNum < 0 {
			taskNum = queueJob.Spec.SchedSpec.MinAvailable
		}

		return taskNum <= readyTaskNum, nil
	}
}

func taskReadyEx(ctx *context, jobName string, tss map[string]int32) wait.ConditionFunc {
	return func() (bool, error) {
		queueJob, err := ctx.karclient.ArbV1().QueueJobs(ctx.namespace).Get(jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		var taskSpecs []arbv1.TaskSpec
		for _, ts := range queueJob.Spec.TaskSpecs {
			if _, found := tss[ts.Template.Name]; found {
				taskSpecs = append(taskSpecs, ts)
			}
		}

		readyTaskNum := map[string]int32{}
		for _, pod := range pods.Items {
			for _, ts := range taskSpecs {
				labelSelector := labels.SelectorFromSet(ts.Selector.MatchLabels)
				if !labelSelector.Matches(labels.Set(pod.Labels)) ||
					!metav1.IsControlledBy(&pod, queueJob) {
					continue
				}

				if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
					readyTaskNum[ts.Template.Name]++
				}
			}
		}

		for name, expected := range tss {
			if readyTaskNum[name] < expected {
				return false, nil
			}
		}
		return true, nil
	}
}

func waitJobReady(ctx *context, name string) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskReady(ctx, name, -1))
}

func waitTasksReady(ctx *context, name string, taskNum int) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskReady(ctx, name, taskNum))
}

func waitTasksReadyEx(ctx *context, name string, ts map[string]int32) error {
	return wait.Poll(100*time.Millisecond, oneMinute, taskReadyEx(ctx, name, ts))
}

func jobNotReady(ctx *context, jobName string) wait.ConditionFunc {
	return func() (bool, error) {
		queueJob, err := ctx.karclient.ArbV1().QueueJobs(ctx.namespace).Get(jobName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		pods, err := ctx.kubeclient.CoreV1().Pods(ctx.namespace).List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		pendingTaskNum := int32(0)
		for _, pod := range pods.Items {
			for _, ts := range queueJob.Spec.TaskSpecs {
				labelSelector := labels.SelectorFromSet(ts.Selector.MatchLabels)
				if !labelSelector.Matches(labels.Set(pod.Labels)) ||
					!metav1.IsControlledBy(&pod, queueJob) {
					continue
				}
				if pod.Status.Phase == v1.PodPending && len(pod.Spec.NodeName) == 0 {
					pendingTaskNum++
				}
			}
		}

		replicas := int32(0)
		for _, ts := range queueJob.Spec.TaskSpecs {
			replicas += ts.Replicas
		}

		return pendingTaskNum == replicas, nil
	}
}

func waitJobNotReady(ctx *context, name string) error {
	return wait.Poll(10*time.Second, oneMinute, jobNotReady(ctx, name))
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

	pods, err := ctx.kubeclient.CoreV1().Pods("").List(metav1.ListOptions{})
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
