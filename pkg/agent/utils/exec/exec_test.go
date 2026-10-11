/*
Copyright 2024 The Volcano Authors.

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

package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCommandContextWithArgs_NoShellInjection ensures CommandContextWithArgs runs
// the program directly (no "sh -c"), so any shell metacharacters in arguments are
// treated as literal data and never executed. This is the regression guard for the
// command-injection sink that previously existed in pkg/networkqos/tc/tc_linux.go
// where ifName was concatenated into a "sh -c" command string.
func TestCommandContextWithArgs_NoShellInjection(t *testing.T) {
	proofDir := t.TempDir()
	proofFile := filepath.Join(proofDir, "pwned")

	malicious := "eth0;printf PWNED > " + proofFile

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	e := &Executor{}
	// printf with %s prints the next arg verbatim. Under the old "sh -c" sink the
	// ";" would terminate the first command and create proofFile. With argv-based
	// execution the whole malicious string is a single literal argument.
	out, err := e.CommandContextWithArgs(ctx, "printf", "%s", malicious)
	if err != nil {
		t.Fatalf("CommandContextWithArgs failed: %v, output: %s", err, out)
	}
	if out != malicious {
		t.Fatalf("expected literal arg %q in output, got %q", malicious, out)
	}

	if _, statErr := os.Stat(proofFile); statErr == nil {
		t.Fatalf("injection regression: proof file %s was created, argv execution was bypassed", proofFile)
	}
}
