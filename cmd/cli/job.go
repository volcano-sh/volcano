package main

import (
	"github.com/spf13/cobra"

	"hpw.cloud/volcano/pkg/cli/job"
)

func buildJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use: "job",
	}

	jobRunCmd := &cobra.Command{
		Use: "run",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.RunJob())
		},
	}
	job.InitRunFlags(jobRunCmd)
	jobCmd.AddCommand(jobRunCmd)

	jobListCmd := &cobra.Command{
		Use: "list",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ListJobs())
		},
	}
	job.InitListFlags(jobListCmd)
	jobCmd.AddCommand(jobListCmd)

	jobSuspendCmd := &cobra.Command{
		Use: "suspend",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.SuspendJob())
		},
	}
	job.InitSuspendFlags(jobSuspendCmd)
	jobCmd.AddCommand(jobSuspendCmd)

	jobResumeCmd := &cobra.Command{
		Use: "resume",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ResumeJob())
		},
	}
	job.InitResumeFlags(jobResumeCmd)
	jobCmd.AddCommand(jobResumeCmd)

	return jobCmd
}
