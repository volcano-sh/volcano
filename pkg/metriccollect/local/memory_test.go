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

package local

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func TestMemoryWorkingSet(t *testing.T) {
	tests := []struct {
		name     string
		usage    int64
		inactive int64
		want     int64
	}{
		{name: "subtract inactive file", usage: 1_000_000_000, inactive: 600_000_000, want: 400_000_000},
		{name: "inactive exceeds usage", usage: 100, inactive: 200, want: 0},
		{name: "no inactive file", usage: 500, inactive: 0, want: 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := memoryWorkingSet(tt.usage, tt.inactive); got != tt.want {
				t.Fatalf("memoryWorkingSet() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseMemoryStatKey(t *testing.T) {
	content := "total_rss 100\ntotal_inactive_file 60\n"
	got, err := parseMemoryStatKey(content, inactiveFileKeyV1)
	if err != nil || got != 60 {
		t.Fatalf("parseMemoryStatKey() = %d, %v, want 60, nil", got, err)
	}
}

func TestParseMemoryStatKeyNotFound(t *testing.T) {
	_, err := parseMemoryStatKey("total_rss 100\n", inactiveFileKeyV1)
	if !errors.Is(err, errMemoryStatKeyNotFound) {
		t.Fatalf("parseMemoryStatKey() err = %v, want errMemoryStatKeyNotFound", err)
	}
}

func TestGetMemoryUsageMissingInactiveFileV1(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, memoryUsageInBytesV1), []byte("500000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat := "total_cache 100\ntotal_rss 200\n"
	if err := os.WriteFile(filepath.Join(dir, cgroup.MemoryUsageFile), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := getMemoryUsage(dir, cgroup.CgroupV1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500_000_000 {
		t.Fatalf("getMemoryUsage(v1) = %d, want 500000000 when inactive file key is absent", got)
	}
}

func TestGetMemoryUsageMissingInactiveFileV2(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, memoryCurrentV2), []byte("500000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat := "anon 200000000\nfile 100000000\n"
	if err := os.WriteFile(filepath.Join(dir, cgroup.MemoryUsageFileV2), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := getMemoryUsage(dir, cgroup.CgroupV2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500_000_000 {
		t.Fatalf("getMemoryUsage(v2) = %d, want 500000000 when inactive_file is absent", got)
	}
}

func TestGetMemoryUsageCgroupV1(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, memoryUsageInBytesV1), []byte("1000000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat := "total_cache 700000000\ntotal_rss 200000000\ntotal_inactive_file 600000000\n"
	if err := os.WriteFile(filepath.Join(dir, cgroup.MemoryUsageFile), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := getMemoryUsage(dir, cgroup.CgroupV1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 400_000_000 {
		t.Fatalf("getMemoryUsage(v1) = %d, want 400000000 (working set, not cache-inclusive sum)", got)
	}
}

func TestGetMemoryUsageCgroupV2(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, memoryCurrentV2), []byte("900000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat := "anon 200000000\nfile 700000000\ninactive_file 550000000\n"
	if err := os.WriteFile(filepath.Join(dir, cgroup.MemoryUsageFileV2), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := getMemoryUsage(dir, cgroup.CgroupV2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 350_000_000 {
		t.Fatalf("getMemoryUsage(v2) = %d, want 350000000", got)
	}
}
