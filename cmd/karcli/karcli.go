/*
Copyright 2017 The Kubernetes Authors.

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
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/karcli/job"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/util/wait"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	// flag.InitFlags()

	// The default glog flush interval is 30 seconds, which is frighteningly long.
	go wait.Until(glog.Flush, *logFlushFreq, wait.NeverStop)
	defer glog.Flush()

	rootCmd := cobra.Command{
		Use: "karcli",
	}

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

	rootCmd.AddCommand(jobCmd)

	rootCmd.Execute()
}

func checkError(cmd *cobra.Command, err error) {
	if err != nil {
		msg := "Failed to"

		// Ignore the root command.
		for cur := cmd; cur.Parent() != nil; cur = cur.Parent() {
			msg = msg + fmt.Sprintf(" %s", cur.Name())
		}

		fmt.Printf("%s: %v\n", msg, err)
	}
}
