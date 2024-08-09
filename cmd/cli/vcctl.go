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
	"os"

	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/component-base/cli"

	"volcano.sh/volcano/pkg/version"
)

func main() {
	rootCmd := cobra.Command{
		Use: "vcctl",
	}

	// tell Cobra not to provide the default completion command
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(buildJobCmd())
	rootCmd.AddCommand(buildQueueCmd())
	rootCmd.AddCommand(buildJobTemplateCmd())
	rootCmd.AddCommand(buildJobFlowCmd())
	rootCmd.AddCommand(buildPodCmd())
	rootCmd.AddCommand(versionCommand())

	code := cli.Run(&rootCmd)
	os.Exit(code)
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
