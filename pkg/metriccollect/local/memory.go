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

var memoryStatMetrics = map[string]bool{
	"total_cache": true,
	"total_rss":   true,
	"total_swap":  true,
}

const (
	defaultMemInfoPath = "/host/proc/meminfo"
	memInfoPathEnv     = "MEM_INFO_PATH_ENV"
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

func getMemoryUsage(cgroupRoot string, cgroupVersion string) (int64, error) {
	usage := int64(0)

	var memoryUsageFile string
	if cgroupVersion == cgroup.CgroupV2 {
		memoryUsageFile = cgroup.MemoryUsageFileV2
	} else {
		memoryUsageFile = cgroup.MemoryUsageFile
	}
	cgroupMemory := filepath.Join(cgroupRoot, memoryUsageFile)
	date, err := os.ReadFile(cgroupMemory)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(date), "\n")
	for _, line := range lines {
		slices := strings.Split(line, " ")
		if len(slices) != 2 {
			continue
		}

		if cgroupVersion == cgroup.CgroupV2 {
			if isMemoryStatMetricV2(slices[0]) {
				value, err := strconv.Atoi(slices[1])
				if err != nil {
					continue
				}
				usage += int64(value)
			}
		} else {
			if memoryStatMetrics[slices[0]] {
				value, err := strconv.Atoi(slices[1])
				if err != nil {
					continue
				}

				usage = usage + int64(value)
			}
		}
	}
	return usage, nil
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

// isMemoryStatMetricV2 checks if the metric is a valid memory stat metric for cgroup v2
func isMemoryStatMetricV2(metric string) bool {
	// In cgroup v2, memory.stat contains different field names
	// Based on actual cgroup v2 memory.stat file content:
	// For memory usage calculation, we focus on the main memory consumption metrics:
	// - anon: anonymous memory (similar to total_rss in v1)
	// - file: file-backed memory (similar to total_cache in v1)
	// - kernel: kernel memory usage
	// - shmem: shared memory
	// - slab: slab memory usage
	// - pagetables: page table memory
	// - kernel_stack: kernel stack memory
	// - percpu: per-CPU memory
	// - sock: socket memory
	// - vmalloc: vmalloc memory
	v2MemoryStatMetrics := map[string]bool{
		"anon":         true, // Anonymous memory (RSS equivalent)
		"file":         true, // File-backed memory (cache equivalent)
		"kernel":       true, // Kernel memory
		"shmem":        true, // Shared memory
		"slab":         true, // Slab memory
		"pagetables":   true, // Page tables
		"kernel_stack": true, // Kernel stack
		"percpu":       true, // Per-CPU memory
		"sock":         true, // Socket memory
		"vmalloc":      true, // Vmalloc memory
		// Note: We exclude some metrics that are subsets or derivatives:
		// - slab_reclaimable, slab_unreclaimable (covered by slab)
		// - active_anon, inactive_anon (covered by anon)
		// - active_file, inactive_file (covered by file)
		// - file_mapped, file_dirty, file_writeback (covered by file)
		// - anon_thp, file_thp, shmem_thp (covered by anon, file, shmem)
	}
	return v2MemoryStatMetrics[metric]
}
