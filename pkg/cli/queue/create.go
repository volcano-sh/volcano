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
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vkapi "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
)

type createFlags struct {
	commonFlags

	Name   string
	Weight int32
}

var createQueueFlags = &createFlags{}

// InitRunFlags is used to init all run flags
func InitRunFlags(cmd *cobra.Command) {
	initFlags(cmd, &createQueueFlags.commonFlags)

	cmd.Flags().StringVarP(&createQueueFlags.Name, "name", "n", "test", "the name of queue")
	cmd.Flags().Int32VarP(&createQueueFlags.Weight, "weight", "w", 1, "the weight of the queue")

}

// CreateQueue creates queue
func CreateQueue() error {
	config, err := buildConfig(createQueueFlags.Master, createQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	queue := &vkapi.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: createQueueFlags.Name,
		},
		Spec: vkapi.QueueSpec{
			Weight: int32(createQueueFlags.Weight),
		},
	}

	queueClient := versioned.NewForConfigOrDie(config)
	if _, err := queueClient.SchedulingV1alpha1().Queues().Create(queue); err != nil {
		return err
	}

	return nil
}
