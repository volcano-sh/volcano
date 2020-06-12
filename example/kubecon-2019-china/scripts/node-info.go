package main

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	config, err := clientcmd.BuildConfigFromFlags("", "C:/Users/m00483107/.kube/config")
	if err != nil {
		panic(err)
	}
	kubeClient := kubernetes.NewForConfigOrDie(config)
	nodes, err := kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	pods, err := kubeClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	fmt.Print("\n--------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf(" %-13s  |", " ")
	for _, n := range nodes.Items {
		fmt.Printf(" %-20s  |", n.Name)
	}
	fmt.Print("\n--------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf(" %-13s  |", "Alloctable")
	for _, n := range nodes.Items {
		res := fmt.Sprintf("cpu: %s", n.Status.Allocatable.Cpu())
		fmt.Printf(" %-20s  |", res)
	}
	fmt.Println()
	fmt.Printf(" %-13s  |", " ")
	for _, n := range nodes.Items {
		res := fmt.Sprintf("mem: %s", n.Status.Allocatable.Memory())
		fmt.Printf(" %-20s  |", res)
	}
	fmt.Print("\n--------------------------------------------------------------------------------------------------------------\n")

	podMap := map[string]*api.Resource{}

	for _, p := range pods.Items {
		nodeName := p.Spec.NodeName
		// Only account running pods here.
		if p.Status.Phase == v1.PodSucceeded || p.Status.Phase == v1.PodFailed {
			continue
		}
		if len(nodeName) == 0 {
			continue
		}
		if _, found := podMap[nodeName]; !found {
			podMap[nodeName] = api.EmptyResource()
		}
		res := api.GetPodResourceWithoutInitContainers(&p)
		podMap[nodeName].Add(res)
	}

	fmt.Printf(" %-13s  |", "Idle")
	for _, n := range nodes.Items {
		allocate := n.Status.Allocatable.DeepCopy()
		c := allocate.Cpu()
		if r, found := podMap[n.Name]; found {
			cpu := c.MilliValue() - int64(r.MilliCPU)
			c.SetMilli(cpu)
		}

		res := fmt.Sprintf("cpu: %s", c)
		fmt.Printf(" %-20s  |", res)
	}
	fmt.Println()
	fmt.Printf(" %-13s  |", " ")
	for _, n := range nodes.Items {
		allocate := n.Status.Allocatable.DeepCopy()
		c := allocate.Memory()
		if r, found := podMap[n.Name]; found {
			cpu := c.Value() - int64(r.Memory)
			c.Set(cpu)
		}

		res := fmt.Sprintf("mem: %s", c)
		fmt.Printf(" %-20s  |", res)
	}
	fmt.Print("\n--------------------------------------------------------------------------------------------------------------\n")
}
