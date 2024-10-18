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

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
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

	// Completed status of the queue
	Completed string = "Completed"

	// State is state of queue
	State string = "State"
)

var listQueueFlags = &listFlags{}

// InitListFlags inits all flags.
func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listQueueFlags.commonFlags)
}

// ListQueue lists all the queue.
func ListQueue(ctx context.Context) error {
	config, err := buildConfig(listQueueFlags.Master, listQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	queues, err := jobClient.SchedulingV1beta1().Queues().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(queues.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}

	// Currently CRD does not support filtering directly from kube-apiserver using fieldSelector,
	// The featuregate called CustomResourceFieldSelectors is turned off by default in kube-apiserver
	// and is still in alpha in v1.30. Therefore we can only get all the podgroups from kube-apiserver
	// and then filtering them.
	pgList, err := jobClient.SchedulingV1beta1().PodGroups("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list podgroups with err: %v", err)
	}

	queueStats := make(map[string]*podGroupStatistics, len(queues.Items))
	for _, queue := range queues.Items {
		queueStats[queue.Name] = &podGroupStatistics{}
	}

	for _, pg := range pgList.Items {
		queueStats[pg.Spec.Queue].statPodGroupCountsForQueue(&pg)
	}

	PrintQueues(queues, queueStats, os.Stdout)

	return nil
}

// PrintQueues prints queue information.
func PrintQueues(queues *v1beta1.QueueList, queueStats map[string]*podGroupStatistics, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-8s%-8s%-8s%-8s%-8s%-8s%-8s\n",
		Name, Weight, State, Inqueue, Pending, Running, Unknown, Completed)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}

	for _, queue := range queues.Items {
		_, err = fmt.Fprintf(writer, "%-25s%-8d%-8s%-8d%-8d%-8d%-8d%-8d\n",
			queue.Name, queue.Spec.Weight, queue.Status.State, queueStats[queue.Name].inqueue, queueStats[queue.Name].pending,
			queueStats[queue.Name].running, queueStats[queue.Name].unknown, queueStats[queue.Name].completed)
		if err != nil {
			fmt.Printf("Failed to print queue command result: %s.\n", err)
		}
	}
}
