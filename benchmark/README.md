# Volcano Benchmark Framework

A performance benchmark framework for the Volcano schedulers, supporting both
local Kind + KWOK clusters and existing multi-node Kubernetes clusters.

## Kind Cluster

Recommended for local development and quick validation. The framework creates a Kind
cluster, builds all images locally, and configures audit logging + monitoring automatically.

### Prerequisites

- Go >= 1.24.0, Docker, kind >= 0.20.0, kubectl, helm >= 3.0, curl, jq, make

```bash
go version && docker info > /dev/null && kind version && kubectl version --client && helm version && jq --version
```

### Quick Start

```bash
cd benchmark

# Setup: create Kind cluster, build images, install Volcano + monitoring
make setup VOLCANO_VERSION=v1.14.2

# Run gang scheduling benchmark
make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100

# Cleanup
make cleanup-all
```

To build and test Volcano from local source instead of a release version:
```bash
make setup
```

### Step-by-Step

#### 1. Create Kind Cluster

```bash
make create-cluster
```

Creates cluster `volcano-benchmark` with config from `config/kind-config.yaml`.
Apiserver audit logging and port mappings (30003/30004) are configured automatically.

#### 2. Create KWOK Nodes

```bash
make create-nodes
```

Installs KWOK controller and creates simulated nodes. Set `SKIP_KWOK=true` to skip
this step when using real cluster nodes.

| Variable | Default | Description |
|---|---|---|
| `KWOK_NODE_COUNT` | `100` | Number of simulated nodes |
| `CPU_PER_NODE` | `32` | CPU capacity per node |
| `MEMORY_PER_NODE` | `256Gi` | Memory capacity per node |
| `KWOK_VERSION` | `v0.7.0` | KWOK controller version |
| `ENABLE_TOPOLOGY` | `false` | Distribute nodes across topology domains and create HyperNode CRDs |
| `TOPOLOGY_RACKS` | `4` | Number of rack-level domains (tier 1) |
| `TOPOLOGY_SPINES` | `2` | Number of spine-level domains (tier 2) |
| `KWOK_NODE_LABELS` | *(empty)* | Comma-separated `key=value` labels for all nodes (e.g. auto-discovery) |

Override example: `make create-nodes KWOK_NODE_COUNT=200 CPU_PER_NODE=64`

**Topology-aware scheduling:**

When `ENABLE_TOPOLOGY=true`, nodes are evenly distributed across racks and spines,
and HyperNode CRDs are created to represent the hierarchy:

```
spine-0 (tier 2)
├── rack-0 (tier 1) ── kwok-node-0 .. kwok-node-24
├── rack-1 (tier 1) ── kwok-node-25 .. kwok-node-49
spine-1 (tier 2)
├── rack-2 (tier 1) ── kwok-node-50 .. kwok-node-74
└── rack-3 (tier 1) ── kwok-node-75 .. kwok-node-99
```

```bash
# Topology mode: create labeled nodes (HyperNode CRDs are created separately after Volcano install)
make create-nodes ENABLE_TOPOLOGY=true TOPOLOGY_RACKS=4 TOPOLOGY_SPINES=2

# Auto-discovery: only label nodes, hypernode-controller builds HyperNodes from ConfigMap
make create-nodes KWOK_NODE_LABELS="topology-rack=rack-0,topology-spine=spine-0"
```

> **Note:** When `ENABLE_TOPOLOGY=true`, this step only creates labeled KWOK nodes.
> HyperNode CRDs require the Volcano CRD to be installed first — use
> `make create-hypernodes` (step 4b) after Volcano installation.

#### 3. Build Images

```bash
make build-images
```

When `VOLCANO_VERSION` is set, only the audit-exporter image is built
(Volcano components are pulled from the Helm repo). Otherwise all Volcano
component images are also built from local source.

#### 4. Install Volcano

```bash
make install-volcano VOLCANO_VERSION=v1.14.2  # from Helm repo
make install-volcano                          # from local source
```

#### 4b. Create HyperNode Topology (optional)

If you used `ENABLE_TOPOLOGY=true` in step 2, create HyperNode CRDs now
(requires the `topology.volcano.sh` CRD from Volcano):

```bash
make create-hypernodes
# or with custom sizing (must match step 2):
make create-hypernodes TOPOLOGY_RACKS=8 TOPOLOGY_SPINES=4
```

> **Important:** `TOPOLOGY_RACKS` and `TOPOLOGY_SPINES` must match the values
> used in step 2 (`create-nodes`), otherwise the HyperNode labelSelectors
> won't match the corresponding nodes. If step 2 used defaults (4 racks / 2 spines),
> use defaults here as well.

For auto-discovery mode (using `KWOK_NODE_LABELS`), skip this step — the
hypernode-controller builds HyperNodes automatically based on node labels
and its ConfigMap configuration.

#### 5. Install Monitoring

```bash
make install-monitoring
```

Deploys Prometheus + kube-state-metrics + Grafana + audit-exporter.

#### 6. Run Tests

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

# Run for specific scheduler (default is agent-scheduler)
make test-pod-env PODS=1000 SCHEDULER_NAME=agent-scheduler

# Run with a yaml profile
make test-config SCENARIO=pod CONFIG=testcases/pod/cases/my-profile.yaml
```

For details on YAML profile parameters, see `testcases/<scenario>/cases/comprehensive.yaml`.

#### 7. View Results

Test results are automatically collected after each run:

- Audit-exporter report: `results/report-<timestamp>.json` (generated when Prometheus is available)
- Pod-timestamp fallback/debug report: `results/pod-latency-<timestamp>.json` (generated via `make collect-pod-latency`)
- Test log: `results/test-<scenario>-<timestamp>.log`
- Grafana dashboard: `http://localhost:30004/d/volcano-benchmark`

#### 8. Cleanup

```bash
make cleanup       # remove test resources, keep cluster
make cleanup-all   # remove everything including the cluster
```

### Other Commands

```bash
# Dry run (skip post-test cleanup, useful for debugging)
DRY_RUN=true make test-gang-env JOBS=10 REPLICAS=10 MIN_AVAILABLE=10

# Collect fallback/debug latency from pod timestamps (requires DRY_RUN, no audit-exporter needed)
make collect-pod-latency

# Run test using a yaml profile
make test-config SCENARIO=gang CONFIG=testcases/gang/cases/comprehensive.yaml

# Cleanup leftover resources (when paired with DRY_RUN mode)
make clean-vcjobs   # gang scenario
make clean-pods     # pod scenario
```

---

## Existing Cluster

For running benchmarks on bare-metal, cloud-managed, or self-hosted multi-node
Kubernetes clusters. When `USE_EXISTING_CLUSTER=true`, the framework skips Kind
cluster creation and image build. Users are responsible for ensuring Volcano and
required images are available on the cluster.

### Prerequisites

1. **kubectl access**: A valid `KUBECONFIG` pointing to the target cluster with cluster-admin privileges.
2. **Helm**: Installed and able to reach the cluster (only needed when installing Volcano via the framework).
3. **Volcano**: Either let the framework install it via Helm, or pre-install your own
   build and set `SKIP_INSTALL_VOLCANO=true`.

### Quick Start

```bash
cd benchmark

# Volcano not installed yet, let the framework install it
make setup USE_EXISTING_CLUSTER=true VOLCANO_VERSION=v1.14.2

# Volcano already installed (e.g., a custom or modified build)
make setup USE_EXISTING_CLUSTER=true SKIP_INSTALL_VOLCANO=true

# Both Volcano and monitoring already deployed, only create KWOK nodes
make setup USE_EXISTING_CLUSTER=true SKIP_INSTALL_VOLCANO=true SKIP_INSTALL_MONITORING=true

# Cluster already has real nodes, skip KWOK entirely
make setup USE_EXISTING_CLUSTER=true SKIP_INSTALL_VOLCANO=true SKIP_KWOK=true

# Custom kubeconfig
KUBECONFIG=/path/to/kubeconfig make setup USE_EXISTING_CLUSTER=true SKIP_INSTALL_VOLCANO=true
```

### Step-by-Step

#### 1. Create KWOK Nodes (optional)

If your cluster already has real worker nodes, skip this step by setting `SKIP_KWOK=true`.
When `SKIP_KWOK=true`, KWOK controller installation is skipped and pod templates will not
include KWOK-specific `nodeSelector` or `tolerations`.

```bash
# With KWOK (simulated nodes)
make create-nodes KWOK_NODE_COUNT=200 CPU_PER_NODE=64

# Without KWOK (using real cluster nodes)
# Set SKIP_KWOK=true during setup or when running tests
```

#### 2. Install Volcano (optional)

If Volcano is not already running on the cluster:
```bash
make install-volcano VOLCANO_VERSION=v1.14.2
```

If Volcano is already installed, set `SKIP_INSTALL_VOLCANO=true` during setup to skip this step.

#### 2b. Create HyperNode Topology (optional)

To benchmark topology-aware scheduling on an existing cluster, you can create
HyperNode CRDs that represent your cluster's network topology. This requires
Volcano to be installed first (for the `topology.volcano.sh` CRD).

**Option A: Using KWOK nodes with topology labels (simulated topology)**

If you created KWOK nodes with `ENABLE_TOPOLOGY=true` in step 1:
```bash
make create-hypernodes TOPOLOGY_RACKS=4 TOPOLOGY_SPINES=2
```

**Option B: Using real nodes with custom labels (real topology)**

For existing clusters with real nodes, label your nodes to reflect the actual
network topology, then create corresponding HyperNodes:

```bash
# 1. Label your real nodes with topology information
kubectl label node worker-01 worker-02 worker-03 topology-rack=rack-0
kubectl label node worker-04 worker-05 worker-06 topology-rack=rack-1

# 2. Create HyperNodes (edit the script or apply manually)
make create-hypernodes TOPOLOGY_RACKS=2 TOPOLOGY_SPINES=1
```

> **Note:** The `create-hypernodes.sh` script uses `type: kwok` in the rack
> labelSelector. For real nodes, either add `type: kwok` label to your nodes,
> or apply HyperNode manifests directly with your own labelSelector. Example:
>
> ```yaml
> apiVersion: topology.volcano.sh/v1alpha1
> kind: HyperNode
> metadata:
>   name: rack-0
> spec:
>   tier: 1
>   tierName: rack
>   members:
>     - type: Node
>       selector:
>         labelMatch:
>           matchLabels:
>             topology-rack: "rack-0"
> ```

**Option C: Auto-discovery via hypernode-controller**

If the hypernode-controller is enabled with the `label` discoverer in the
Volcano ConfigMap, you only need to label nodes — the controller will automatically
create HyperNode resources:

```bash
kubectl label node worker-01 topology-rack=rack-0 topology-spine=spine-0
# ... controller auto-creates rack-0, spine-0 HyperNodes
```

The scheduler uses a virtual `ClusterHyperNode` as a global root, so you
only need rack (tier 1) and spine (tier 2) levels — no explicit root HyperNode is required.

#### 3. Build and Deploy Audit-Exporter (optional)

The audit-exporter is a custom component built from this repository. It is not published
to DockerHub, so you need to build the image and push it to a registry your cluster can access.

```bash
# Build the image locally
make build-audit-exporter

# Tag and push to your registry
docker tag volcanosh/kube-apiserver-audit-exporter:dev myregistry.example.com/audit-exporter:dev
docker push myregistry.example.com/audit-exporter:dev
```

You also need to enable apiserver audit logging on your cluster. See
[Enabling Apiserver Audit Logging](#enabling-apiserver-audit-logging) for instructions.

If you do not need microsecond-precision latency, you can skip audit-exporter entirely
and use the [pod-timestamp fallback](#fallback-pod-timestamps-second-precision) instead.

#### 4. Install Monitoring (optional)

```bash
# Using the default audit-exporter image name
make install-monitoring

# Using a custom image from your private registry
make install-monitoring AUDIT_EXPORTER_IMAGE=myregistry.example.com/audit-exporter:dev
```

If monitoring is already deployed on the cluster, set `SKIP_INSTALL_MONITORING=true` during setup.

#### 5. Run Tests

The test scripts access Prometheus to collect audit-exporter metrics. On existing
clusters, Prometheus is not accessible via `localhost:30003` by default (unlike Kind
which has `extraPortMappings`). Use `kubectl port-forward` or set `PROM_URL`.

If you skipped KWOK during setup (`SKIP_KWOK=true`), you must also pass `SKIP_KWOK=true`
when running tests, so that pod/vcjob templates do not include KWOK-specific
`nodeSelector` and `tolerations`:

```bash
# Option 1: port-forward (recommended)
kubectl port-forward svc/prometheus-service -n volcano-monitoring 30003:8080 &
make test-gang-env SKIP_KWOK=true JOBS=10 REPLICAS=100 MIN_AVAILABLE=100

# Option 2: NodePort via node IP
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
PROM_URL=http://${NODE_IP}:30003 make test-gang-env SKIP_KWOK=true JOBS=10 REPLICAS=100 MIN_AVAILABLE=100
```

If Prometheus is not reachable, the audit-exporter report is skipped and tests still run normally.

**Without audit-exporter (pod-timestamp fallback):**

If you did not set up audit-exporter, use `DRY_RUN=true` to keep pods alive after the test,
then collect fallback/debug latency from pod timestamps:

```bash
DRY_RUN=true make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100
make collect-pod-latency
make clean-vcjobs   # cleanup when done
```

See [Scheduling Latency Collection](#scheduling-latency-collection) for details on both approaches.

#### 6. Monitoring Access

| Service | Kind cluster | Existing cluster |
|---------|-------------|-----------------|
| Prometheus | `http://localhost:30003` | `kubectl port-forward` or `http://<node-ip>:30003` |
| Grafana | `http://localhost:30004` | `kubectl port-forward` or `http://<node-ip>:30004` |

Port-forward example:
```bash
kubectl port-forward svc/prometheus-service -n volcano-monitoring 30003:8080 &
kubectl port-forward svc/grafana -n volcano-monitoring 30004:3000 &
# Access Prometheus at http://localhost:30003, Grafana at http://localhost:30004
```

#### 7. Cleanup

```bash
# Remove test resources, keep the cluster (never deletes external clusters)
make cleanup

# Remove Volcano and monitoring as well (still does NOT delete the cluster itself)
make cleanup-all
```

---

## Reference

### Layout

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

### Scenarios

Each scenario lives under `testcases/<scenario>`:

| Scenario | Scheduler | Description                                                 |
|----------|-----------|-------------------------------------------------------------|
| `gang` | `volcano` (session-based) | Gang scheduling benchmark                                   |
| `pod` | `agent-scheduler` (per-pod) | Bare pod scheduling benchmark (default for agent-scheduler) |

### Scheduling Latency Collection

There are two approaches to collect scheduling latency metrics:

#### Recommended: audit-exporter (microsecond precision)

The audit-exporter reads apiserver audit log events and exposes pod scheduling latency
as Prometheus histograms with `MicroTime` precision. Metrics persist in Prometheus
independently of pod lifecycle, so pods can be cleaned up immediately after the test.

On Kind clusters, audit logging and audit-exporter are configured automatically.
On existing clusters, you need to:
1. Enable apiserver audit logging, see [Enabling Apiserver Audit Logging](#enabling-apiserver-audit-logging)
2. Build and push the audit-exporter image, see [Existing Cluster Step 3](#3-build-and-deploy-audit-exporter-optional)

#### Fallback/debug: pod timestamps (second precision)

If audit logging is not enabled and you do not want to modify the apiserver configuration,
you can collect scheduling latency from pod object timestamps (`CreationTimestamp` to
`PodScheduled` condition's `LastTransitionTime`). This is not the primary report source
for Volcano benchmarks; prefer Prometheus scheduler metrics or audit-exporter metrics
when they are available.

Limitations:
- **Second-level precision only** (`metav1.Time`), sub-second latencies show as 0ms
- **Requires DRY_RUN mode**, pods must still exist when collecting metrics

```bash
# Step 1: Run test with DRY_RUN=true to keep pods after the test
DRY_RUN=true make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100

# Step 2: Collect latency from pod timestamps
make collect-pod-latency

# Step 3: Clean up pods manually when done
make clean-vcjobs   # for gang scenario
make clean-pods     # for pod scenario
```

The report is saved to `results/pod-latency-<timestamp>.json`.

### Enabling Apiserver Audit Logging

For microsecond-precision scheduling latency, the apiserver must have audit logging enabled.

Add the following flags to your kube-apiserver configuration:

```
--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml
--audit-log-path=/var/log/kubernetes/kube-apiserver-audit.log
--audit-log-maxsize=10240
--audit-log-maxage=7
--audit-log-maxbackup=3
```

The audit policy file can be found at
`third_party/kube-apiserver-audit-exporter/audit-policy.yaml` in this repository.
Copy it to your control-plane node(s) and restart the apiserver.

For kubeadm-based clusters, edit `/etc/kubernetes/manifests/kube-apiserver.yaml` on the
control-plane node to add the flags and volume mounts:

```yaml
# Add to spec.containers[0].command:
- --audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml
- --audit-log-path=/var/log/kubernetes/kube-apiserver-audit.log

# Add to spec.containers[0].volumeMounts:
- name: audit-policies
  mountPath: /etc/kubernetes/policies
  readOnly: true
- name: audit-logs
  mountPath: /var/log/kubernetes
  readOnly: false

# Add to spec.volumes:
- name: audit-policies
  hostPath:
    path: /etc/kubernetes/policies
    type: DirectoryOrCreate
- name: audit-logs
  hostPath:
    path: /var/log/kubernetes
    type: DirectoryOrCreate
```

After saving, the kubelet will automatically restart the apiserver static pod.

### Monitoring

Metrics come from three sources:

1. **audit-exporter**: microsecond-precision latency from apiserver audit events (scheduler-agnostic)
2. **Volcano internal**: `volcano_session_*`, `volcano_plugin_*`, `volcano_action_*`, etc.
3. **kube-state-metrics**: pod / job lifecycle counts

Services are exposed via NodePort on ports 30003/30004.

| Service | URL |
|---------|-----|
| Prometheus | `http://<host-ip>:30003` |
| Grafana | `http://<host-ip>:30004` (admin/admin) |
| Dashboard | `http://<host-ip>:30004/d/volcano-benchmark` |

#### Audit-exporter metrics

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

#### Volcano internal metrics

Metrics exposed by the Volcano scheduler and controller-manager.
The Grafana dashboard panels that reference these metrics require the corresponding
Volcano version with the metrics instrumented.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `volcano_e2e_scheduling_latency_milliseconds` | Histogram | — | Duration of one scheduling session execution |
| `volcano_open_session_duration_milliseconds` | Histogram | — | Duration of opening a scheduling session |
| `volcano_e2e_job_scheduling_latency_milliseconds` | Histogram | — | Per-job duration from creation to last task assumed |
| `volcano_e2e_job_scheduling_duration` | Gauge | `job_name`, `queue`, `job_namespace` | Per-job duration from creation to last task assumed |
| `volcano_plugin_scheduling_latency_milliseconds` | Histogram | `plugin`, `OnSession` | Plugin execution latency |
| `volcano_action_scheduling_latency_milliseconds` | Histogram | `action` | Action (allocate/reclaim/preempt) latency |
| `volcano_task_scheduling_latency_milliseconds` | Histogram | `stage` | Task latency from creation to `Watched`, `Assumed`, `PreBound`, or `Bound` |
| `volcano_scheduling_stage_duration_milliseconds` | Histogram | `stage` | Per-task processing time for `Predicate`, `Scoring`, `PreBind`, or `Bind` |
| `volcano_worker_scheduling_cycle_duration_milliseconds` | Histogram | — | Duration of one agent worker scheduling cycle |
| `volcano_update_snapshot_duration_milliseconds` | Histogram | — | Duration of updating the agent scheduling snapshot from cache |
| `volcano_controller_job_to_pod_creation_latency_milliseconds` | Histogram | — | Latency from VCJob creation to pod creation |
| `volcano_schedule_attempts_total` | Counter | `result` | Number of attempts to schedule pods |

### Variables

#### Global

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_NAME` | `volcano-benchmark` | Kind cluster name |
| `USE_EXISTING_CLUSTER` | `false` | Skip Kind cluster creation and image build, use existing kubeconfig |
| `SKIP_INSTALL_VOLCANO` | `false` | Skip Volcano installation (use pre-installed Volcano on the cluster) |
| `SKIP_INSTALL_MONITORING` | `false` | Skip monitoring stack installation (Prometheus, Grafana, audit-exporter) |
| `SKIP_KWOK` | `false` | Skip KWOK controller installation and node creation. Must also be passed when running tests (e.g. `make test-gang-env SKIP_KWOK=true`) to exclude KWOK nodeSelector/tolerations from pod templates |
| `AUDIT_EXPORTER_IMAGE` | `volcanosh/kube-apiserver-audit-exporter:dev` | Audit-exporter container image (override for private registries) |
| `KUBECONFIG` | `~/.kube/config` | Path to kubeconfig file (used for existing cluster access) |
| `KWOK_NODE_COUNT` | `100` | Number of KWOK nodes |
| `CPU_PER_NODE` | `32` | CPU per KWOK node |
| `MEMORY_PER_NODE` | `256Gi` | Memory per KWOK node |
| `KWOK_VERSION` | `v0.7.0` | KWOK controller version |
| `VOLCANO_VERSION` | _(empty)_ | Release tag (e.g. `v1.14.0`) to install from Helm repo |
| `VOLCANO_SCHEDULER_KUBE_API_QPS` | `5000` | Volcano scheduler Kubernetes client QPS used when installing Volcano |
| `VOLCANO_SCHEDULER_KUBE_API_BURST` | `10000` | Volcano scheduler Kubernetes client burst used when installing Volcano |
| `VOLCANO_CONTROLLER_KUBE_API_QPS` | `5000` | Volcano controller-manager Kubernetes client QPS used when installing Volcano |
| `VOLCANO_CONTROLLER_KUBE_API_BURST` | `10000` | Volcano controller-manager Kubernetes client burst used when installing Volcano |
| `PROM_URL` | `http://localhost:30003` | Prometheus URL for metrics collection |
| `DRY_RUN` | `false` | Skip post-test cleanup (useful for debugging or pod-timestamp collection) |

#### Gang Scenario (`test-gang-env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `JOBS` | `10` | Number of VCJobs to create |
| `REPLICAS` | `100` | Replicas per task |
| `MIN_AVAILABLE` | `100` | MinAvailable for gang scheduling |

For all available parameters (network topology, partition policy, node selectors, etc.),
see `testcases/gang/cases/comprehensive.yaml`.

#### Pod Scenario (`test-pod-env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `PODS` | `500` | Number of bare pods to create |
| `SCHEDULER_NAME` | `agent-scheduler` | The scheduler to benchmark |

For all available parameters, see `testcases/pod/cases/case-template.yaml`.

### Adding New Scenarios

1. Create `testcases/<scenario>/config/` with `scheduler-config.yaml` and `queue.yaml`
2. Create `testcases/<scenario>/*_test.go` with test logic
3. Run: `make setup && make test-config SCENARIO=<scenario> CONFIG=<profile>.yaml`
