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

		slot := halfCPU
		rep := clusterSize(context, slot)

		var job1Expected, job2Expected int
		num := getPodNumQuota(context, slot)
		// Do not consider the impact of the default queue
		// Even if the queue is reclaimed, the queue's allocated resources is still
		// greater than the reserved resources.
		if int(rep) <= num/2+1 {
			err := fmt.Errorf("cluster resource insufficient, reclaim will not happen. "+
				"Pod quota of queue <%s> is <%d>, actually avaliable schedulable pod number is <%d>, "+
				"scheduled pod number is not more than quota", defaultQueue1, num/2+1, rep)
			Expect(err).NotTo(HaveOccurred())
		} else {
			job1Expected = num/2 + 1
			job2Expected = int(rep) - job1Expected
		}

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
			queue, err := context.vcclient.SchedulingV1alpha2().Queues().Get(defaultQueue1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Running == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())

		spec.name = "q2-qj-2"
		spec.queue = defaultQueue2
		job2 := createJob(context, spec)
		err = waitTasksReady(context, job2, job2Expected)
		Expect(err).NotTo(HaveOccurred())

		err = waitTasksReady(context, job1, job1Expected)
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
			queue, err := context.vcclient.SchedulingV1alpha2().Queues().Get(defaultQueue1, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Status.Pending == 1, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
