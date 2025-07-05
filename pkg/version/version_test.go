package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	// Override the globals so we can predictably assert them
	Version = "vX.Y.Z"
	GitSHA = "deadbeef"
	Built   = "2025-07-05T12:00:00Z"

	got := Info("v1beta1")
	if len(got) != 6 {
		t.Fatalf("Info() returned %d lines; want 6", len(got))
	}

	tests := []struct {
		line, wantPrefix string
	}{
		{got[0], "API Version: v1beta1"},
		{got[1], "Version: vX.Y.Z"},
		{got[2], "Git SHA: deadbeef"},
		{got[3], "Built At: 2025-07-05T12:00:00Z"},
		{got[4], "Go Version: " + runtime.Version()},
		{got[5], "Go OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH},
	}

	for i, tc := range tests {
		if !strings.HasPrefix(tc.line, tc.wantPrefix) {
			t.Errorf("line %d = %q; want prefix %q", i, tc.line, tc.wantPrefix)
		}
	}
}
