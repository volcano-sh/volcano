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
	"os"
	"path/filepath"
	"strconv"
	"time"

	lagencyerror "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	schedv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

var (
	OneMinute  = 1 * time.Minute
	TwoMinute  = 2 * time.Minute
	FiveMinute = 5 * time.Minute
	OneCPU     = v1.ResourceList{"cpu": resource.MustParse("1000m")}
	TwoCPU     = v1.ResourceList{"cpu": resource.MustParse("2000m")}
	ThreeCPU   = v1.ResourceList{"cpu": resource.MustParse("3000m")}
	ThirtyCPU  = v1.ResourceList{"cpu": resource.MustParse("30000m")}
	HalfCPU    = v1.ResourceList{"cpu": resource.MustParse("500m")}
	CPU1Mem1   = v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")}
	CPU2Mem2   = v1.ResourceList{"cpu": resource.MustParse("2000m"), "memory": resource.MustParse("2048Mi")}
	CPU4Mem4   = v1.ResourceList{"cpu": resource.MustParse("4000m"), "memory": resource.MustParse("4096Mi")}
)

const (
	TimeOutMessage               = "timed out waiting for the condition"
	WorkerPriority               = "worker-pri"
	WorkerPriorityValue          = -50
	MasterPriority               = "master-pri"
	MasterPriorityValue          = 100
	NodeFieldSelectorKeyNodeName = "metadata.name"
	SchedulerName                = "volcano"
	ExecuteAction                = "ExecuteAction"
	DefaultQueue                 = "default"
	NumStress                    = 10
)

const (
	DefaultBusyBoxImage = "busybox:1.24"
	DefaultNginxImage   = "nginx:1.14"
	DefaultMPIImage     = "volcanosh/example-mpi:0.0.1"
	DefaultTFImage      = "volcanosh/dist-mnist-tf-example:0.0.1"
)

func CpuResource(request string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(request)}
}

func HomeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func MasterURL() string {
	if m := os.Getenv("MASTER"); m != "" {
		return m
	}
	return ""
}

func KubeconfigPath(home string) string {
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

type TestContext struct {
	Kubeclient *kubernetes.Clientset
	Vcclient   *vcclient.Clientset

	Namespace        string
	Queues           []string
	PriorityClasses  map[string]int32
	UsingPlaceHolder bool
}

type Options struct {
	Namespace          string
	Queues             []string
	PriorityClasses    map[string]int32
	NodesNumLimit      int
	NodesResourceLimit v1.ResourceList
}

var VcClient *vcclient.Clientset
var KubeClient *kubernetes.Clientset

func InitTestContext(o Options) *TestContext {
	By("Initializing test context")

	if o.Namespace == "" {
		o.Namespace = helpers.GenRandomStr(8)
	}
	ctx := &TestContext{
		Namespace:        o.Namespace,
		Queues:           o.Queues,
		PriorityClasses:  o.PriorityClasses,
		Vcclient:         VcClient,
		Kubeclient:       KubeClient,
		UsingPlaceHolder: false,
	}

	_, err := ctx.Kubeclient.CoreV1().Namespaces().Create(context.TODO(),
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ctx.Namespace,
			},
		},
		metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to create namespace")

	CreateQueues(ctx)
	createPriorityClasses(ctx)

	if o.NodesNumLimit != 0 && o.NodesResourceLimit != nil {
		setPlaceHolderForSchedulerTesting(ctx, o.NodesResourceLimit, o.NodesNumLimit)
		ctx.UsingPlaceHolder = true
	}

	return ctx
}

func NamespaceNotExist(ctx *TestContext) wait.ConditionFunc {
	return NamespaceNotExistWithName(ctx, ctx.Namespace)
}

func NamespaceNotExistWithName(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := ctx.Kubeclient.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	}
}

func FileExist(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func CleanupTestContext(ctx *TestContext) {
	By("Cleaning up test context")

	foreground := metav1.DeletePropagationForeground
	err := ctx.Kubeclient.CoreV1().Namespaces().Delete(context.TODO(), ctx.Namespace, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred(), "failed to delete namespace")

	deleteQueues(ctx)
	deletePriorityClasses(ctx)

	if ctx.UsingPlaceHolder {
		deletePlaceHolder(ctx)
	}

	// Wait for namespace deleted.
	err = wait.Poll(100*time.Millisecond, FiveMinute, NamespaceNotExist(ctx))
	Expect(err).NotTo(HaveOccurred(), "failed to wait for namespace deleted")
}

func createPriorityClasses(cxt *TestContext) {
	for name, value := range cxt.PriorityClasses {
		_, err := cxt.Kubeclient.SchedulingV1().PriorityClasses().Create(context.TODO(),
			&schedv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Value:         value,
				GlobalDefault: false,
			},
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create priority class: %s", name)
	}
}

func deletePriorityClasses(cxt *TestContext) {
	for name := range cxt.PriorityClasses {
		err := cxt.Kubeclient.SchedulingV1().PriorityClasses().Delete(context.TODO(), name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
}

func setPlaceHolderForSchedulerTesting(ctx *TestContext, req v1.ResourceList, reqNum int) (bool, error) {
	if !satisfyMinNodesRequirements(ctx, reqNum) {
		return false, lagencyerror.New("Failed to setup environment, you need to have at least " + strconv.Itoa(reqNum) + " worker node.")
	}

	nodes, err := ctx.Kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	pods, err := ctx.Kubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
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

	// var minCPU, minMemory
	minCPU := req.Cpu()
	minMemory := req.Memory()
	resourceRichNode := 0

	// init placeholders
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

func createPlaceHolder(ctx *TestContext, phr v1.ResourceList, nodeName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName + "-placeholder",
			Namespace: ctx.Namespace,
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
					Image: DefaultNginxImage,
				},
			},
			NodeName: nodeName,
		},
	}
	_, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func deletePlaceHolder(ctx *TestContext) {
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(map[string]string{"role": "placeholder"}).String(),
	}
	podList, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), listOptions)
	Expect(err).NotTo(HaveOccurred(), "failed to list pods")

	for _, pod := range podList.Items {
		err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to delete pod %s", pod.Name)
	}
}
