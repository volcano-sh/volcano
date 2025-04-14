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
	"volcano.sh/volcano/pkg/cli/podgroup"
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
func GetQueue(ctx context.Context) error {
	config, err := buildConfig(getQueueFlags.Master, getQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if getQueueFlags.Name == "" {
		err := fmt.Errorf("name is mandatory to get the particular queue details")
		return err
	}

	queueClient := versioned.NewForConfigOrDie(config)
	queue, err := queueClient.SchedulingV1beta1().Queues().Get(ctx, getQueueFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Although the featuregate called CustomResourceFieldSelectors is enabled by default after v1.31, there are still
	// users using k8s versions lower than v1.31. Therefore we can only get all the podgroups from kube-apiserver
	// and then filtering them.
	pgList, err := queueClient.SchedulingV1beta1().PodGroups("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list podgroup for queue %s with err: %v", getQueueFlags.Name, err)
	}

	pgStats := &podgroup.PodGroupStatistics{}
	for _, pg := range pgList.Items {
		if pg.Spec.Queue == getQueueFlags.Name {
			pgStats.StatPodGroupCountsForQueue(&pg)
		}
	}

	PrintQueue(queue, pgStats, os.Stdout)

	return nil
}

// PrintQueue prints queue information.
func PrintQueue(queue *v1beta1.Queue, pgStats *podgroup.PodGroupStatistics, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-8s%-8s%-8s%-8s%-8s%-8s%-8s%-8s\n",
		Name, Weight, State, Parent, Inqueue, Pending, Running, Unknown, Completed)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}

	_, err = fmt.Fprintf(writer, "%-25s%-8d%-8s%-8s%-8d%-8d%-8d%-8d%-8d\n",
		queue.Name, queue.Spec.Weight, queue.Status.State, queue.Spec.Parent, pgStats.Inqueue,
		pgStats.Pending, pgStats.Running, pgStats.Unknown, pgStats.Completed)
	if err != nil {
		fmt.Printf("Failed to print queue command result: %s.\n", err)
	}
}
