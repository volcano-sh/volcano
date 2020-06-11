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

package queue

import (
	"context"
	"fmt"

	"volcano.sh/volcano/pkg/client/clientset/versioned"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type deleteFlags struct {
	commonFlags

	// Name is name of queue
	Name string
}

var deleteQueueFlags = &deleteFlags{}

// InitDeleteFlags is used to init all flags during queue deleting.
func InitDeleteFlags(cmd *cobra.Command) {
	initFlags(cmd, &deleteQueueFlags.commonFlags)

	cmd.Flags().StringVarP(&deleteQueueFlags.Name, "name", "n", "", "the name of queue")
}

// DeleteQueue delete queue.
func DeleteQueue() error {
	config, err := buildConfig(deleteQueueFlags.Master, deleteQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if len(deleteQueueFlags.Name) == 0 {
		return fmt.Errorf("Queue name must be specified")
	}

	queueClient := versioned.NewForConfigOrDie(config)
	return queueClient.SchedulingV1beta1().Queues().Delete(context.TODO(), deleteQueueFlags.Name, metav1.DeleteOptions{})
}
