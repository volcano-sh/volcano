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
	"fmt"
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
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"

	"volcano.sh/volcano/pkg/controllers/job/helpers"
	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

var (
	OneMinute  = 1 * time.Minute
	TwoMinute  = 2 * time.Minute
	FiveMinute = 5 * time.Minute
	TenMinute  = 10 * time.Minute
	OneCPU     = v1.ResourceList{"cpu": resource.MustParse("1000m")}
	TwoCPU     = v1.ResourceList{"cpu": resource.MustParse("2000m")}
	ThreeCPU   = v1.ResourceList{"cpu": resource.MustParse("3000m")}
	ThirtyCPU  = v1.ResourceList{"cpu": resource.MustParse("30000m")}
	HalfCPU    = v1.ResourceList{"cpu": resource.MustParse("500m")}
	CPU1Mem1   = v1.ResourceList{"cpu": resource.MustParse("1000m"), "memory": resource.MustParse("1024Mi")}
	CPU2Mem2   = v1.ResourceList{"cpu": resource.MustParse("2000m"), "memory": resource.MustParse("2048Mi")}
	CPU4Mem4   = v1.ResourceList{"cpu": resource.MustParse("4000m"), "memory": resource.MustParse("4096Mi")}
	CPU5Mem5   = v1.ResourceList{"cpu": resource.MustParse("5000m"), "memory": resource.MustParse("5120Mi")}
	CPU6Mem6   = v1.ResourceList{"cpu": resource.MustParse("6000m"), "memory": resource.MustParse("6144Mi")}
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
	DefaultStorageClass          = "standard"
)

const (
	DefaultBusyBoxImage = "busybox"
	DefaultNginxImage   = "nginx"
	DefaultMPIImage     = "volcanosh/example-mpi:0.0.3"
	DefaultTFImage      = "volcanosh/dist-mnist-tf-example:0.0.1"
	// "volcanosh/pytorch-mnist-v1beta1-9ee8fda-example:0.0.1" is from "docker.io/kubeflowkatib/pytorch-mnist:v1beta1-9ee8fda"
	DefaultPytorchImage = "volcanosh/pytorch-mnist-v1beta1-9ee8fda-example:0.0.1"
	DefaultRayImage     = "rayproject/ray:2.49.0"
	LogTimeFormat       = "[ 2006/01/02 15:04:05.000 ]"
)

func CPUResource(request string) v1.ResourceList {
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
	DeservedResource map[string]v1.ResourceList
	QueueParent      map[string]string
	PriorityClasses  map[string]int32
	UsingPlaceHolder bool
}

type Options struct {
	Namespace          string
	Queues             []string
	DeservedResource   map[string]v1.ResourceList
	QueueParent        map[string]string
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
		DeservedResource: o.DeservedResource,
		QueueParent:      o.QueueParent,
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

	// Clean up hypernodes first
	err := CleanupHyperNodes(ctx)
	Expect(err).NotTo(HaveOccurred(), "failed to clean up hypernodes")

	foreground := metav1.DeletePropagationForeground
	err = ctx.Kubeclient.CoreV1().Namespaces().Delete(context.TODO(), ctx.Namespace, metav1.DeleteOptions{
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

func GetPodList(ctx context.Context, c clientset.Interface, namespace string, ls *metav1.LabelSelector) *v1.PodList {
	selector, err := metav1.LabelSelectorAsSelector(ls)
	Expect(err).NotTo(HaveOccurred())
	podList, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	Expect(err).NotTo(HaveOccurred())
	return podList
}

// DumpTestContextIfFailed is a helper function to be used in JustAfterEach hooks
// It checks if the test failed and dumps the context if needed
func DumpTestContextIfFailed(ctx *TestContext, specReport SpecReport) {
	if !specReport.Failed() || ctx == nil {
		return
	}

	By("Dumping test context for failed test")
	DumpTestContext(ctx)
}

// DumpTestContext collects and dumps all relevant Kubernetes resources
// for a given namespace when a test fails. This provides complete cluster
// state snapshots for debugging test failures.
func DumpTestContext(ctx *TestContext) {
	artifactsPath := os.Getenv("ARTIFACTS_PATH")
	if artifactsPath == "" {
		artifactsPath = "./_artifacts"
	}

	// Create directory structure: test-context/{namespace}/
	nsPath := filepath.Join(artifactsPath, "test-context", ctx.Namespace)
	if err := os.MkdirAll(nsPath, 0755); err != nil {
		klog.Errorf("Failed to create artifacts directory: %v", err)
		return
	}

	klog.Infof("Dumping test context for namespace %s to %s", ctx.Namespace, nsPath)

	resourceDumpers := map[string]func() error{
		"pods":            func() error { return dumpPods(ctx, nsPath) },
		"podgroups":       func() error { return dumpPodGroups(ctx, nsPath) },
		"queues":          func() error { return dumpQueues(ctx, nsPath) },
		"priorityclasses": func() error { return dumpPriorityClasses(ctx, nsPath) },
		"VC jobs":         func() error { return dumpVCJobs(ctx, nsPath) },
		"VC cronjobs":     func() error { return dumpVCronJobs(ctx, nsPath) },
		"K8s jobs":        func() error { return dumpK8sJobs(ctx, nsPath) },
		"K8s cronjobs":    func() error { return dumpK8sCronJobs(ctx, nsPath) },
		"deployments":     func() error { return dumpDeployments(ctx, nsPath) },
		"statefulsets":    func() error { return dumpStatefulSets(ctx, nsPath) },
	}

	for name, dumper := range resourceDumpers {
		if err := dumper(); err != nil {
			klog.Errorf("Failed to dump %s: %v", name, err)
		}
	}

	klog.Infof("Finished dumping test context for namespace %s", ctx.Namespace)
}

// dumpPods dumps all pods in the namespace
func dumpPods(ctx *TestContext, path string) error {
	pods, err := ctx.Kubeclient.CoreV1().Pods(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}
	return writeResourceToFile(pods, filepath.Join(path, "pods.yaml"))
}

// dumpPodGroups dumps all podgroups in the namespace
func dumpPodGroups(ctx *TestContext, path string) error {
	pgs, err := ctx.Vcclient.SchedulingV1beta1().PodGroups(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list podgroups: %v", err)
	}
	return writeResourceToFile(pgs, filepath.Join(path, "podgroups.yaml"))
}

// dumpQueues dumps all queues (cluster-scoped resource)
func dumpQueues(ctx *TestContext, path string) error {
	var queues *schedulingv1beta1.QueueList
	var err error
	queues, err = ctx.Vcclient.SchedulingV1beta1().Queues().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list queues: %v", err)
	}
	return writeResourceToFile(queues, filepath.Join(path, "queues.yaml"))
}

// dumpPriorityClasses dumps all priority classes (cluster-scoped resource)
func dumpPriorityClasses(ctx *TestContext, path string) error {
	pcs, err := ctx.Kubeclient.SchedulingV1().PriorityClasses().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list priority classes: %v", err)
	}
	return writeResourceToFile(pcs, filepath.Join(path, "priorityclasses.yaml"))
}

// dumpVCJobs dumps all Volcano Jobs in the namespace
func dumpVCJobs(ctx *TestContext, path string) error {
	jobs, err := ctx.Vcclient.BatchV1alpha1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list VC jobs: %v", err)
	}
	return writeResourceToFile(jobs, filepath.Join(path, "vcjobs.yaml"))
}

// dumpVCronJobs dumps all Volcano CronJobs in the namespace
func dumpVCronJobs(ctx *TestContext, path string) error {
	cronjobs, err := ctx.Vcclient.BatchV1alpha1().CronJobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list VC cronjobs: %v", err)
	}
	return writeResourceToFile(cronjobs, filepath.Join(path, "vcronjobs.yaml"))
}

// dumpK8sJobs dumps all standard Kubernetes Jobs in the namespace
func dumpK8sJobs(ctx *TestContext, path string) error {
	jobs, err := ctx.Kubeclient.BatchV1().Jobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list K8s jobs: %v", err)
	}
	return writeResourceToFile(jobs, filepath.Join(path, "k8s-jobs.yaml"))
}

// dumpK8sCronJobs dumps all standard Kubernetes CronJobs in the namespace
func dumpK8sCronJobs(ctx *TestContext, path string) error {
	cronjobs, err := ctx.Kubeclient.BatchV1().CronJobs(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list K8s cronjobs: %v", err)
	}
	return writeResourceToFile(cronjobs, filepath.Join(path, "k8s-cronjobs.yaml"))
}

// dumpDeployments dumps all deployments in the namespace
func dumpDeployments(ctx *TestContext, path string) error {
	deployments, err := ctx.Kubeclient.AppsV1().Deployments(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %v", err)
	}
	return writeResourceToFile(deployments, filepath.Join(path, "deployments.yaml"))
}

// dumpStatefulSets dumps all statefulsets in the namespace
func dumpStatefulSets(ctx *TestContext, path string) error {
	statefulsets, err := ctx.Kubeclient.AppsV1().StatefulSets(ctx.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %v", err)
	}
	return writeResourceToFile(statefulsets, filepath.Join(path, "statefulsets.yaml"))
}

// writeResourceToFile writes a Kubernetes resource to a YAML file
func writeResourceToFile(obj any, filePath string) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal resource: %v", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %v", filePath, err)
	}

	klog.V(4).Infof("Dumped resource to %s", filePath)
	return nil
}
