/*
Copyright 2017 The Volcano Authors.

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
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/wait"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/version"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	// flag.InitFlags()

	klog.InitFlags(nil)

	// The default klog flush interval is 30 seconds, which is frighteningly long.
	go wait.Until(klog.Flush, *logFlushFreq, wait.NeverStop)
	defer klog.Flush()

	rootCmd := cobra.Command{
		Use: "vcctl",
	}

	// tell Cobra not to provide the default completion command
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(buildJobCmd())
	rootCmd.AddCommand(buildQueueCmd())
	rootCmd.AddCommand(versionCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Failed to execute command: %v\n", err)
		os.Exit(2)
	}
}

func checkError(cmd *cobra.Command, err error) {
	if err != nil {
		msg := "Failed to"

		// Ignore the root command.
		for cur := cmd; cur.Parent() != nil; cur = cur.Parent() {
			msg += fmt.Sprintf(" %s", cur.Name())
		}

		fmt.Printf("%s: %v\n", msg, err)
		os.Exit(2)
	}
}

var versionExample = `vcctl version`

func versionCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:     "version",
		Short:   "Print the version information",
		Long:    "Print the version information",
		Example: versionExample,
		Run: func(cmd *cobra.Command, args []string) {
			version.PrintVersionAndExit()
		},
	}
	return command
}
