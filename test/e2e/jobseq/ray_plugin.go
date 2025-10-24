/*
Copyright 2025 The Volcano Authors.

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

package jobseq

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Ray Plugin E2E Test", func() {
	BeforeEach(func() {
		By("Prune images before test")
		PruneUnusedImagesOnAllNodes(e2eutil.KubeClient)
	})
	AfterEach(func() {
		By("Prune images after test")
		PruneUnusedImagesOnAllNodes(e2eutil.KubeClient)
	})

	It("Will Start in pending state and  get running phase", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(ctx)

		slot := e2eutil.OneCPU

		spec := &e2eutil.JobSpec{
			Name: "ray-cluster-job",
			Min:  1,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Event:  vcbus.PodEvictedEvent,
					Action: vcbus.RestartJobAction,
				},
			},
			Plugins: map[string][]string{
				"ray": {"--head=head", "--worker=worker", "--port=2345", "--headContainer=ray", "--workerContainer=ray"},
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "head",
					Img:  e2eutil.DefaultRayImage,
					Req:  slot,
					Min:  1,
					Rep:  1,
				},
				{
					Name: "worker",
					Img:  e2eutil.DefaultRayImage,
					Req:  slot,
					Min:  1,
					Rep:  1,
				},
			},
		}

		job := e2eutil.CreateJob(ctx, spec)

		By("checking if Service for ray job is created")
		Eventually(func() error {
			_, err := ctx.Kubeclient.CoreV1().
				Services(job.Namespace).
				Get(context.TODO(), job.Name, metav1.GetOptions{})
			return err
		}, "60s", "2s").Should(Succeed())

		err := e2eutil.WaitJobPhases(ctx, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running})
		Expect(err).NotTo(HaveOccurred())
	})
})
