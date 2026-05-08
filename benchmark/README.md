# Volcano Benchmark Framework

A Kind + KWOK based performance benchmark framework for the Volcano schedulers.

## Quick Start

```bash
cd benchmark
```

### 1. Setup Environment

```bash
make setup VOLCANO_VERSION=v1.14.2
```

This creates a Kind cluster, sets up KWOK simulated nodes, builds the audit-exporter image,
installs Volcano from the Helm repo, and deploys the monitoring stack (Prometheus, Grafana, audit-exporter).

To test local source code instead of a release version:
```bash
make setup
```

### 2. Run Tests

```bash
make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100
```

This creates 10 VCJobs, each with 100 replicas (minAvailable=100), to benchmark the Volcano
batch scheduler's gang scheduling performance. Scheduling latency metrics are automatically
collected from audit-exporter and printed after the test.

To benchmark bare pod scheduling (agent-scheduler, no gang/queue):
```bash
make test-pod-env PODS=500
```

For more details on each step, see the [Step-by-Step Guide](#step-by-step-guide).

### Other Useful Commands

```bash
# Dry run (skip post-test cleanup, useful for debugging)
DRY_RUN=true make test-gang-env JOBS=10 REPLICAS=10 MIN_AVAILABLE=10

# Run test using a yaml profile (see testcases/<scenario>/cases/ for examples)
make test-config SCENARIO=gang CONFIG=testcases/gang/cases/comprehensive.yaml
make test-config SCENARIO=pod CONFIG=testcases/pod/cases/my-profile.yaml

# Cleanup leftover resources (when paired with DRY_RUN mode)
make clean-vcjobs   # gang scenario
make clean-pods     # pod scenario

# Delete test resources, keep the cluster
make cleanup

# Delete everything including the cluster
make cleanup-all
```

## Layout

```text
third_party/kube-apiserver-audit-exporter/ # Customized apiserver audit log exporter for precise latency metrics
benchmark/
  config/              # Configurations for infrastructure (e.g., Kind cluster config and audit policies)
  manifests/           # Kubernetes YAML manifests for benchmark infrastructure setup
    audit-exporter/    # Dockerfile and DaemonSet for deploying the audit-exporter
    monitoring/        # Prometheus, Grafana dashboards, and kube-state-metrics settings
  scripts/             # Shell scripts invoked by the Makefile (cluster creation, setup, and cleanup)
  testcases/           # Source code for E2E test scenarios (e.g., 'gang', 'pod')
    util/              # Shared test utilities and constants
    <scenario>/        # Scenario-specific directory
      cases/           # Pre-defined test configurations (YAML) for quickly running different scales
      config/          # Dedicated configurations (like 'scheduler-config.yaml' and 'queue.yaml') for this scenario
      *_test.go        # Go test logic defining workloads and metrics evaluation
  results/             # (Generated dynamically) Contains JSON metric reports and test execution logs
```

## Scenarios

Each scenario lives under `testcases/<scenario>`:

| Scenario | Scheduler | Description                                                 |
|----------|-----------|-------------------------------------------------------------|
| `gang` | `volcano` (session-based) | Gang scheduling benchmark                                   |
| `pod` | `agent-scheduler` (per-pod) | Bare pod scheduling benchmark (default for agent-scheduler) |

## Monitoring

Metrics come from three sources:

1. **audit-exporter** — microsecond-precision latency from apiserver audit events (scheduler-agnostic)
2. **Volcano internal** — `volcano_session_*`, `volcano_plugin_*`, `volcano_action_*`, etc.
3. **kube-state-metrics** — pod / job lifecycle counts

Services are exposed via Kind NodePort + `extraPortMappings` on ports 30003/30004.

| Service | URL |
|---------|-----|
| Prometheus | `http://<host-ip>:30003` |
| Grafana | `http://<host-ip>:30004` (admin/admin) |
| Dashboard | `http://<host-ip>:30004/d/volcano-benchmark` |

### Audit-exporter metrics

The apiserver writes audit events to `/var/log/kubernetes/kube-apiserver-audit.log`
(configured in `config/kind-config.yaml`). The audit-exporter tails that file and exposes:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `pod_scheduling_latency_seconds` | Histogram | `namespace`, `user` | Time from pod creation to `pods/binding` creation. `user` identifies the scheduler |
| `batchjob_completion_latency_seconds` | Histogram | `namespace`, `user` | Time from job creation to completion (`batch.Job` and `batch.volcano.sh/Job`) |
| `api_requests_total` | Counter | `namespace`, `user`, `verb`, `resource`, `code` | Audited API request count |
| `pod_deleted_total` | Counter | `namespace`, `user`, `phase` | Pod deletions by terminal phase |
| `pod_completed_total` | Counter | `namespace`, `user`, `phase` | Pods reaching `Succeeded` or `Failed` |

Why audit logs: `metav1.MicroTime` precision (vs `metav1.Time` on pod objects), and
`pods/binding` is the universal binding entrypoint — works for any scheduler.

### Volcano internal metrics

Metrics exposed by the Volcano scheduler and controller-manager.
The Grafana dashboard panels that reference these metrics require the corresponding
Volcano version with the metrics instrumented.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `volcano_e2e_scheduling_latency_milliseconds` | Histogram | — | E2E scheduling latency (scheduling algorithm + binding) |
| `volcano_e2e_job_scheduling_latency_milliseconds` | Histogram | — | E2E job scheduling latency |
| `volcano_e2e_job_scheduling_duration` | Gauge | `job_name`, `queue`, `job_namespace` | E2E job scheduling duration |
| `volcano_plugin_scheduling_latency_milliseconds` | Histogram | `plugin`, `OnSession` | Plugin execution latency |
| `volcano_action_scheduling_latency_milliseconds` | Histogram | `action` | Action (allocate/reclaim/preempt) latency |
| `volcano_task_scheduling_latency_milliseconds` | Histogram | — | Task scheduling latency |
| `volcano_schedule_attempts_total` | Counter | `result` | Number of attempts to schedule pods |

## Step-by-Step Guide

### Prerequisites

- Go >= 1.24.0, Docker, kind >= 0.20.0, kubectl, helm >= 3.0, curl, jq, make

```bash
go version && docker info > /dev/null && kind version && kubectl version --client && helm version && jq --version
```

### Step 1: Create Kind Cluster

```bash
cd benchmark
make create-cluster
```

Creates cluster `volcano-benchmark` with config from `config/kind-config.yaml`.

### Step 2: Create KWOK Nodes

```bash
make create-nodes
```

Installs KWOK controller and creates simulated nodes:

| Variable | Default |
|---|---|
| `KWOK_NODE_COUNT` | `100` |
| `CPU_PER_NODE` | `32` |
| `MEMORY_PER_NODE` | `256Gi` |
| `KWOK_VERSION` | `v0.7.0` |

Override example: `make create-nodes KWOK_NODE_COUNT=200 CPU_PER_NODE=64`

### Step 3: Build Images

```bash
make build-images
```

When `VOLCANO_VERSION` is set, only the audit-exporter image is built
(Volcano components are pulled from the Helm repo). Otherwise all Volcano
component images are also built from local source.

### Step 4: Install Volcano

```bash
make install-volcano VOLCANO_VERSION=v1.14.2  # from Helm repo
make install-volcano                          # from local source
```

### Step 5: Install Monitoring

```bash
make install-monitoring
```

Deploys Prometheus + kube-state-metrics + Grafana + audit-exporter.

### Step 6: Run Tests

**Gang scheduling:**
```bash
# Quick run with env vars
make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100

# Run with a yaml profile
make test-config SCENARIO=gang CONFIG=testcases/gang/cases/comprehensive.yaml
```

**Bare pod scheduling:**
```bash
# Quick run with env vars
make test-pod-env PODS=500

# Run for specific scheduler(default is agent-scheduler)
make test-pod-env PODS=1000 SCHEDULER_NAME=agent-scheduler

# Run with a yaml profile
make test-config SCENARIO=pod CONFIG=testcases/pod/cases/my-profile.yaml
```

For details on YAML profile parameters, see `testcases/<scenario>/cases/comprehensive.yaml`.

### Step 7: View Results

Test results are automatically collected after each run:

- JSON report: `results/report-<timestamp>.json`
- Test log: `results/test-<scenario>-<timestamp>.log`
- Grafana dashboard: `http://<host-ip>:30004/d/volcano-benchmark`

### Step 8: Cleanup

```bash
make cleanup       # remove test resources, keep cluster
make cleanup-all   # remove everything
```

## Variables

### Global

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_NAME` | `volcano-benchmark` | Kind cluster name |
| `KWOK_NODE_COUNT` | `100` | Number of KWOK nodes |
| `CPU_PER_NODE` | `32` | CPU per KWOK node |
| `MEMORY_PER_NODE` | `256Gi` | Memory per KWOK node |
| `VOLCANO_VERSION` | _(empty)_ | Release tag (e.g. `v1.14.0`) to install from Helm repo |
| `DRY_RUN` | `false` | Skip post-test cleanup (useful for debugging) |

### Gang Scenario (`test-gang-env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `JOBS` | `10` | Number of VCJobs to create |
| `REPLICAS` | `100` | Replicas per task |
| `MIN_AVAILABLE` | `100` | MinAvailable for gang scheduling |

For all available parameters (network topology, partition policy, node selectors, etc.),
see `testcases/gang/cases/comprehensive.yaml`.

### Pod Scenario (`test-pod-env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `PODS` | `500` | Number of bare pods to create |
| `SCHEDULER_NAME` | `agent-scheduler` | The scheduler to benchmark |

For all available parameters, see `testcases/pod/cases/case-template.yaml`.

## Adding New Scenarios

1. Create `testcases/<scenario>/config/` with `scheduler-config.yaml` and `queue.yaml`
2. Create `testcases/<scenario>/*_test.go` with test logic
3. Run: `make setup && make test-config SCENARIO=<scenario> CONFIG=<profile>.yaml`
