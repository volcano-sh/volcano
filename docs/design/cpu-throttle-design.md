CPU Throttle Feature Design
===

Design Principles
---

The CPU Throttle feature dynamically adjusts the BestEffort root cgroup CPU quota based on node allocatable CPU and real-time CPU usage. It follows a "monitor-event-handler" architecture pattern, implementing periodic monitoring with conservative updates to avoid jitter.

Core Design Philosophy

1. **Quota Budgeting**: Computes a BE CPU quota budget from allocatable CPU minus real-time CPU usage.

2. **Jitter Control**: Use a jitter limit percentage to avoid frequent updates for small jitter.

3. **QoS Awareness**: Only applies throttling to BE cgroup; higher-priority workloads are protected by request accounting.

4. **Runtime Configurability**: Supports dynamic configuration updates for flexible adaptation.

Implementation Architecture
---

### Probe Layer (cpumonitor)

**Responsibility**: Monitor node CPU utilization and trigger throttling events

**Key Components**:

- `detectCPUQuota()`: Core detection logic

- Configuration parameters: `cpuThrottlingThreshold` (allocatable percentage limit), `cpuJitterLimitPercent` (minimum delta before emitting updates), `cpuRecoverLimitPercent` (maximum increase per update)

- State management: `lastCPUQuotaMilli` (last emitted BE quota in millicores)

**Quota Computation Logic**:

```
allowedMilli = allocatableMilli * cpuThrottlingThreshold / 100
availableBEMilli = max(allowedMilli - usedMilli, 0)
cap availableBEMilli increase to lastCPUQuotaMilli * (1 + cpuRecoverLimitPercent/100)
emit event if first run, or lastCPUQuotaMilli == 0 and availableBEMilli != 0,
or |availableBEMilli - lastCPUQuotaMilli| >= lastCPUQuotaMilli * cpuJitterLimitPercent / 100
```

### Event Layer

Event Type: `NodeCPUThrottleEvent`

```go
type NodeCPUThrottleEvent struct {
    TimeStamp time.Time
    Resource  v1.ResourceName
    CPUQuotaMilli int64 // BE quota in millicores
}
```

### Handler Layer (cputhrottle)

**Responsibility**: Execute actual CPU throttling operations

**Core Algorithm**:

- **Quota Application**: Converts millicores to CFS quota and writes to the BE root cgroup

- **cgroup Operations**: Directly manipulates `cpu.cfs_quota_us` files

**Key Methods**:

```go
applyBEQuota()             // Apply BE root quota
quotaFromMilliCPU()        // Convert millicores to quota
writeBEQuota()             // Update cgroup quota file
```

Technical Implementation Details
---

**CPU Quota Calculation**

```
quota = milliCPU * 100000 / 1000
quota <= 0 means unlimited (-1)
```

**cgroup File Operations**

- Read: cgroup path from QoS BE root
- Write: Update quota value to corresponding cgroup file

**Configuration Hot Updates**
Supports dynamic updates via `ColocationConfig`:

- `cpuThrottlingThreshold`

- `cpuJitterLimitPercent`

- `cpuRecoverLimitPercent`

- Feature enable/disable

## Configuration

Example Configuration

```yaml
cpuThrottlingConfig:
  enable: true
  cpuThrottlingThreshold: 80    # Allow BE quota up to 80% of allocatable CPU
  cpuJitterLimitPercent: 1      # Emit updates when quota changes by >=1%
  cpuRecoverLimitPercent: 10    # Cap quota increases to 10% per update
```