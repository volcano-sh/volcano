/*
Copyright 2019 The Volcano Authors.

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
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

type listFlags struct {
	commonFlags
}

const (
	// Weight of the queue
	Weight string = "Weight"

	// Name of queue
	Name string = "Name"

	// Pending status of the queue
	Pending string = "Pending"

	// Running status of the queue
	Running string = "Running"

	// Unknown status of the queue
	Unknown string = "Unknown"

	// Inqueue status of queue
	Inqueue string = "Inqueue"

	// State is state of queue
	State string = "State"
)

var listQueueFlags = &listFlags{}

// InitListFlags inits all flags.
func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listQueueFlags.commonFlags)
}

// ListQueue lists all the queue.
func ListQueue() error {
	config, err := buildConfig(listQueueFlags.Master, listQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	queues, err := jobClient.SchedulingV1beta1().Queues().List(context.TODO(), metav1.ListOptions{})
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

// PrintQueues prints queue information.
func PrintQueues(queues *v1beta1.QueueList, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-8s%-8s%-8s%-8s%-8s%-8s\n",
		Name, Weight, State, Inqueue, Pending, Running, Unknown)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}
	for _, queue := range queues.Items {
		_, err = fmt.Fprintf(writer, "%-25s%-8d%-8s%-8d%-8d%-8d%-8d\n",
			queue.Name, queue.Spec.Weight, queue.Status.State, queue.Status.Inqueue,
			queue.Status.Pending, queue.Status.Running, queue.Status.Unknown)
		if err != nil {
			fmt.Printf("Failed to print queue command result: %s.\n", err)
		}
	}

}
