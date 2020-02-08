/*
Copyright 2018 The Volcano Authors.

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

package e2e

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Queue E2E Test", func() {
	It("Reclaim", func() {
		context := initTestContext(options{
			queues: []string{defaultQueue1, defaultQueue2},
		})
		defer cleanupTestContext(context)

		slot := oneCPU
		rep := clusterSize(context, slot)

		spec := &jobSpec{
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: 1,
					rep: rep,
				},
			},
		}

		spec.name = "q1-qj-1"
		spec.queue = defaultQueue1
		job1 := createJob(context, spec)
		err := waitJobReady(context, job1)
		Expect(err).NotTo(HaveOccurred())

		err = waitQueueStatus(func() (bool, error) {
			queue, err := context.vcclient.SchedulingV1beta1().Queues().Get(defaultQueue1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			Expect(err).NotTo(HaveOccurred())
		}

		spec.name = "q2-qj-2"
		spec.queue = defaultQueue2
		job2 := createJob(context, spec)
		err = waitTasksReady(context, job2, expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1, expected)
		Expect(err).NotTo(HaveOccurred())

		// Test Queue status
		spec = &jobSpec{
			name:  "q1-qj-2",
			queue: defaultQueue1,
			tasks: []taskSpec{
				{
					img: defaultNginxImage,
					req: slot,
					min: rep * 2,
					rep: rep * 2,
				},
			},
		}
		job3 := createJob(context, spec)
		err = waitJobStatePending(context, job3)
		Expect(err).NotTo(HaveOccurred())
		err = waitQueueStatus(func() (bool, error) {
			queue, err := context.vcclient.SchedulingV1beta1().Queues().Get(defaultQueue1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Pending == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

})
