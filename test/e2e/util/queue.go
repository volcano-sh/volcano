/*
Copyright 2021 The Volcano Authors.

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

package util

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

func CreateQueues(cxt *TestContext) {
	for _, q := range cxt.Queues {

		_, err := cxt.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})

		//TODO: Better not found error
		if err != nil {
			_, err = cxt.Vcclient.SchedulingV1beta1().Queues().Create(context.TODO(), &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: q,
				},
				Spec: schedulingv1beta1.QueueSpec{
					Weight: 1,
				},
			}, metav1.CreateOptions{})
		}

		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteQueues(cxt *TestContext) {
	foreground := metav1.DeletePropagationForeground

	for _, q := range cxt.Queues {
		queue, err := cxt.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		queue.Status.State = schedulingv1beta1.QueueStateClosed
		_, err = cxt.Vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), queue, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		err = wait.Poll(100*time.Millisecond, FiveMinute, queueClosed(cxt, q))
		Expect(err).NotTo(HaveOccurred())

		err = cxt.Vcclient.SchedulingV1beta1().Queues().Delete(context.TODO(), q,
			metav1.DeleteOptions{
				PropagationPolicy: &foreground,
			})

		Expect(err).NotTo(HaveOccurred())
	}
}

func SetQueueReclaimable(cxt *TestContext, queues []string, reclaimable bool) {
	for _, q := range queues {
		queue, err := cxt.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), q, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get queue")
		queue.Spec.Reclaimable = &reclaimable
		_, err = cxt.Vcclient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to update queue")
	}
}

func WaitQueueStatus(condition func() (bool, error)) error {
	return wait.Poll(100*time.Millisecond, FiveMinute, condition)
}

func queueClosed(ctx *TestContext, name string) wait.ConditionFunc {
	return func() (bool, error) {
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if queue.Status.State != schedulingv1beta1.QueueStateClosed {
			return false, nil
		}

		return true, nil
	}
}
