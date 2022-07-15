package jobseq

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vcbatchv1 "volcano.sh/apis/pkg/apis/batch/v1"
	vcbusv1 "volcano.sh/apis/pkg/apis/bus/v1"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("Pytorch Plugin E2E Test", func() {
	It("will run and complete finally", func() {
		context := e2eutil.InitTestContext(e2eutil.Options{})
		defer e2eutil.CleanupTestContext(context)

		slot := e2eutil.OneCPU

		spec := &e2eutil.JobSpec{
			Name: "pytorch-job",
			Min:  1,
			Policies: []vcbatchv1.LifecyclePolicy{
				{
					Action: vcbusv1.CompleteJobAction,
					Event:  vcbusv1.TaskCompletedEvent,
				},
			},
			Plugins: map[string][]string{
				"pytorch": {"--master=master", "--worker=worker", "--port=23456"},
			},
			Tasks: []e2eutil.TaskSpec{
				{
					Name:       "master",
					Img:        "docker.io/kubeflowkatib/pytorch-mnist:v1beta1-45c5727",
					Req:        slot,
					Min:        1,
					Rep:        1,
					WorkingDir: "/home",
					// Need sometime waiting for worker node ready
					Command: `python3 /opt/pytorch-mnist/mnist.py --epochs=1`,
				},
				{
					Name:       "worker",
					Img:        "docker.io/kubeflowkatib/pytorch-mnist:v1beta1-45c5727",
					Req:        slot,
					Min:        2,
					Rep:        2,
					WorkingDir: "/home",
					Command:    "python3 /opt/pytorch-mnist/mnist.py --epochs=1",
				},
			},
		}

		job := e2eutil.CreateJob(context, spec)
		err := e2eutil.WaitJobPhases(context, job, []vcbatchv1.JobPhase{
			vcbatchv1.Pending, vcbatchv1.Running, vcbatchv1.Completed})
		Expect(err).NotTo(HaveOccurred())
	})
})
