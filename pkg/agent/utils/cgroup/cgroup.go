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

package cgroup

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	cgroupsystemd "github.com/opencontainers/runc/libcontainer/cgroups/systemd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type CgroupSubsystem string

const (
	CgroupMemorySubsystem CgroupSubsystem = "memory"
	CgroupCpuSubsystem    CgroupSubsystem = "cpu"
	CgroupNetCLSSubsystem CgroupSubsystem = "net_cls"

	CgroupKubeRoot string = "kubepods"

	SystemdSuffix       string = ".slice"
	PodCgroupNamePrefix string = "pod"

	CPUQoSLevelFile string = "cpu.qos_level"
	CPUUsageFile    string = "cpuacct.usage"

	CPUQuotaBurstFile string = "cpu.cfs_burst_us"
	CPUQuotaTotalFile string = "cpu.cfs_quota_us"

	MemoryUsageFile    string = "memory.stat"
	MemoryQoSLevelFile string = "memory.qos_level"
	MemoryLimitFile    string = "memory.limit_in_bytes"

	NetCLSFileName string = "net_cls.classid"

	CPUShareFileName string = "cpu.shares"
)

type CgroupManager interface {
	GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error)
	GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error)
	GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error)
}

type CgroupManagerImpl struct {
	// cgroupDriver is the driver that the kubelet uses to manipulate cgroups on the host (cgroupfs or systemd)
	cgroupDriver string

	// cgroupRoot is the root cgroup to use for pods.
	cgroupRoot string

	// kubeCgroupRoot sames with kubelet configuration "cgroup-root"
	kubeCgroupRoot string
}

func NewCgroupManager(cgroupDriver, cgroupRoot, kubeCgroupRoot string) CgroupManager {
	return &CgroupManagerImpl{
		cgroupDriver:   cgroupDriver,
		cgroupRoot:     cgroupRoot,
		kubeCgroupRoot: kubeCgroupRoot,
	}
}

func (c *CgroupManagerImpl) GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupManagerImpl) GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupManagerImpl) GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}
	cgroupName = append(cgroupName, getPodCgroupNameSuffix(podUID))

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupManagerImpl) CgroupNameToCgroupPath(cgroupName []string) (string, error) {
	switch c.cgroupDriver {
	case "cgroupfs":
		return CgroupName(cgroupName).ToCgroupfs()
	case "systemd":
		return CgroupName(cgroupName).ToSystemd()
	default:
		return "", fmt.Errorf("unsupported cgroup driver: %s", c.cgroupDriver)
	}
}

type CgroupName []string

func (cgroupName CgroupName) ToSystemd() (string, error) {
	if len(cgroupName) == 0 || (len(cgroupName) == 1 && cgroupName[0] == "") {
		return "/", nil
	}

	newparts := []string{}
	for _, part := range cgroupName {
		part = escapeSystemdCgroupName(part)
		newparts = append(newparts, part)
	}

	result, err := cgroupsystemd.ExpandSlice(strings.Join(newparts, "-") + SystemdSuffix)
	if err != nil {
		return "", fmt.Errorf("error converting cgroup name [%v] to systemd format: %v", cgroupName, err)
	}
	return result, nil
}

func (cgroupName CgroupName) ToCgroupfs() (string, error) {
	return "/" + path.Join(cgroupName...), nil
}

func escapeSystemdCgroupName(part string) string {
	return strings.Replace(part, "-", "_", -1)
}

// getPodCgroupNameSuffix returns the last element of the pod CgroupName identifier
func getPodCgroupNameSuffix(podUID types.UID) string {
	return PodCgroupNamePrefix + string(podUID)
}
