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

package util

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// CheckError prints the error of commands.
func CheckError(cmd *cobra.Command, err error) {
	if err != nil {
		var msg strings.Builder
		msg.WriteString("Failed to")

		// Ignore the root command.
		for cur := cmd; cur.Parent() != nil; cur = cur.Parent() {
			fmt.Fprintf(&msg, " %s", cur.Name())
		}

		fmt.Printf("%s: %v\n", msg.String(), err)
		os.Exit(-1)
	}
}
