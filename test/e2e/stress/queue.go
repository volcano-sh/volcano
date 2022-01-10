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

package stress

import (
	"context"
	"fmt"
	"sync"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("[Stress] Queue Test", func() {
	var ctx *e2eutil.TestContext

	ginkgo.BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
	})

	ginkgo.AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	ginkgo.Context("[Sequential] with specific number of stress", func() {
		var wg sync.WaitGroup

		ginkgo.It("should create multiple queues successfully", func() {
			for i := 0; i < e2eutil.NumStress; i++ {
				index := i
				wg.Add(1)
				go func() {
					defer ginkgo.GinkgoRecover()
					defer wg.Done()

					queueName := fmt.Sprintf("queue-%d", index)
					e2eutil.CreateQueue(ctx, queueName)
					err := e2eutil.WaitQueueStatus(func() (bool, error) {
						queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), queueName, metav1.GetOptions{})
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						return queue.Status.State == schedulingv1beta1.QueueStateOpen, nil
					})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}()
			}
			wg.Wait()
		})

		ginkgo.It("should delete multiple queues successfully", func() {
			for i := 0; i < e2eutil.NumStress; i++ {
				index := i
				wg.Add(1)
				go func() {
					defer ginkgo.GinkgoRecover()
					defer wg.Done()

					e2eutil.DeleteQueue(ctx, fmt.Sprintf("queue-%d", index))
				}()
			}
			wg.Wait()
		})
	})
})
