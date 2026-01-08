# Agent Cgroup V2 Adaptation

## Table of Contents

- [Summary](#summary)
- [Motivation](#motivation)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Design Details](#design-details)
  - [Architecture Overview](#architecture-overview)
  - [Cgroup Version Detection](#cgroup-version-detection)
  - [Cgroup Driver Detection](#cgroup-driver-detection)
  - [Unified Manager Interface](#unified-manager-interface)
  - [File Structure Mapping](#file-structure-mapping)
  - [Event Handler Adaptation](#event-handler-adaptation)
- [Implementation Details](#implementation-details)
  - [Core Components](#core-components)
  - [Resource Management](#resource-management)
  - [QoS Management](#qos-management)
  - [Testing Strategy](#testing-strategy)
- [Backward Compatibility](#backward-compatibility)
- [Future Work](#future-work)

## Summary

This document describes the design and implementation of cgroup v2 adaptation for the Volcano Agent. The work enables Volcano Agent to seamlessly work with both cgroup v1 and v2 systems while maintaining backward compatibility. The adaptation includes automatic version detection, unified management interfaces, and event handler updates to support cgroup v2-specific file formats and hierarchies.

## Motivation

Container runtime environments are evolving towards cgroup v2, which provides a unified hierarchy and improved resource management capabilities compared to cgroup v1's separated subsystem approach. Major Kubernetes distributions and container runtimes are transitioning to cgroup v2 as the default, making it essential for Volcano Agent to support both versions.

### Key Challenges:

1. **Different Hierarchies**: Cgroup v1 uses separate subsystems (`/sys/fs/cgroup/cpu`, `/sys/fs/cgroup/memory`) while cgroup v2 uses a unified hierarchy (`/sys/fs/cgroup`)
2. **File Format Changes**: Control files have different names and formats between versions (e.g., `cpu.shares` vs `cpu.weight`, `memory.limit_in_bytes` vs `memory.max`)
3. **Resource Representation**: CPU and memory limits are expressed differently in v2
4. **Driver Compatibility**: Both systemd and cgroupfs drivers need to work with both cgroup versions

## Goals

- **Automatic Detection**: Seamlessly detect cgroup version and driver configuration
- **Unified Interface**: Provide a consistent API for both cgroup v1 and v2
- **Backward Compatibility**: Maintain full compatibility with existing cgroup v1 deployments
- **Event Handler Support**: Update all relevant event handlers for cgroup v2 file formats
- **Robust Testing**: Comprehensive unit test coverage for both versions

## Non-Goals

- **Migration Tools**: This design does not include tools for migrating from v1 to v2
- **Hybrid Support**: Mixed v1/v2 environments are not supported in a single deployment
- **Performance Optimization**: This work focuses on compatibility, not performance improvements

## Design Details

### Architecture Overview

The cgroup v2 adaptation introduces a layered architecture that abstracts the differences between cgroup versions:

```
┌─────────────────────────────────────────────────────────┐
│               Event Handlers Layer                      │
│    (Resources, CPUQoS, MemoryQoS, CPUBurst)            │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│           CgroupManager Interface                       │
│        (Version-agnostic operations)                    │
│  - GetRootCgroupPath()                                  │
│  - GetQoSCgroupPath()                                   │
│  - GetPodCgroupPath()                                   │
│  - GetCgroupVersion()                                   │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│           CgroupManagerImpl                             │
│  Unified implementation supporting both v1 & v2         │
│  ┌───────────────────────────────────────────────────┐  │
│  │  • cgroupDriver: systemd/cgroupfs                │  │
│  │  • cgroupVersion: v1/v2 (auto-detected)          │  │
│  │  • buildCgroupPath(): version-aware path builder │  │
│  └───────────────────────────────────────────────────┘  │
│           ┌──────────────┴──────────────┐               │
│           │                             │               │
│  ┌─────────────────┐         ┌─────────────────┐       │
│  │  v1 Behavior    │         │  v2 Behavior    │       │
│  │  Separate paths │         │  Unified path   │       │
│  │  per subsystem  │         │  hierarchy      │       │
│  └─────────────────┘         └─────────────────┘       │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│          Detection & Driver Layer                       │
│  • IsCgroupsV2(): filesystem magic number check         │
│  • GetCgroupDriver(): multi-source driver detection     │
│  • DetectCgroupVersion(): version detection with env    │
│    override for testing                                 │
└─────────────────────────────────────────────────────────┘
```

### Cgroup Version Detection

The system implements robust cgroup version detection using multiple methods:

#### 1. Filesystem Magic Number Detection

```go
func IsCgroupsV2(unifiedMountpoint string) bool {
    var st unix.Statfs_t
    err := unix.Statfs(unifiedMountpoint, &st)
    if err != nil {
        // Error handling
    }
    return st.Type == unix.CGROUP2_SUPER_MAGIC
}
```

- Uses `statfs` system call to check filesystem type
- `CGROUP2_SUPER_MAGIC` (0x63677270) identifies cgroup v2 filesystems
- Based on proven methods from container runtime projects like runc

#### 2. Test Environment Override

```go
func DetectCgroupVersion(cgroupRoot string) (string, error) {
    // Support test environment override
    if testVersion := os.Getenv("VOLCANO_TEST_CGROUP_VERSION"); testVersion != "" {
        // Handle test cases
    }
    // Production detection logic
}
```

- Environment variable `VOLCANO_TEST_CGROUP_VERSION` for unit testing
- Supports values: "v1", "v2", "1", "2"
- Falls back to filesystem detection in production

### Cgroup Driver Detection

The system detects the cgroup driver (systemd vs cgroupfs) through multiple methods with fallback priority:

#### Priority Order:
1. **Environment Variable**: `CGROUP_DRIVER`
2. **Kubelet Config Files**: Parse configuration files
3. **Kubelet Process Arguments**: Extract from running process
4. **System Detection**: Filesystem-based detection
5. **Default Fallback**: cgroupfs

#### Config File Detection

```go
func getCgroupDriverFromKubeletConfig() string {
    configPaths := []string{
        "/var/lib/kubelet/config.yaml",
        "/var/lib/kubelet/kubeadm-flags.env",
    }
    // Parse each config file for cgroupDriver setting
}
```

#### Process-based Detection

```go
func getCgroupDriverFromKubeletProcess() string {
    kubeletPID := findKubeletProcess()
    cmdline := readProcessCmdline(kubeletPID)
    return parseCgroupDriverFromCmdline(cmdline)
}
```

- Finds kubelet process by scanning `/proc`
- Reads command line arguments from `/proc/<pid>/cmdline`
- Parses `--cgroup-driver` parameter

### Unified Manager Interface

The `CgroupManagerImpl` provides a unified implementation that supports both cgroup v1 and v2:

```go
type CgroupManager interface {
    GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error)
    GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error)
    GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error)
    GetCgroupVersion() string
}

type CgroupManagerImpl struct {
    cgroupDriver   string  // systemd or cgroupfs
    cgroupRoot     string  // mount point
    kubeCgroupRoot string  // kubelet cgroup root
    cgroupVersion  string  // v1 or v2
}
```

#### Path Generation Logic:

The `buildCgroupPath()` method adapts to the cgroup version automatically:

```go
func (c *CgroupManagerImpl) buildCgroupPath(cgroupSubsystem CgroupSubsystem, cgroupPath string) string {
    switch c.cgroupVersion {
    case CgroupV1:
        // v1: each controller has its own hierarchy
        return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath)
    case CgroupV2:
        // v2: unified hierarchy, no subsystem in path
        return filepath.Join(c.cgroupRoot, cgroupPath)
    }
}
```

**Cgroup v1 Example**: Separate subsystem paths
- CPU: `/sys/fs/cgroup/cpu/kubepods/burstable/pod<uid>`
- Memory: `/sys/fs/cgroup/memory/kubepods/burstable/pod<uid>`

**Cgroup v2 Example**: Unified hierarchy
- All subsystems: `/sys/fs/cgroup/kubepods/burstable/pod<uid>`

### File Structure Mapping

The system maps between v1 and v2 control files:

| Function | Cgroup v1 | Cgroup v2 | Format Change |
|----------|-----------|-----------|---------------|
| CPU Shares/Weight | `cpu.shares` | `cpu.weight` | shares(1024) → weight(100) |
| CPU Quota | `cpu.cfs_quota_us` | `cpu.max` | "200000" → "200000 100000" |
| CPU Burst | `cpu.cfs_burst_us` | `cpu.max.burst` | Same format |
| Memory Limit | `memory.limit_in_bytes` | `memory.max` | bytes → "max" for unlimited |
| Memory High | N/A | `memory.high` | New in v2 |
| Memory Low | N/A | `memory.low` | New in v2 |
| Memory Min | N/A | `memory.min` | New in v2 |

### Event Handler Adaptation

Each event handler is updated to support cgroup v2:

#### Resources Handler
- **Purpose**: Manages CPU and memory resource limits for containers
- **V2 Changes**:
  - Uses `cpu.weight` instead of `cpu.shares`
  - Uses `cpu.max` with "quota period" format
  - Uses `memory.max` with "max" for unlimited resources
  - Handles unified hierarchy paths

#### CPU QoS Handler
- **Purpose**: Sets CPU quality-of-service levels
- **V2 Changes**: Currently uses legacy `cpu.qos_level` file (vendor-specific)

#### Memory QoS Handler
- **Purpose**: Sets memory quality-of-service levels  
- **V2 Changes**: Currently uses legacy `memory.qos_level` file (vendor-specific)

#### CPU Burst Handler
- **Purpose**: Manages CPU burst capabilities
- **V2 Changes**:
  - Uses `cpu.max.burst` instead of `cpu.cfs_burst_us`
  - Reads quota from `cpu.max` format
  - Handles unified cgroup paths