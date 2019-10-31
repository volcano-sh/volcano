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
	"fmt"

	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	"volcano.sh/volcano/pkg/client/clientset/versioned"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ActionOpen is `open` action
	ActionOpen = "open"
	// ActionClose is `close` action
	ActionClose = "close"
	// ActionUpdate is `update` action
	ActionUpdate = "update"
)

type operateFlags struct {
	commonFlags

	// Name is name of queue
	Name string
	// Weight is weight of queue
	Weight int32
	// Action is operation action of queue
	Action string
}

var operateQueueFlags = &operateFlags{}

// InitOperateFlags is used to init all run flags
func InitOperateFlags(cmd *cobra.Command) {
	initFlags(cmd, &operateQueueFlags.commonFlags)

	cmd.Flags().StringVarP(&operateQueueFlags.Name, "name", "n", "", "the name of queue")
	cmd.Flags().Int32VarP(&operateQueueFlags.Weight, "weight", "w", 1, "the weight of the queue")
	cmd.Flags().StringVarP(&operateQueueFlags.Action, "action", "a", "",
		"operate action to queue, valid actions are open, close, update")
}

// OperateQueue operates queue
func OperateQueue() error {
	config, err := buildConfig(operateQueueFlags.Master, operateQueueFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if len(operateQueueFlags.Name) == 0 {
		return fmt.Errorf("Queue name must be specified")
	}

	queueClient := versioned.NewForConfigOrDie(config)
	queue, err := queueClient.SchedulingV1alpha2().Queues().Get(operateQueueFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	switch operateQueueFlags.Action {
	case ActionOpen:
		queue.Spec.State = v1alpha2.QueueStateOpen
	case ActionClose:
		queue.Spec.State = v1alpha2.QueueStateClosed
	case ActionUpdate:
		if operateQueueFlags.Weight == 0 {
			return fmt.Errorf("When %s queue %s, weight must be specified, "+
				"the value must be greater than 0", ActionUpdate, operateQueueFlags.Name)
		}

		queue.Spec.Weight = int32(operateQueueFlags.Weight)
	case "":
		return fmt.Errorf("Action can not be null")
	default:
		return fmt.Errorf("Action %s invalid, valid actions are %s and %s",
			operateQueueFlags.Action, ActionOpen, ActionClose)
	}

	_, err = queueClient.SchedulingV1alpha2().Queues().Update(queue)

	return err
}
