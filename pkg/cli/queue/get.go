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

type getFlags struct {
	commonFlags

	Name string
}

var getQueueFlags = &getFlags{}

// InitGetFlags is used to init all flags.
func InitGetFlags(cmd *cobra.Command) {
	initFlags(cmd, &getQueueFlags.commonFlags)

	cmd.Flags().StringVarP(&getQueueFlags.Name, "name", "n", "", "the name of queue")

}

// GetQueue gets a queue.
func GetQueue() error {
	config, err := buildConfig(getQueueFlags.Master, getQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if getQueueFlags.Name == "" {
		err := fmt.Errorf("name is mandatory to get the particular queue details")
		return err
	}

	queueClient := versioned.NewForConfigOrDie(config)
	queue, err := queueClient.SchedulingV1beta1().Queues().Get(context.TODO(), getQueueFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	PrintQueue(queue, os.Stdout)

	return nil
}

// PrintQueue prints queue information.
func PrintQueue(queue *v1beta1.Queue, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-8s%-8s%-8s%-8s%-8s%-8s\n",
		Name, Weight, State, Inqueue, Pending, Running, Unknown)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}
	_, err = fmt.Fprintf(writer, "%-25s%-8d%-8s%-8d%-8d%-8d%-8d\n",
		queue.Name, queue.Spec.Weight, queue.Status.State, queue.Status.Inqueue,
		queue.Status.Pending, queue.Status.Running, queue.Status.Unknown)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}
}
