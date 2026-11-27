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

package tc

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"

	"volcano.sh/volcano/pkg/agent/utils/exec"
)

// envInNetNS is set when the test binary has been re-executed inside a new
// user and network namespace, where it may create links and qdiscs.
const envInNetNS = "VOLCANO_TC_TEST_IN_NETNS"

const testLink = "vcqos0"

// runInNetNS re-executes the calling test inside "unshare -rn" so it runs as
// root of a private network namespace without needing privileges on the host.
// It returns true when the caller is already running inside that namespace and
// should perform the test body; otherwise it reports the child's result and
// returns false. The test is skipped where unprivileged user namespaces are
// not available.
func runInNetNS(t *testing.T) bool {
	t.Helper()
	if os.Getenv(envInNetNS) == "1" {
		return true
	}
	unshare, err := osexec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare not found")
	}
	if out, err := osexec.Command(unshare, "-rn", "true").CombinedOutput(); err != nil {
		t.Skipf("unprivileged user and network namespaces are not available: %v, %s", err, out)
	}
	cmd := osexec.Command(unshare, "-rn", os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), envInNetNS+"=1")
	out, err := cmd.CombinedOutput()
	t.Logf("test output inside network namespace:\n%s", out)
	if err != nil {
		t.Fatalf("test failed inside network namespace: %v", err)
	}
	return false
}

type fakeExecutor struct {
	cmds []string
	err  error
}

func (f *fakeExecutor) CommandContext(_ context.Context, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	if f.err != nil {
		return "simulated tc failure", f.err
	}
	return "", nil
}

// qdiscTypes returns "type@parent" for every qdisc on the test link, ignoring
// the kernel default noqueue qdisc.
func qdiscTypes(t *testing.T) []string {
	t.Helper()
	link, err := netlink.LinkByName(testLink)
	require.NoError(t, err)
	qdiscs, err := netlink.QdiscList(link)
	require.NoError(t, err)
	var result []string
	for _, q := range qdiscs {
		switch {
		case q.Type() == QdiscTypeNoQueue:
		case q.Attrs().Parent == netlink.HANDLE_ROOT:
			result = append(result, q.Type()+"@root")
		case q.Attrs().Parent == netlink.HANDLE_CLSACT:
			result = append(result, q.Type()+"@clsact")
		default:
			result = append(result, q.Type()+"@other")
		}
	}
	sort.Strings(result)
	return result
}

func setupLink(t *testing.T, preRootFq, preClsact bool) {
	t.Helper()
	require.NoError(t, netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: testLink}}))
	t.Cleanup(func() {
		if link, err := netlink.LinkByName(testLink); err == nil {
			_ = netlink.LinkDel(link)
		}
	})
	link, err := netlink.LinkByName(testLink)
	require.NoError(t, err)
	require.NoError(t, netlink.LinkSetUp(link))
	if preRootFq {
		require.NoError(t, netlink.QdiscAdd(&netlink.Fq{QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index, Parent: netlink.HANDLE_ROOT}}))
	}
	if preClsact {
		require.NoError(t, netlink.QdiscAdd(&netlink.GenericQdisc{QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index, Parent: netlink.HANDLE_CLSACT}, QdiscType: QdiscTypeClsact}))
	}
}

func useFakeExecutor(t *testing.T, err error) *fakeExecutor {
	t.Helper()
	original := exec.GetExecutor()
	fake := &fakeExecutor{err: err}
	exec.SetExecutor(fake)
	t.Cleanup(func() { exec.SetExecutor(original) })
	return fake
}

func TestAddFilter(t *testing.T) {
	if !runInNetNS(t) {
		return
	}
	nsPath := "/proc/self/ns/net"
	errTC := errors.New("tc failed")

	tests := []struct {
		name        string
		preRootFq   bool
		preClsact   bool
		filterErr   error
		wantErr     bool
		wantQdiscs  []string
		wantFilters int
	}{
		{
			name:        "clean interface",
			wantQdiscs:  []string{"clsact@clsact", "fq@root"},
			wantFilters: 1,
		},
		{
			name:        "reuses a clsact qdisc installed by another plugin",
			preClsact:   true,
			wantQdiscs:  []string{"clsact@clsact", "fq@root"},
			wantFilters: 1,
		},
		{
			name:        "reuses an existing root fq qdisc",
			preRootFq:   true,
			wantQdiscs:  []string{"clsact@clsact", "fq@root"},
			wantFilters: 1,
		},
		{
			name:        "reuses both existing qdiscs",
			preRootFq:   true,
			preClsact:   true,
			wantQdiscs:  []string{"clsact@clsact", "fq@root"},
			wantFilters: 1,
		},
		{
			name:        "filter failure rolls back the qdiscs it created",
			filterErr:   errTC,
			wantErr:     true,
			wantQdiscs:  nil,
			wantFilters: 1,
		},
		{
			name:        "filter failure keeps qdiscs that existed before",
			preClsact:   true,
			filterErr:   errTC,
			wantErr:     true,
			wantQdiscs:  []string{"clsact@clsact"},
			wantFilters: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupLink(t, tt.preRootFq, tt.preClsact)
			fake := useFakeExecutor(t, tt.filterErr)
			tc := &TCCmd{}

			supported, err := tc.PreAddFilter(nsPath, testLink)
			require.NoError(t, err)
			require.True(t, supported, "PreAddFilter must accept this interface")

			err = tc.AddFilter(nsPath, testLink)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantQdiscs, qdiscTypes(t))
			assert.Len(t, fake.cmds, tt.wantFilters)
		})
	}
}

func TestAddFilterRetryAfterFailure(t *testing.T) {
	if !runInNetNS(t) {
		return
	}
	nsPath := "/proc/self/ns/net"
	setupLink(t, false, false)
	tc := &TCCmd{}

	useFakeExecutor(t, errors.New("tc failed"))
	require.Error(t, tc.AddFilter(nsPath, testLink))

	fake := useFakeExecutor(t, nil)
	require.NoError(t, tc.AddFilter(nsPath, testLink), "a retried ADD must not fail with EEXIST")
	assert.Equal(t, []string{"clsact@clsact", "fq@root"}, qdiscTypes(t))
	assert.Len(t, fake.cmds, 1)
}

func TestAddFilterThenRemoveFilter(t *testing.T) {
	if !runInNetNS(t) {
		return
	}
	nsPath := "/proc/self/ns/net"
	setupLink(t, false, false)
	useFakeExecutor(t, nil)
	tc := &TCCmd{}

	require.NoError(t, tc.AddFilter(nsPath, testLink))
	require.NoError(t, tc.RemoveFilter(nsPath, testLink))
	assert.Empty(t, qdiscTypes(t))
}

func TestPreAddFilterRejectsForeignQdiscs(t *testing.T) {
	if !runInNetNS(t) {
		return
	}
	nsPath := "/proc/self/ns/net"
	setupLink(t, false, false)
	link, err := netlink.LinkByName(testLink)
	require.NoError(t, err)
	require.NoError(t, netlink.QdiscAdd(&netlink.Tbf{
		QdiscAttrs: netlink.QdiscAttrs{LinkIndex: link.Attrs().Index, Parent: netlink.HANDLE_ROOT},
		Rate:       1 << 20, Limit: 1 << 16, Buffer: 1 << 14,
	}))

	supported, err := (&TCCmd{}).PreAddFilter(nsPath, testLink)
	require.NoError(t, err)
	assert.False(t, supported, "an interface with a foreign root qdisc must be skipped")
}
