package main

import (
	"github.com/spf13/cobra"

	"volcano.sh/volcano/pkg/cli/job"
)

func buildJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "vcctl command line operation job",
	}

	jobRunCmd := &cobra.Command{
		Use:   "run",
		Short: "run job by parameters from the command line",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.RunJob())
		},
	}
	job.InitRunFlags(jobRunCmd)
	jobCmd.AddCommand(jobRunCmd)

	jobListCmd := &cobra.Command{
		Use:   "list",
		Short: "list job information",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ListJobs())
		},
	}
	job.InitListFlags(jobListCmd)
	jobCmd.AddCommand(jobListCmd)

	jobViewCmd := &cobra.Command{
		Use:   "view",
		Short: "show job information",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ViewJob())
		},
	}
	job.InitViewFlags(jobViewCmd)
	jobCmd.AddCommand(jobViewCmd)

	jobSuspendCmd := &cobra.Command{
		Use:   "suspend",
		Short: "abort a job",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.SuspendJob())
		},
	}
	job.InitSuspendFlags(jobSuspendCmd)
	jobCmd.AddCommand(jobSuspendCmd)

	jobResumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "resume a job",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ResumeJob())
		},
	}
	job.InitResumeFlags(jobResumeCmd)
	jobCmd.AddCommand(jobResumeCmd)

	jobDelCmd := &cobra.Command{
		Use:   "delete",
		Short: "delete a job",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.DeleteJob())
		},
	}
	job.InitDeleteFlags(jobDelCmd)
	jobCmd.AddCommand(jobDelCmd)

	return jobCmd
}
