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

package jobseq

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("MPI E2E Test", func() {
	It("will run and complete finally", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU

		spec := &e2eutil.JobSpec{
			Name: "mpi",
			Policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			Plugins: map[string][]string{
				"ssh": {},
				"env": {},
				"svc": {},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:       "mpimaster",
					Img:        e2eutil.DefaultMPIImage,
					Req:        slot,
					Min:        1,
					Rep:        1,
					WorkingDir: "/home",
					// Need sometime waiting for worker node ready
					Command: `sleep 5;
mkdir -p /var/run/sshd; /usr/sbin/sshd;
mpiexec --allow-run-as-root --hostfile /etc/volcano/mpiworker.host -np 2 mpi_hello_world > /home/re`,
				},
				{
					Name:       "mpiworker",
					Img:        e2eutil.DefaultMPIImage,
					Req:        slot,
					Min:        2,
					Rep:        2,
					WorkingDir: "/home",
					Command:    "mkdir -p /var/run/sshd; /usr/sbin/sshd -D;",
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)

		err := e2eutil.WaitJobPhases(context, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completing, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())
	})

})
