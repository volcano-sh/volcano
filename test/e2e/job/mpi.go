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

package job

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vcbus "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
)

var _ = Describe("MPI E2E Test", func() {
	It("will run and complete finally", func() {
		context := initTestContext(options{})
		defer cleanupTestContext(context)

		slot := oneCPU

		spec := &jobSpec{
			name: "mpi",
			policies: []vcbatch.LifecyclePolicy{
				{
					Action: vcbus.CompleteJobAction,
					Event:  vcbus.TaskCompletedEvent,
				},
			},
			plugins: map[string][]string{
				"ssh": {},
				"env": {},
				"svc": {},
			},
			tasks: []taskSpec{
				{
					name:       "mpimaster",
					img:        defaultMPIImage,
					req:        slot,
					min:        1,
					rep:        1,
					workingDir: "/home",
					//Need sometime waiting for worker node ready
					command: `sleep 5;
mkdir -p /var/run/sshd; /usr/sbin/sshd;
mpiexec --allow-run-as-root --hostfile /etc/volcano/mpiworker.host -np 2 mpi_hello_world > /home/re`,
				},
				{
					name:       "mpiworker",
					img:        defaultMPIImage,
					req:        slot,
					min:        2,
					rep:        2,
					workingDir: "/home",
					command:    "mkdir -p /var/run/sshd; /usr/sbin/sshd -D;",
				},
			},
		}

		job := createJob(context, spec)

		err := waitJobPhases(context, job, []vcbatch.JobPhase{
			vcbatch.Pending, vcbatch.Running, vcbatch.Completing, vcbatch.Completed})
		Expect(err).NotTo(HaveOccurred())
	})

})
