/*
Copyright 2019 The Kubernetes Authors.

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

package main

import (
	"github.com/spf13/cobra"

	"github.com/kubernetes-sigs/volcano/pkg/cli/job"
)

func buildJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "Operations related to the volcano job",
	}

	jobRunCmd := &cobra.Command{
		Use:   "run",
		Short: "creates jobs",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.RunJob())
		},
	}
	job.InitRunFlags(jobRunCmd)
	jobCmd.AddCommand(jobRunCmd)

	jobListCmd := &cobra.Command{
		Use:   "list",
		Short: "lists all the jobs",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ListJobs())
		},
	}
	job.InitListFlags(jobListCmd)
	jobCmd.AddCommand(jobListCmd)

	jobSuspendCmd := &cobra.Command{
		Use:   "suspend",
		Short: "creates a job command to abort job",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.SuspendJob())
		},
	}
	job.InitSuspendFlags(jobSuspendCmd)
	jobCmd.AddCommand(jobSuspendCmd)

	jobResumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "creates command to resume job",
		Run: func(cmd *cobra.Command, args []string) {
			checkError(cmd, job.ResumeJob())
		},
	}
	job.InitResumeFlags(jobResumeCmd)
	jobCmd.AddCommand(jobResumeCmd)

	return jobCmd
}
