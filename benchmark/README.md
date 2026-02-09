# Volcano Benchmark Framework

A Kind + KWOK based performance benchmark framework for the Volcano scheduler.

## Quick Start

```bash
# Full workflow (local source, gang scenario): create cluster -> build images -> install -> run tests -> report
cd benchmark
make all SCENARIO=gang

# Full workflow (release version)
make all SCENARIO=gang VOLCANO_VERSION=v1.10.0

# Run a specific predefined test case under gang scheduling
make test-gang-20x50    # 20 jobs × 50 pods/job
make test-gang-10x100   # 10 jobs × 100 pods/job

# Run ad-hoc CLI test with custom parameters
make test-cli SCENARIO=gang JOBS=5 PODS=200 CPU=2 MEMORY=2Gi

# Cleanup
make cleanup            # Delete all resources except the kind cluster
make cleanup-all        # Delete all resources including the kind cluster
```

## Scenario-Based Architecture

Each test scenario has its own self-contained directory under `testcases/<scenario>/manifests/`, including:
- Scheduler configuration (which plugins to enable)
- Queue definition
- KWOK Stages (node heartbeat, pod lifecycle simulation)
- Job templates (for scenarios that need them)

The `SCENARIO` environment variable (default: `gang`) determines which scenario's manifests are used during setup.

### Available Scenarios

| Scenario | Description                  | Gang Plugin |
|----------|------------------------------|-------------|
| `default` | Baseline — default configmap | Disabled |
| `gang` | Gang scheduling   | Enabled |

## Two Test Execution Modes

### 1. Predefined Test Cases

Run pre-configured test cases with fixed parameters:

```bash
# Setup the environment for gang scenario
make setup SCENARIO=gang

# Run predefined test cases
make test-gang-20x50     # 20 jobs × 50 pods/job, minAvailable=50
make test-gang-10x100    # 10 jobs × 100 pods/job, minAvailable=100

# Run all gang test cases
make test                # Runs all tests in the current SCENARIO
```

### 2. Ad-hoc CLI Tests

Run custom tests with arbitrary parameters via command-line options:

```bash
# Via run-tests.sh directly
./scripts/run-tests.sh gang --jobs=5 --pods=200 --cpu=2 --memory=2Gi

# Via Makefile
make test-cli SCENARIO=gang JOBS=5 PODS=200 CPU=2 MEMORY=2Gi

# Or directly with go test
BENCHMARK_SCENARIO=gang BENCHMARK_JOBS=10 BENCHMARK_PODS=100 \
  BENCHMARK_CPU=1 BENCHMARK_MEMORY=1Gi \
  go test -v -run TestFromCLI -timeout 600s ./testcases/gang/
```

run-tests.sh CLI options (gang scenario):

| Option | Default | Description |
|--------|---------|-------------|
| `--jobs=N` | _(required)_ | Number of VCJobs to create |
| `--pods=N` | _(required)_ | Number of pods per job |
| `--cpu=N` | `1` | CPU request per pod |
| `--memory=SIZE` | `1Gi` | Memory request per pod |
| `--min-available=N` | same as --pods | minAvailable for gang scheduling |
| `--queue=NAME` | `benchmark-queue` | Volcano queue name |

CLI environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `BENCHMARK_JOBS` | _(required)_ | Number of VCJobs to create |
| `BENCHMARK_PODS` | _(required)_ | Number of pods per job |
| `BENCHMARK_CPU` | `1` | CPU request per pod |
| `BENCHMARK_MEMORY` | `1Gi` | Memory request per pod |
| `BENCHMARK_MIN_AVAILABLE` | same as PODS | minAvailable for gang scheduling |
| `BENCHMARK_QUEUE` | `benchmark-queue` | Volcano queue name |
| `BENCHMARK_SCENARIO` | `default` | Scenario name |

## Monitoring

- Prometheus: http://localhost:30090
- Grafana: http://localhost:30080 (admin/admin)

### Metrics Reference

| Metric | Source | Description |
|--------|--------|-------------|
| VCJob submission to pod creation latency | Go code `MeasurePodsCreationLatency()` | Pod creation completion timestamp - VCJob submission timestamp |
| Core scheduling latency | Prometheus: `volcano_e2e_scheduling_latency_milliseconds` | Volcano internal scheduling latency histogram |
| Pod status statistics | kube-state-metrics | Three curves: created / scheduled / deleted |

## Step-by-Step Testing Guide

### Prerequisites

Ensure the following tools are installed and available in your `PATH`:

- **Go** >= 1.24.0
- **Docker** (running)
- **kind** >= 0.20.0 — for creating local Kubernetes clusters
- **kubectl** — matching your cluster version
- **helm** >= 3.0 — for installing Volcano
- **curl** and **jq** — for collecting Prometheus metrics
- **make** — for running Makefile targets

Verify with:

```bash
go version && docker info > /dev/null && kind version && kubectl version --client && helm version && jq --version
```

### Step 1: Create the Kind Cluster

```bash
cd benchmark
make create-cluster
```

This creates a Kind cluster named `volcano-benchmark` with high API server QPS/burst settings and NodePort mappings for Prometheus (30090) and Grafana (30080). The kubeconfig is exported to `benchmark/kubeconfig`.

### Step 2: Create KWOK Simulated Nodes

```bash
make create-nodes SCENARIO=gang
```

By default, 100 KWOK nodes are created, each with 32 CPU and 256Gi memory. KWOK Stages are loaded from the scenario's manifests directory. Customize via environment variables:

```bash
KWOK_NODE_COUNT=200 CPU_PER_NODE=64 MEMORY_PER_NODE=512Gi make create-nodes SCENARIO=gang
```

### Step 3: Build Volcano Images (local mode only)

```bash
make build-images
```

This runs `make images` in the Volcano root directory and loads the resulting images into the Kind cluster. This step is automatically skipped when using `VOLCANO_VERSION`.

### Step 4: Install Volcano

**Option A — Local source (default):**

```bash
make install-volcano SCENARIO=gang
```

Installs Volcano from the local Helm chart at `installer/helm/chart/volcano`. Applies the scenario's scheduler config.

**Option B — Specific release version:**

```bash
make install-volcano SCENARIO=gang VOLCANO_VERSION=v1.10.0
```

Downloads and installs the specified version from the official Volcano Helm repository (`https://volcano-sh.github.io/volcano`). No local image build is needed.

### Step 5: Install Monitoring

```bash
make install-monitoring
```

Deploys Prometheus (with 1-second scrape interval), kube-state-metrics, and Grafana with a pre-configured dashboard. Access:

- Prometheus: http://localhost:30090
- Grafana: http://localhost:30080 (credentials: admin/admin)

### Step 6: Run Tests

Run all gang scheduling tests:

```bash
make test SCENARIO=gang
```

Run a specific test case:

```bash
make test-gang-20x50     # 20 jobs × 50 pods/job, minAvailable=50
make test-gang-10x100    # 10 jobs × 100 pods/job, minAvailable=100
```

Run ad-hoc CLI test:

```bash
make test-cli SCENARIO=gang JOBS=15 PODS=80 CPU=1 MEMORY=1Gi
```

Results are saved to `results/`.

### Step 7: Collect Report

```bash
make report
```

Queries Prometheus for scheduling latency (P50/P99), pod counts, and job E2E duration. Outputs a JSON report to `results/report-<timestamp>.json`.

### Step 8: View Results

Open the Grafana dashboard at http://localhost:30080/d/volcano-benchmark to see:

- **Pod Scheduling Progress**: time-series chart with created/scheduled/deleted pod counts (X-axis: time, Y-axis: pod count, 1-second refresh)
- **Scheduling Latency**: P50 and P99 latency over time
- **Job E2E Duration**: table of per-job scheduling durations

The Go test output also prints:
- Total pod count
- Pod creation latency (VCJob submit → all pods created)
- Throughput (pods/sec)

### Step 9: Cleanup

```bash
make cleanup          # Remove test resources, keep the cluster
make cleanup-all      # Remove everything including the Kind cluster
```

Cleanup does not require a `SCENARIO` parameter — it removes all VCJobs, pods, and queues regardless of scenario.

## Configuring Parameters

### Makefile Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_NAME` | `volcano-benchmark` | Kind cluster name |
| `SCENARIO` | `gang` | Test scenario (`gang`, `default`, etc.) |
| `KWOK_NODE_COUNT` | `100` | Number of KWOK simulated nodes |
| `CPU_PER_NODE` | `32` | CPU capacity per KWOK node |
| `MEMORY_PER_NODE` | `256Gi` | Memory capacity per KWOK node |
| `TEST_CASE` | `$(SCENARIO)` | Test case to run (used by `make test`) |
| `VOLCANO_VERSION` | _(empty)_ | Set to a release tag (e.g. `v1.10.0`) to install from Helm repo |

## Adding New Scenarios

1. Create `testcases/<scenario>/manifests/` with:
   - `scheduler-config.yaml` — Volcano scheduler configuration
   - `queue.yaml` — Volcano queue definition
   - KWOK Stage files (`node-heartbeat.yaml`, `pod-complete.yaml`, `pod-delete.yaml`)
   - Any job templates needed (e.g. `vcjob-template.yaml`)
2. Create `testcases/<scenario>/*_test.go` files with test logic
3. Run: `make setup SCENARIO=<scenario> && make test SCENARIO=<scenario>`

## Dependencies

- Go >= 1.24.0
- Docker
- kind >= 0.20.0
- kubectl
- helm >= 3.0
- curl, jq
- sigs.k8s.io/e2e-framework (Go module)
