package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

var _ = Describe("MPI E2E Test", func() {
	It("will run and complete finally", func() {
		context := initTestContext()
		defer cleanupTestContext(context)

		slot := oneCPU

		spec := &jobSpec{
			name: "mpi",
			policies: []vkv1.LifecyclePolicy{
				{
					Action: vkv1.CompleteJobAction,
					Event:  vkv1.TaskCompletedEvent,
				},
			},
			plugins: map[string][]string{
				"ssh": []string{},
				"env": []string{},
			},
			tasks: []taskSpec{
				{
					name:       "mpimaster",
					img:        defaultMPIImage,
					req:        slot,
					min:        1,
					rep:        1,
					workingDir: "/home",
					command: `sleep 3;
MPI_HOST=` + "`" + `cat /etc/volcano/mpiworker.host | tr "\n" ","` + "`" + `;
mkdir -p /var/run/sshd; /usr/sbin/sshd;
mpiexec --allow-run-as-root --host ${MPI_HOST} -np 2 mpi_hello_world > /home/re`,
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

		err := waitJobStates(context, job, []vkv1.JobPhase{
			vkv1.Pending, vkv1.Running, vkv1.Completing, vkv1.Completed})
		Expect(err).NotTo(HaveOccurred())
	})

})
