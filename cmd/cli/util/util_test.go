/*
Copyright 2026 The Volcano Authors.

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
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckError(t *testing.T) {
	const childModeEnv = "VOLCANO_TEST_CHECK_ERROR_CHILD"
	// CheckError exits the process, so exercise its failure path in this test binary's child.
	if mode := os.Getenv(childModeEnv); mode != "" {
		root := &cobra.Command{Use: "vcctl"}
		job := &cobra.Command{Use: "job"}
		list := &cobra.Command{Use: "list"}
		root.AddCommand(job)
		job.AddCommand(list)
		if mode == "custom" {
			root.SetErr(os.Stdout)
		}
		CheckError(list, errors.New("request denied"))
		return
	}

	const diagnostic = "Failed to list job: request denied\n"
	tests := []struct {
		name       string
		mode       string
		wantStdout string
		wantStderr string
	}{
		{name: "default error stream", mode: "default", wantStderr: diagnostic},
		{name: "inherited custom error stream", mode: "custom", wantStdout: diagnostic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCheckError$")
			cmd.Env = append(os.Environ(), childModeEnv+"="+tt.mode)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			var exitErr *exec.ExitError
			require.ErrorAs(t, cmd.Run(), &exitErr)
			assert.Equal(t, tt.wantStdout, stdout.String())
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}

func TestCheckErrorNoError(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{Use: "list"}
	cmd.SetErr(&output)
	CheckError(cmd, nil)
	assert.Empty(t, output.String())
}
