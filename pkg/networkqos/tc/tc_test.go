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

package tc

import (
	"strings"
	"testing"

	"volcano.sh/volcano/pkg/networkqos/utils"
)

// TestBuildFilterArgs_IfNamePassedVerbatim is the revert-detection guard for
// the command-injection fix at tc_linux.go AddFilter. The CVE sink previously
// concatenated ifName into a "sh -c" string, letting shell metacharacters in an
// attacker-controlled ifName (via the Multus k8s.v1.cni.cncf.io/ifname
// annotation) execute arbitrary commands on the node as root. buildFilterArgs
// must keep ifName as a single literal argv element so no shell ever sees it.
//
// If someone reverts the fix back to fmt.Sprintf + CommandContext (sh -c), this
// test will not by itself fail (it only exercises the pure constructor); but
// any revert that inlines/drops buildFilterArgs will force deleting this test,
// making the regression visible at review time.
func TestBuildFilterArgs_IfNamePassedVerbatim(t *testing.T) {
	cases := []struct {
		name   string
		ifName string
	}{
		{"benign", "eth0"},
		{"semicolon_injection", "eth0;touch /tmp/pwned"},
		{"short_semicolon", "a;id"},
		{"command_substitution", "a$(id)"},
		{"backticks", "a`id`"},
		{"pipe", "a|cat /etc/shadow"},
		{"ampersand", "a&rm -rf /"},
		{"newline", "a\nid"},
		{"redirect", "a>evil"},
	}

	wantPrefix := []string{"filter", "add", "dev"}
	wantSuffix := []string{"egress", "bpf", "direct-action", "obj", utils.TCPROGPath, "sec", "tc"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := buildFilterArgs(tc.ifName)

			// ifName MUST be a single argv element at index 3, byte-identical to input.
			if len(args) < 4 || args[3] != tc.ifName {
				t.Fatalf("ifName must be a single literal argv element at index 3: got args=%v", args)
			}

			// ifName must not be split across multiple argv elements by any shell logic.
			joined := strings.Join(args, "\x00")
			if got := strings.Count(joined, tc.ifName); got != 1 {
				t.Fatalf("ifName should appear exactly once in argv, got %d occurrences in %v", got, args)
			}

			// Structural shape: prefix + [ifName] + suffix.
			if len(args) != len(wantPrefix)+1+len(wantSuffix) {
				t.Fatalf("argv length = %d, want %d: args=%v", len(args), len(wantPrefix)+1+len(wantSuffix), args)
			}
			for i, w := range wantPrefix {
				if args[i] != w {
					t.Fatalf("args[%d] = %q, want %q", i, args[i], w)
				}
			}
			for i, w := range wantSuffix {
				if args[len(wantPrefix)+1+i] != w {
					t.Fatalf("suffix args[%d] = %q, want %q", len(wantPrefix)+1+i, args[len(wantPrefix)+1+i], w)
				}
			}
		})
	}
}

// TestBuildFilterArgs_NoShellMetacharInterpolation is a negative proof: the
// returned argv, when joined with spaces to mimic what a shell would have seen
// under the OLD sink, must still contain the metacharacters as inert data. This
// documents that the safety property is "no shell", not "strip metacharacters".
func TestBuildFilterArgs_NoShellMetacharInterpolation(t *testing.T) {
	malicious := "eth0;touch /tmp/pwned"
	args := buildFilterArgs(malicious)
	// The semicolon must be present as literal data inside args[3], not parsed.
	if !strings.Contains(args[3], ";") {
		t.Fatalf("expected ';' to be preserved as literal data in ifName argv element, got %q", args[3])
	}
}
