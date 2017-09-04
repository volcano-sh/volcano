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
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubernetes-incubator/kube-arbitrator/cmd/app"
	"github.com/kubernetes-incubator/kube-arbitrator/cmd/app/options"
	examplecontroller "github.com/kubernetes-incubator/kube-arbitrator/pkg/controller"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

func main() {
	s := options.NewServerOption()
	s.AddFlags(pflag.CommandLine)

	if err := app.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// testCacheMain()
	// panic("The kube-arbitrator is not ready now.")
}

func testCacheMain() {
	kubeconfig := flag.String("kubeconfig", "/var/run/kubernetes/admin.kubeconfig", "Path to a kube config. Only required if out-of-cluster.")
	flag.Parse()

	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	config, err := buildConfig(*kubeconfig)
	if err != nil {
		panic(err)
	}

	c := &examplecontroller.SchedulerCacheController{
		SchedulerCacheControllerConfig: config,
		SchedulerCache:                 schedulercache.New(),
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go c.Run(ctx)

	testDump := func() {
		for {
			time.Sleep(10 * time.Second)
			snapshot := c.SchedulerCache.Dump()

			fmt.Printf("====== DUMP NODES size %d\n", len(snapshot.Nodes))
			for key, value := range snapshot.Nodes {
				if n := value.Node(); n != nil {
					fmt.Printf("====== DUMP NODES, KBY (%s), VALUE name(%s) namespace(%s)\n", key, value.Node().Name, value.Node().Namespace)
					fmt.Println("           Capacity")
					for k, v := range value.Node().Status.Capacity {
						fmt.Printf("               %s: %s\n", k, v.String())
					}
					fmt.Println("           Allocatable")
					for k, v := range value.Node().Status.Allocatable {
						fmt.Printf("               %s: %s\n", k, v.String())
					}
				} else {
					fmt.Printf("====== DUMP NODES, KBY (%s)\n", key)
				}
			}
			fmt.Printf("====== DUMP PODS size %d\n", len(snapshot.Pods))
			for key, value := range snapshot.Pods {
				fmt.Printf("====== DUMP PODS, KEY (%s), VALUE name(%s) namespace(%s) status(%s)\n", key, value.Pod().Name, value.Pod().Namespace, value.Pod().Status.Phase)
				fmt.Printf("           InitContainers size(%d)\n", len(value.Pod().Spec.InitContainers))
				for i, ic := range value.Pod().Spec.InitContainers {
					fmt.Printf("                Container (%d)\n", i)
					fmt.Println("                     limit")
					for k, v := range ic.Resources.Limits {
						fmt.Printf("                          %s: %s\n", k, v.String())
					}
					fmt.Println("                     requests")
					for k, v := range ic.Resources.Requests {
						fmt.Printf("                          %s: %s\n", k, v.String())
					}
				}
				fmt.Printf("           Containers size(%d)\n", len(value.Pod().Spec.Containers))
				for i, ic := range value.Pod().Spec.Containers {
					fmt.Printf("                Container (%d)\n", i)
					fmt.Println("                     limit")
					for k, v := range ic.Resources.Limits {
						fmt.Printf("                          %s: %s\n", k, v.String())
					}
					fmt.Println("                     requests")
					for k, v := range ic.Resources.Requests {
						fmt.Printf("                          %s: %s\n", k, v.String())
					}
				}
			}
			fmt.Printf("====== DUMP ALLOCATORS size %d\n", len(snapshot.Allocators))
			for key, value := range snapshot.Allocators {
				fmt.Printf("====== DUMP ALLOCATORS, KEY (%s), VALUE(%s) \n", key, value.Name)
			}
		}
	}
	go testDump()

	time.Sleep(3600 * time.Second)

	panic("unreachable!")
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
