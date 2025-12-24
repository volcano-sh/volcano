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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/file"
)

const (
	defaultProcPath = "/host/proc/stat"
	procStatPathEnv = "PROC_STAT_PATH"

	// jiffies is the kernel tick unit.
	jiffies = float64(10 * time.Millisecond)
)

type CPUResourceCollector struct {
	cgroupManager cgroup.CgroupManager
}

func NewCPUResourceCollector(cgroupManager cgroup.CgroupManager) (SubCollector, error) {
	return &CPUResourceCollector{
		cgroupManager: cgroupManager,
	}, nil
}

func (c *CPUResourceCollector) Run() {}

func (c *CPUResourceCollector) CollectLocalMetrics(metricInfo *LocalMetricInfo, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error) {
	cgroupPath, err := c.cgroupManager.GetRootCgroupPath(cgroup.CgroupCpuSubsystem)
	if err != nil {
		return nil, err
	}

	cgroupVersion := c.cgroupManager.GetCgroupVersion()
	podAllUsage, err := getMilliCPUUsage(cgroupPath, cgroupVersion)
	if err != nil {
		return nil, err
	}

	finalUsage := int64(0)
	if !metricInfo.IncludeGuaranteedPods {
		for _, qos := range []corev1.PodQOSClass{corev1.PodQOSBurstable, corev1.PodQOSBestEffort} {
			cgroupPath, err = c.cgroupManager.GetQoSCgroupPath(qos, cgroup.CgroupCpuSubsystem)
			if err != nil {
				return nil, err
			}

			count, err := getMilliCPUUsage(cgroupPath, cgroupVersion)
			if err != nil {
				return nil, err
			}
			finalUsage += count
		}
	} else {
		finalUsage = podAllUsage
	}

	if metricInfo.IncludeSystemUsed {
		nodeUsage, err := nodeCPUUsage()
		if err != nil {
			return nil, fmt.Errorf("failed to get node usage, err: %v", err)
		}
		systemUsage := nodeUsage - podAllUsage
		finalUsage += systemUsage
	}
	sample := prompb.TimeSeries{
		Samples: []prompb.Sample{
			{
				Timestamp: timestamp.FromTime(time.Now()),
				Value:     float64(finalUsage),
			},
		},
	}
	return []*prompb.TimeSeries{&sample}, nil
}

func getMilliCPUUsage(cgroupRoot string, cgroupVersion string) (int64, error) {
	startTime := time.Now().UnixNano()

	var cpuUsageFile string
	if cgroupVersion == cgroup.CgroupV2 {
		cpuUsageFile = cgroup.CPUUsageFileV2
	} else {
		cpuUsageFile = cgroup.CPUUsageFile
	}

	cgroupsCPU := filepath.Join(cgroupRoot, cpuUsageFile)

	var startUsage int64
	var err error
	if cgroupVersion == cgroup.CgroupV1 {
		startUsage, err = file.ReadIntFromFile(cgroupsCPU)
	} else {
		startUsage, err = readCPUUsageV2(cgroupsCPU)
	}

	if err != nil {
		return 0, err
	}
	time.Sleep(1 * time.Second)
	endTime := time.Now().UnixNano()

	var endUsage int64
	if cgroupVersion == cgroup.CgroupV1 {
		endUsage, err = file.ReadIntFromFile(cgroupsCPU)
	} else {
		endUsage, err = readCPUUsageV2(cgroupsCPU)
	}
	if err != nil {
		return 0, err
	}
	if endTime-startTime == 0 {
		return 0, fmt.Errorf("statistic time is zero")
	}
	return (endUsage - startUsage) * 1000 / (endTime - startTime), nil
}

func nodeCPUState() (uint64, error) {
	procStatFile := os.Getenv(procStatPathEnv)
	if procStatFile == "" {
		procStatFile = defaultProcPath
	}
	contents, err := os.ReadFile(procStatFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read proc state file: %v", err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		total := uint64(0)
		// sum cpu time = $user+$nice+$system+$idle+$iowait+$irq+$softirq
		for i := 1; i < len(fields); i++ {
			val, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse state filed: %v, err: %v", fields[i], err)
			}
			// exclude $idle $iowait time.
			if i == 4 || i == 5 {
				continue
			}
			total += val
		}
		return total, nil
	}
	return 0, fmt.Errorf("invalid proc stat file: %s", procStatFile)
}

func nodeCPUUsage() (int64, error) {
	startTime := time.Now().UnixNano()
	total0, err := nodeCPUState()
	if err != nil {
		return 0, err
	}
	time.Sleep(time.Second)
	endTime := time.Now().UnixNano()
	total1, err := nodeCPUState()
	if err != nil {
		return 0, err
	}

	totalTicks := float64(total1 - total0)
	if totalTicks <= 0 {
		return 0, fmt.Errorf("negative total tick: %v", totalTicks)
	}
	cpuUsage := int64((totalTicks / (float64(endTime-startTime) / jiffies)) * 1000)
	klog.V(4).InfoS("Node cpu usage", "value", cpuUsage)
	return cpuUsage, nil
}

func readCPUUsageV2(filePath string) (int64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		if fields[0] == "usage_usec" {
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse usage_usec value: %v", err)
			}
			return value * 1000, nil
		}
	}

	return 0, fmt.Errorf("usage_usec field not found in cpu.stat file")
}
