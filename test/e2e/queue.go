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

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Queue E2E Test", func() {
	ginkgo.It("Reclaim", func() {
		context := initTestContext()
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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitQueueStatus(func() (bool, error) {
			queue, err := context.kbclient.SchedulingV1alpha1().Queues().Get(defaultQueue1, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return queue.Status.Running == 1, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		expected := int(rep) / 2
		// Reduce one pod to tolerate decimal fraction.
		if expected > 1 {
			expected--
		} else {
			err := fmt.Errorf("expected replica <%d> is too small", expected)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		spec.name = "q2-qj-2"
		spec.queue = defaultQueue2
		job2 := createJob(context, spec)
		err = waitTasksReady(context, job2, expected)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = waitTasksReady(context, job1, expected)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

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
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = waitQueueStatus(func() (bool, error) {
			queue, err := context.kbclient.SchedulingV1alpha1().Queues().Get(defaultQueue1, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return queue.Status.Pending == 1, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

})
