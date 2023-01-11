/*
Copyright 2022 The Volcano Authors.

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

var _ = Describe("ssh Plugin E2E Test", func() {
	It("will run and complete finally", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU

		spec := &e2eutil.JobSpec{
			Name: "ssh",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			Plugins: map[string][]string{
				"ssh": {"--port=1000"},
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name: "client",
					Img:  e2eutil.DefaultMPIImage,
					Req:  slot,
					Min:  1,
					Rep:  1,
					// Need sometime waiting for worker node ready
					Command: `sleep 5;
touch a.txt;
scp -P 1000 a.txt ssh-server-0.ssh:~;`,
				},
				{
					Name:    "server",
					Img:     e2eutil.DefaultMPIImage,
					Req:     slot,
					Min:     1,
					Rep:     1,
					Command: "mkdir -p /var/run/sshd; /usr/sbin/sshd -p 1000 -D;",
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completing, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())
	})
})
