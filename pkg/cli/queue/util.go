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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// Initialize client auth plugin.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"

	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/apis/pkg/client/clientset/versioned"
)

func createQueueCommand(ctx context.Context, config *rest.Config, action busv1alpha1.Action) error {
	queueClient := versioned.NewForConfigOrDie(config)
	queue, err := queueClient.SchedulingV1beta1().Queues().Get(ctx, operateQueueFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	ctrlRef := metav1.NewControllerRef(queue, helpers.V1beta1QueueKind)
	cmd := &busv1alpha1.Command{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-",
				queue.Name, strings.ToLower(string(action))),
			OwnerReferences: []metav1.OwnerReference{
				*ctrlRef,
			},
		},
		TargetObject: ctrlRef,
		Action:       string(action),
	}

	if _, err := queueClient.BusV1alpha1().Commands("default").Create(ctx, cmd, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}
