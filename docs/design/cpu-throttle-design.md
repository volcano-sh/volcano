CPU Throttle Feature Design
===

Design Principles
---

The CPU Throttle feature is a new addition to Volcano Agent that dynamically adjusts Pod CPU quotas in response to node CPU resource pressure. It follows a "probe-event-handler" architecture pattern, implementing real-time monitoring and intelligent response to node CPU utilization.

Core Design Philosophy

1. **Gradual Throttling**: Avoids sudden performance drops by incrementally reducing CPU quotas

2. **Dual-Threshold Mechanism**: Uses activation threshold and protection watermark to prevent frequent state oscillations

3. **QoS Awareness**: Only throttles low-priority Pods while protecting critical workloads

4. **Runtime Configurability**: Supports dynamic configuration updates for flexible adaptation

Implementation Architecture
---

### Probe Layer (nodemonitor)

**Responsibility**: Monitor node CPU utilization and trigger throttling events

**Key Components**:

- `detectCPUThrottling()`: Core detection logic

- Configuration parameters: `cpuThrottlingThreshold` (activation threshold), `cpuProtectionWatermark` (protection watermark)

- State management: `cpuThrottlingActive` (current throttling state)

**State Transition Logic**:

```
Not throttling + CPU usage >= threshold → Start throttling (start)
Throttling + CPU usage >= threshold → Continue throttling (continue)
Throttling + CPU usage <= watermark → Stop throttling (stop)
```

### Event Layer

Event Type: `NodeCPUThrottleEvent`

```go
type NodeCPUThrottleEvent struct {
    TimeStamp time.Time
    Resource  v1.ResourceName
    Action    string // "start", "continue", "stop"
    Usage     int64  // Current CPU usage percentage
}
```

### Handler Layer (cputhrottle)

**Responsibility**: Execute actual CPU throttling operations

**Core Algorithm**:

- **Gradual Throttling**: Reduces CPU quota by configured percentage (default 10%) per step

- **Minimum Guarantee**: Ensures Pods retain minimum CPU quota (default 20%)

- **cgroup Operations**: Directly manipulates `cpu.cfs_quota_us` files

**Key Methods**:

```go
stepThrottleCPU()          // Execute stepped throttling
stopCPUThrottle()          // Restore original quotas
calculateSteppedQuota()    // Calculate new quota
```

Technical Implementation Details
---

**CPU Quota Calculation**

```
// Current quota = Read from cgroup file or calculate from Pod spec
// New quota = Current quota * (1 - throttle step percentage)
// Min quota = Original quota * minimum percentage
newQuota = max(currentQuota * (100-step)/100, minQuota)
```

**cgroup File Operations**

- Read: `/sys/fs/cgroup/cpu,cpuacct/.../cpu.cfs_quota_us`
- Write: Update quota value to corresponding cgroup file

**Configuration Hot Updates**
Supports dynamic updates via `ColocationConfig`:

- Throttle step percentage

- Minimum quota percentage

- Feature enable/disable

Monitoring and Observability
---

**Logging**

- Info Level: State change events (start/stop)
- V(2) Level: Continuous throttling events (continue)
- V(4) Level: Detailed detection information

## Configuration

Example Configuration

```yaml
cpuThrottlingConfig:
  enable: true
  cpuThrottlingThreshold: 85    # Start throttling at 85% CPU usage
  cpuProtectionWatermark: 60    # Stop throttling at 60% CPU usage
  cpuThrottlingStepPercent: 10    # Reduce quota by 10% per step
  cpuMinQuotaPercent: 20     # Minimum 20% quota retained
```