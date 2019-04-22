/*
Copyright 2019 The Kubernetes Authors.

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

package queue

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type listFlags struct {
	commonFlags
}

const (
	// Weight of the queue
	Weight string = "Weight"

	// Name of queue
	Name string = "Name"
)

var listQueueFlags = &listFlags{}

// InitListFlags inits all flags
func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listQueueFlags.commonFlags)
}

// ListQueue lists all the queue
func ListQueue() error {
	config, err := buildConfig(listQueueFlags.Master, listQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	queues, err := jobClient.SchedulingV1alpha1().Queues().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(queues.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintQueues(queues, os.Stdout)

	return nil
}

// PrintQueues prints queue information
func PrintQueues(queues *v1alpha1.QueueList, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-8s\n",
		Name, Weight)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}
	for _, queue := range queues.Items {
		_, err = fmt.Fprintf(writer, "%-25s%-8d\n",
			queue.Name, queue.Spec.Weight)
		if err != nil {
			fmt.Printf("Failed to print queue command result: %s.\n", err)
		}
	}

}
