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
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/moby/sys/userns"
	cgroupsystemd "github.com/opencontainers/runc/libcontainer/cgroups/systemd"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// Global variables for cgroup v2 detection
var (
	isUnified     bool
	isUnifiedOnce sync.Once
	CgroupVersion string
)

type CgroupSubsystem string

const (
	CgroupMemorySubsystem CgroupSubsystem = "memory"
	CgroupCpuSubsystem    CgroupSubsystem = "cpu"
	CgroupNetCLSSubsystem CgroupSubsystem = "net_cls"

	CgroupKubeRoot string = "kubepods"

	SystemdSuffix       string = ".slice"
	PodCgroupNamePrefix string = "pod"

	// Cgroupv1 specific files
	CPUQoSLevelFile string = "cpu.qos_level"
	CPUUsageFile    string = "cpuacct.usage"

	CPUQuotaBurstFile string = "cpu.cfs_burst_us"
	CPUQuotaTotalFile string = "cpu.cfs_quota_us"

	MemoryUsageFile    string = "memory.stat"
	MemoryQoSLevelFile string = "memory.qos_level"
	MemoryLimitFile    string = "memory.limit_in_bytes"

	NetCLSFileName string = "net_cls.classid"

	CPUShareFileName string = "cpu.shares"

	// Cgroupv2 specific files
	CPUWeightFileV2 string = "cpu.weight"
	CPUUsageFileV2  string = "cpu.stat"

	CPUQuotaBurstFileV2 string = "cpu.max.burst"
	CPUQuotaTotalFileV2 string = "cpu.max"
	CPUIdleFileV2       string = "cpu.idle"

	MemoryUsageFileV2 string = "memory.stat"
	MemoryHighFileV2  string = "memory.high"
	MemoryLowFileV2   string = "memory.low"
	MemoryMinFileV2   string = "memory.min"
	MemoryMaxFileV2   string = "memory.max"

	// Cgroup version constants
	CgroupV1 string = "v1"
	CgroupV2 string = "v2"

	// Default cgroup mount points
	DefaultCgroupV1MountPoint string = "/sys/fs/cgroup"
	DefaultCgroupV2MountPoint string = "/sys/fs/cgroup"

	// Cgroup driver types
	CgroupDriverSystemd  string = "systemd"
	CgroupDriverCgroupfs string = "cgroupfs"
)

type CgroupManager interface {
	GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error)
	GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error)
	GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error)
	GetCgroupVersion() string
}

type CgroupManagerImpl struct {
	// cgroupDriver is the driver that the kubelet uses to manipulate cgroups on the host (cgroupfs or systemd)
	cgroupDriver string

	// cgroupRoot is the root cgroup to use for pods.
	cgroupRoot string

	// kubeCgroupRoot sames with kubelet configuration "cgroup-root"
	kubeCgroupRoot string

	// cgroupVersion indicates the cgroup version (v1 or v2)
	cgroupVersion string
}

// GetCgroupDriver gets the cgroup driver from multiple sources in order of priority
func GetCgroupDriver() string {
	// 1. Try to get from environment variable
	if driver := os.Getenv("CGROUP_DRIVER"); driver != "" {
		if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
			return driver
		}
	}

	// 2. Try to read from kubelet config file
	if driver := readKubeletCgroupDriver(); driver != "" {
		return driver
	}

	// 3. Try to detect from system
	if driver, err := DetectCgroupDriver(); err == nil {
		return driver
	}

	// 4. Default fallback
	return CgroupDriverCgroupfs
}

// readKubeletCgroupDriver reads cgroup driver from kubelet config file or process command line arguments
func readKubeletCgroupDriver() string {
	// First, try to read from kubelet config files (original method)
	driver := getCgroupDriverFromKubeletConfig()
	if driver != "" {
		return driver
	}

	// If config files don't exist or don't contain cgroup driver info, try process-based method
	klog.V(4).Infof("Config files not found or don't contain cgroup driver, trying process-based method")

	driver = getCgroupDriverFromKubeletProcess()
	if driver != "" {
		klog.V(4).Infof("Found cgroup driver from kubelet process: %s", driver)
		return driver
	}

	return ""
}

// getCgroupDriverFromKubeletConfig gets cgroup driver by reading kubelet config files
// This function is separated for easier unit testing
func getCgroupDriverFromKubeletConfig() string {
	// Common kubelet config file paths
	configPaths := []string{
		"/var/lib/kubelet/config.yaml",
		"/etc/kubernetes/kubelet.conf",
		"/var/lib/kubelet/kubeadm-flags.env",
	}

	for _, configPath := range configPaths {
		if driver := parseKubeletConfig(configPath); driver != "" {
			klog.V(4).Infof("Found cgroup driver from config file %s: %s", configPath, driver)
			return driver
		}
	}

	return ""
}

// getCgroupDriverFromKubeletProcess gets cgroup driver by finding kubelet process and parsing its command line arguments
// This function is separated for easier unit testing
func getCgroupDriverFromKubeletProcess() string {
	// Find kubelet process
	kubeletPID, err := findKubeletProcess()
	if err != nil {
		klog.V(4).Infof("Failed to find kubelet process: %v", err)
		return ""
	}

	// Read command line arguments from /proc/<pid>/cmdline
	cmdline, err := readProcessCmdline(kubeletPID)
	if err != nil {
		klog.V(4).Infof("Failed to read kubelet cmdline: %v", err)
		return ""
	}

	// Parse cgroup driver from command line arguments
	return parseCgroupDriverFromCmdline(cmdline)
}

// parseKubeletConfig parses kubelet config file to extract cgroup driver
func parseKubeletConfig(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	content := string(data)

	// Look for cgroupDriver in YAML format
	if strings.Contains(content, "cgroupDriver:") {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "cgroupDriver:") {
				driver := strings.TrimSpace(strings.TrimPrefix(line, "cgroupDriver:"))
				if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
					return driver
				}
			}
		}
	}

	// Look for --cgroup-driver in command line arguments
	if strings.Contains(content, "--cgroup-driver") {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "--cgroup-driver") {
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "--cgroup-driver" && i+1 < len(parts) {
						driver := parts[i+1]
						if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
							return driver
						}
					}
				}
			}
		}
	}

	return ""
}

// findKubeletProcess finds the kubelet process PID by reading /proc filesystem
func findKubeletProcess() (int, error) {
	procDir, err := os.Open("/proc")
	if err != nil {
		return 0, fmt.Errorf("failed to open /proc: %v", err)
	}
	defer procDir.Close()

	entries, err := procDir.Readdirnames(0)
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc directory: %v", err)
	}

	for _, entry := range entries {
		// Check if entry is a numeric PID
		pid, err := strconv.Atoi(entry)
		if err != nil {
			continue // Skip non-numeric entries
		}

		// Read process comm file to get process name
		commPath := filepath.Join("/proc", entry, "comm")
		commData, err := os.ReadFile(commPath)
		if err != nil {
			continue // Skip if we can't read comm file
		}

		// Check if this is kubelet process
		comm := strings.TrimSpace(string(commData))
		if comm == "kubelet" {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("kubelet process not found")
}

// readProcessCmdline reads the command line arguments from /proc/<pid>/cmdline
func readProcessCmdline(pid int) ([]string, error) {
	cmdlinePath := filepath.Join("/proc", strconv.Itoa(pid), "cmdline")
	cmdlineData, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cmdline file: %v", err)
	}

	// Split by null bytes and filter out empty strings
	var args []string
	for _, arg := range strings.Split(string(cmdlineData), "\x00") {
		if arg != "" {
			args = append(args, arg)
		}
	}

	return args, nil
}

// parseCgroupDriverFromCmdline parses cgroup driver from command line arguments
func parseCgroupDriverFromCmdline(args []string) string {
	// First, try to find --cgroup-driver parameter
	for i, arg := range args {
		// Handle both formats: "--cgroup-driver=value" and "--cgroup-driver value"
		if strings.HasPrefix(arg, "--cgroup-driver=") {
			driver := strings.TrimPrefix(arg, "--cgroup-driver=")
			if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
				return driver
			}
		} else if arg == "--cgroup-driver" && i+1 < len(args) {
			driver := args[i+1]
			if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
				return driver
			}
		}
	}

	// If --cgroup-driver not found, try to find --config parameter
	for i, arg := range args {
		// Handle both formats: "--config=value" and "--config value"
		if strings.HasPrefix(arg, "--config=") {
			configPath := strings.TrimPrefix(arg, "--config=")
			// Read and parse the config file
			if driver := parseKubeletConfig(configPath); driver != "" {
				return driver
			}
		} else if arg == "--config" && i+1 < len(args) {
			configPath := args[i+1]
			// Read and parse the config file
			if driver := parseKubeletConfig(configPath); driver != "" {
				return driver
			}
		}
	}

	return ""
}

// DetectCgroupDriver detects the cgroup driver (cgroupfs or systemd) on the system
func DetectCgroupDriver() (string, error) {
	// Check if systemd is managing cgroups by looking for systemd cgroup hierarchy
	// In systemd-managed systems, there's typically a systemd slice at the root
	if _, err := os.Stat("/sys/fs/cgroup/system.slice"); err == nil {
		return CgroupDriverSystemd, nil
	}

	// Check if we can find systemd cgroup paths
	if _, err := os.Stat("/sys/fs/cgroup/systemd"); err == nil {
		return CgroupDriverSystemd, nil
	}

	// Check for cgroupfs by looking for traditional cgroup hierarchy
	// In cgroupfs systems, we typically see individual controller directories
	if _, err := os.Stat("/sys/fs/cgroup/cpu"); err == nil {
		return CgroupDriverCgroupfs, nil
	}

	// Additional check for cgroup v2 with cgroupfs driver
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		// Check if systemd is not managing this hierarchy
		if _, err := os.Stat("/sys/fs/cgroup/system.slice"); err != nil {
			return CgroupDriverCgroupfs, nil
		}
	}

	// Check for hybrid mode where systemd might be managing some controllers
	if _, err := os.Stat("/sys/fs/cgroup/unified"); err == nil {
		// In hybrid mode, check if systemd is managing the unified hierarchy
		if _, err := os.Stat("/sys/fs/cgroup/unified/system.slice"); err == nil {
			return CgroupDriverSystemd, nil
		}
		return CgroupDriverCgroupfs, nil
	}

	return "", fmt.Errorf("unable to detect cgroup driver")
}

// IsCgroupsV2 checks once if the CGroup V2 is in use.
// This is a more robust implementation based on mature projects like runc.
func IsCgroupsV2(unifiedMountpoint string) bool {
	isUnifiedOnce.Do(func() {
		var st unix.Statfs_t
		err := unix.Statfs(unifiedMountpoint, &st)
		if err != nil {
			if os.IsNotExist(err) && userns.RunningInUserNS() {
				// ignore the "not found" error if running in userns
				klog.ErrorS(err, "cgroup mount missing, assuming cgroup v1", "path", unifiedMountpoint)
				isUnified = false
				return
			}
			klog.ErrorS(err, "Failed to statfs cgroup root, assuming cgroup v1", "mountpoint", unifiedMountpoint)
			isUnified = false
			return
		}
		isUnified = st.Type == unix.CGROUP2_SUPER_MAGIC
	})
	return isUnified
}

// DetectCgroupVersion detects the cgroup version on the system
// For unit testing, it also supports forcing the version via environment variable
func DetectCgroupVersion(cgroupRoot string) string {
	// Check for test environment variable first (for unit testing)
	if testVersion := os.Getenv("VOLCANO_TEST_CGROUP_VERSION"); testVersion != "" {
		switch testVersion {
		case CgroupV1, "1":
			CgroupVersion = CgroupV1
			return CgroupV1
		case CgroupV2, "2":
			CgroupVersion = CgroupV2
			return CgroupV2
		default:
			klog.Warningf("Invalid VOLCANO_TEST_CGROUP_VERSION value: %s, falling back to system detection", testVersion)
		}
	}

	// Production logic: detect from filesystem
	if IsCgroupsV2(cgroupRoot) {
		CgroupVersion = CgroupV2
		return CgroupV2
	}
	CgroupVersion = CgroupV1
	return CgroupV1
}

// NewCgroupManager creates a new cgroup manager based on the detected cgroup version
func NewCgroupManager(cgroupDriver, cgroupRoot, kubeCgroupRoot string) CgroupManager {
	cgroupVersion := DetectCgroupVersion(cgroupRoot)

	// Auto-detect cgroupDriver if not provided
	if cgroupDriver == "" {
		cgroupDriver = GetCgroupDriver()
	}

	return &CgroupManagerImpl{
		cgroupDriver:   cgroupDriver,
		cgroupRoot:     cgroupRoot,
		kubeCgroupRoot: kubeCgroupRoot,
		cgroupVersion:  cgroupVersion,
	}
}

// GetCgroupVersion returns the cgroup version
func (c *CgroupManagerImpl) GetCgroupVersion() string {
	return c.cgroupVersion
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
	return c.buildCgroupPath(cgroupSubsystem, cgroupPath), nil
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
	return c.buildCgroupPath(cgroupSubsystem, cgroupPath), nil
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

	return c.buildCgroupPath(cgroupSubsystem, cgroupPath), nil
}

// buildCgroupPath constructs the final cgroup path based on cgroup version
func (c *CgroupManagerImpl) buildCgroupPath(cgroupSubsystem CgroupSubsystem, cgroupPath string) string {
	switch c.cgroupVersion {
	case CgroupV1:
		// v1: each controller has its own hierarchy
		return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath)
	case CgroupV2:
		// v2: unified hierarchy, no subsystem in path
		return filepath.Join(c.cgroupRoot, cgroupPath)
	default:
		// Fallback to v1 behavior
		return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath)
	}
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
