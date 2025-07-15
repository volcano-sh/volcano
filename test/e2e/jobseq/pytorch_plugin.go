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

package jobseq

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Pytorch Plugin E2E Test", func() {
	It("will run and complete finally", func() {
		// Community CI can skip this use case, and enable this use case verification when releasing the version.
		Skip("Pytorch's test image download fails probabilistically, causing the current use case to fail. ")
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU

		spec := &e2eutil.JobSpec{
			Name: "pytorch-job",
			Min:  1,
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			Plugins: map[string][]string{
				"pytorch": {"--master=master", "--worker=worker", "--port=23456"},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:       "master",
					Img:        e2eutil.DefaultPytorchImage,
					Req:        slot,
					Min:        1,
					Rep:        1,
					WorkingDir: "/home",
					// Need sometime waiting for worker node ready
					Command: `python3 /opt/pytorch-mnist/mnist.py --epochs=1`,
				},
				{
					Name:       "worker",
					Img:        e2eutil.DefaultPytorchImage,
					Req:        slot,
					Min:        2,
					Rep:        2,
					WorkingDir: "/home",
					Command:    "python3 /opt/pytorch-mnist/mnist.py --epochs=1",
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())
	})
})
