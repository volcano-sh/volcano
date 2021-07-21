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
	"fmt"
	"sync"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("[Stress] Job Test", func() {
	var ctx *e2eutil.TestContext

	ginkgo.BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
	})

	ginkgo.AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	ginkgo.Context("[Sequential] with specific number of stress", func() {
		var wg sync.WaitGroup

		ginkgo.It("should create multiple jobs successfully", func() {
			for i := 0; i < e2eutil.NumStress; i++ {
				index := i
				wg.Add(1)
				go func() {
					defer ginkgo.GinkgoRecover()
					defer wg.Done()

					job := &e2eutil.JobSpec{
						Name: fmt.Sprintf("job-%d", index),
						Tasks: []e2eutil.TaskSpec{
							{
								Img: e2eutil.DefaultNginxImage,
								Req: e2eutil.HalfCPU,
								Min: 1,
								Rep: 1,
							},
						},
					}
					referenceJob := e2eutil.CreateJob(ctx, job)
					err := e2eutil.WaitTasksReady(ctx, referenceJob, 1)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}()
			}
			wg.Wait()
		})
	})
})
