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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/prompb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const (
	defaultMemInfoPath = "/host/proc/meminfo"
	memInfoPathEnv     = "MEM_INFO_PATH_ENV"

	memoryUsageInBytesV1 = "memory.usage_in_bytes"
	memoryCurrentV2      = "memory.current"

	inactiveFileKeyV1 = "total_inactive_file"
	inactiveFileKeyV2 = "inactive_file"
)

type MemoryResourceCollector struct {
	cgroupManager cgroup.CgroupManager
}

func NewMemoryResourceCollector(cgroupManager cgroup.CgroupManager) (SubCollector, error) {
	return &MemoryResourceCollector{
		cgroupManager: cgroupManager,
	}, nil
}

func (c *MemoryResourceCollector) Run() {}

func (c *MemoryResourceCollector) CollectLocalMetrics(metricInfo *LocalMetricInfo, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error) {
	var (
		count int64
		err   error
	)
	cgroupPath, err := c.cgroupManager.GetRootCgroupPath(cgroup.CgroupMemorySubsystem)
	if err != nil {
		return nil, err
	}

	count, err = getMemoryUsage(cgroupPath, c.cgroupManager.GetCgroupVersion())
	if err != nil {
		return nil, err
	}

	if metricInfo.IncludeSystemUsed {
		count, err = nodeMemoryUsage()
		if err != nil {
			return nil, err
		}
	}
	sample := prompb.TimeSeries{
		Samples: []prompb.Sample{
			{
				Timestamp: timestamp.FromTime(time.Now()),
				Value:     float64(count),
			},
		},
	}
	return []*prompb.TimeSeries{&sample}, nil
}

// getMemoryUsage reports cgroup memory working set, aligned with kubelet eviction:
// total usage minus inactive file cache (reclaimable under pressure).
func getMemoryUsage(cgroupRoot string, cgroupVersion string) (int64, error) {
	var usageFile, inactiveKey string
	if cgroupVersion == cgroup.CgroupV2 {
		usageFile = memoryCurrentV2
		inactiveKey = inactiveFileKeyV2
	} else {
		usageFile = memoryUsageInBytesV1
		inactiveKey = inactiveFileKeyV1
	}

	usage, err := readCgroupCounter(filepath.Join(cgroupRoot, usageFile))
	if err != nil {
		return 0, err
	}

	statFile := cgroup.MemoryUsageFile
	if cgroupVersion == cgroup.CgroupV2 {
		statFile = cgroup.MemoryUsageFileV2
	}
	inactive, err := readMemoryStatValue(filepath.Join(cgroupRoot, statFile), inactiveKey)
	if err != nil {
		return 0, err
	}

	return memoryWorkingSet(usage, inactive), nil
}

func memoryWorkingSet(usage, inactiveFile int64) int64 {
	if inactiveFile >= usage {
		return 0
	}
	return usage - inactiveFile
}

func readCgroupCounter(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cgroup counter %s: %w", path, err)
	}
	return value, nil
}

func readMemoryStatValue(statPath, key string) (int64, error) {
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}
	value, ok := parseMemoryStatKey(string(data), key)
	if !ok {
		return 0, fmt.Errorf("memory stat key %q not found in %s", key, statPath)
	}
	return value, nil
}

func parseMemoryStatKey(content, key string) (int64, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func nodeMemoryUsage() (int64, error) {
	memInfoFile := os.Getenv(memInfoPathEnv)
	if memInfoFile == "" {
		memInfoFile = defaultMemInfoPath
	}
	content, err := os.ReadFile(memInfoFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read mem info, err: %v", err)
	}

	total, available := int64(0), int64(0)
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.SplitN(line, ":", 2)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "MemTotal" {
			total, err = strconv.ParseInt(strings.Fields(fields[1])[0], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to get mem total, err: %v", err)
			}
		}
		if fields[0] == "MemAvailable" {
			available, err = strconv.ParseInt(strings.Fields(fields[1])[0], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to get mem available, err: %v", err)
			}
			break
		}
	}

	// mem info unit KB.
	systemUsed := (total - available) * 1024
	klog.V(4).InfoS("System used memory", "value", systemUsed)
	return systemUsed, nil
}
