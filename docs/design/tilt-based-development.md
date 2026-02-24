# Tilt-Based Local Development Workflow

## Motivation

### Problem

Developing and testing Volcano components locally requires building Docker images, pushing them to a registry, deploying to a Kubernetes cluster, and restarting pods for every code change. This cycle is slow and error-prone, creating friction for contributors who need fast feedback loops during scheduler, controller, or webhook development.

The existing `hack/local-up-volcano.sh` script provides a one-shot local cluster setup, but it does not support iterative development. Each code change requires tearing down and rebuilding the environment, which can take several minutes.

### Design Goal

1. Enable sub-minute code-to-running-pod feedback for all Volcano control-plane components (scheduler, controller-manager, webhook-manager).
2. Provide a single-command workflow (`make dev-up`) that provisions a local Kind cluster with a container registry and deploys Volcano with live-reloading.
3. Support multi-architecture development (amd64 and arm64) without manual configuration.
4. Integrate unit tests and end-to-end tests as on-demand resources within the same development environment.
5. Keep all development tooling self-contained in `_output/bin` to avoid polluting the host system.

## Architecture Overview

The workflow is built on three core tools:

- **[Kind](https://kind.sigs.k8s.io/)** — Local Kubernetes cluster running in Docker containers.
- **[ctlptl](https://github.com/tilt-dev/ctlptl)** — Declarative cluster lifecycle manager that provisions Kind clusters with an attached local container registry.
- **[Tilt](https://tilt.dev/)** — Development environment that watches source files, builds container images, and live-updates running pods.

```
Developer workstation
+-------------------------------------------------------------------+
|                                                                   |
|  make dev-up                                                      |
|    |                                                              |
|    +-> dev-install-tilt    (download Tilt binary to _output/bin)  |
|    +-> dev-install-kind    (download Kind binary to _output/bin)  |
|    +-> dev-install-ctlptl  (download ctlptl binary to _output/bin)|
|    +-> dev-create-kind-cluster  (ctlptl apply)                    |
|    +-> tilt up                                                    |
|                                                                   |
+-------------------------------------------------------------------+
|                                                                   |
|  Kind cluster: kind-volcano-dev                                   |
|  +-------------------------------------------------------------+  |
|  |  volcano-system namespace                                   |  |
|  |  +------------------+  +------------------+                 |  |
|  |  | vc-scheduler     |  | vc-controller-   |                 |  |
|  |  | (live-reload)    |  | manager          |                 |  |
|  |  +------------------+  | (live-reload)    |                 |  |
|  |                        +------------------+                 |  |
|  |  +------------------+  +------------------+                 |  |
|  |  | vc-webhook-      |  | admission-init   |                 |  |
|  |  | manager          |  | (Job)            |                 |  |
|  |  | (live-reload)    |  +------------------+                 |  |
|  |  +------------------+                                       |  |
|  |                                                             |  |
|  |  volcano-monitoring namespace                               |  |
|  |  +------------------+  +-------+  +------------------+      |  |
|  |  | prometheus       |  |grafana|  | kube-state-      |      |  |
|  |  | (:3001)          |  |(:3000)|  | metrics          |      |  |
|  |  +------------------+  +-------+  +------------------+      |  |
|  +-------------------------------------------------------------+  |
|                                                                   |
|  Local registry: volcano-registry:5005                            |
+-------------------------------------------------------------------+
```

### Component Relationship

1. **ctlptl** provisions a Kind cluster (`kind-volcano-dev`) with 1 control-plane node and 4 worker nodes, plus a local Docker registry (`volcano-registry:5005`).
2. **Tilt** watches the Volcano source tree and uses `docker_build` with `live_update` to sync code changes into running containers.
3. Inside each container, an **entrypoint.sh** script runs the Volcano binary as a child process. When Tilt syncs new files and touches `/tmp/reload`, the script stops the process, rebuilds the binary in-container, and restarts it.
4. The Helm chart is rendered by Tilt's `helm()` function with development-specific value overrides (increased log verbosity, relaxed scheduling period, control-plane tolerations).

## File Layout

All Tilt-specific files live under `hack/tilt/`:

```
hack/tilt/
+-- Makefile                        # Make targets for dev lifecycle
+-- Tiltfile                        # Tilt orchestration (builds, deploys, resources)
+-- Dockerfile.tilt                 # Multi-stage Dockerfile for scheduler/controller/webhook
+-- Dockerfile.admission-init.tilt  # Lightweight Dockerfile for admission init Job
+-- entrypoint.sh                   # Live-reload entrypoint for Volcano binaries
+-- ctlptl-kind-registry.yaml       # ctlptl declarative config for Kind + registry
+-- values-tilt-override.yaml       # Helm value overrides for local development
```

The `hack/tilt/Makefile` is included by the root `Makefile`, exposing `dev-*` targets at the top level.

## Detailed Design

### Cluster Provisioning

The cluster is managed declaratively through ctlptl. The configuration is composed at apply time from two files:

1. `ctlptl-kind-registry.yaml` — defines the local Docker registry and the cluster identity:

```yaml
apiVersion: ctlptl.dev/v1alpha1
kind: Registry
name: volcano-registry
port: 5005
---
apiVersion: ctlptl.dev/v1alpha1
kind: Cluster
name: kind-volcano-dev
product: kind
registry: volcano-registry
```

2. `hack/e2e-kind-config.yaml` — the shared Kind cluster configuration (feature gates, containerd patches, node topology, kubeadm patches). This file is the single source of truth for Kind cluster settings, shared between the Tilt dev workflow and the e2e test workflow (`run-e2e-kind.sh`).

At `make dev-create-kind-cluster` time, the Makefile merges these two files: it appends `e2e-kind-config.yaml` content (stripped of `kind:` and `apiVersion:` headers) under the `kindV1Alpha4Cluster:` key and pipes the result to `ctlptl apply -f -`. This ensures any Kind cluster config changes (e.g., new feature gates, node count, kubeadm patches) are automatically picked up by both workflows without duplication.

ctlptl handles:
- Creating the local Docker registry container.
- Creating the Kind cluster with the registry pre-configured so that images pushed to `localhost:5005` are accessible inside the cluster.
- Idempotent applies — running `ctlptl apply` when the cluster already exists is a no-op.

### Image Building and Live-Reload

Tilt builds four container images:

| Image | Dockerfile | Live-reload | Purpose |
|---|---|---|---|
| `volcanosh/vc-scheduler` | `Dockerfile.tilt` | Yes | Batch scheduler |
| `volcanosh/vc-controller-manager` | `Dockerfile.tilt` | Yes | Controller manager |
| `volcanosh/vc-webhook-manager` | `Dockerfile.tilt` | Yes | Webhook admission controller |
| `volcanosh/vc-admission-init` | `Dockerfile.admission-init.tilt` | No | One-shot TLS certificate init Job |

The three main components share a single `Dockerfile.tilt` parameterized by `COMPONENT` build arg. The Dockerfile:

1. Starts from `golang:1.25.0` (matching the project's Go version).
2. Copies source code and runs `go mod download`.
3. Builds the binary with `GOOS=linux GOARCH=${TARGETARCH}`.
4. Uses `dumb-init` as PID 1 to forward signals correctly.
5. Delegates to `entrypoint.sh` for runtime process management.

The `live_update` configuration syncs `pkg/`, `cmd/<component>`, `third_party/`, and `entrypoint.sh` into the running container and touches `/tmp/reload` to trigger a rebuild.

#### Live-Reload Mechanism

The `entrypoint.sh` script implements a file-watch loop:

```
1. Build and start the Volcano binary as a background child process.
2. Loop every 1 second:
   a. Check if /tmp/reload exists.
   b. If yes: stop child, rebuild binary (go build), start child, remove /tmp/reload.
```

The rebuild uses `GOARCH="$(go env GOARCH)"` to detect the container's architecture at runtime, supporting both amd64 and arm64 Kind clusters.

### Multi-Architecture Support

All download URLs and build commands use architecture-aware variables:

- **Tilt and ctlptl downloads** (`hack/tilt/Makefile`): A `HOSTARCH` variable maps `uname -m` output to release archive naming (`x86_64` stays as-is, `aarch64` becomes `arm64`).
- **Docker image builds** (`Dockerfile.tilt`): Uses `ARG TARGETARCH` (set automatically by Docker BuildKit) for cross-compilation.
- **In-container rebuilds** (`entrypoint.sh`): Uses `go env GOARCH` to detect the runtime architecture.

### Helm Integration

Tilt renders the Volcano Helm chart using its built-in `helm()` function, combining:

1. The default `values.yaml` from the chart.
2. A `values-tilt-override.yaml` with development-specific settings:
   - `image_pull_policy: IfNotPresent` (images come from the local registry).
   - Scheduler and controller log verbosity at level 5.
   - Scheduling period reduced to 1 second.
   - Control-plane tolerations on all components (so pods can schedule on Kind's control-plane node if needed).

### Resource Grouping

Tilt resources are organized into labeled groups for the Tilt UI:

- **volcano**: Core Volcano resources (namespaces, CRDs, admission, scheduler, controllers, roles).
- **monitoring**: Prometheus, Grafana, kube-state-metrics.
- **tests-install**: Manual install targets for test dependencies (KWOK, Ginkgo).
- **tests**: Unit tests and per-suite e2e test triggers (all manual, on-demand).

Resource dependencies ensure correct ordering:
- `volcano-admission` depends on `volcano-namespaces`, `volcano-crds`, and `volcano-admission-init`.
- `volcano-scheduler` and `volcano-controllers` depend on `volcano-namespaces`.

### Testing Integration

The Tiltfile registers test suites as `local_resource` entries with `trigger_mode=TRIGGER_MODE_MANUAL` and `auto_init=False`:

- `unit-tests` - Runs `make unit-test`.
- `e2e-tests-all` - Runs all e2e suites sequentially.
- Individual suite targets: `e2e-tests-jobp`, `e2e-tests-jobseq`, `e2e-tests-schedulingbase`, `e2e-tests-schedulingaction`, `e2e-tests-vcctl`, `e2e-tests-dra`, `e2e-tests-hypernode`, `e2e-tests-cronjob`.
- `e2e-tests-schedulingbase-focused-example` — A pre-configured single-test example (see [Focused Test Execution](#focused-test-execution) below).

Test dependencies (KWOK, Ginkgo) are registered as separate manual install resources under the `tests-install` label. E2E test resources declare `resource_deps` on `install-ginkgo` (and `install-kwok` for `e2e-tests-all`), which means Tilt will block a triggered test from running until its dependencies have reached the Ready state. However, since the dependencies are also manual-trigger resources, they are **not** triggered automatically — the developer must trigger them explicitly on first use. Once a dependency has been triggered and reaches Ready, it stays Ready for the remainder of the Tilt session, so subsequent test runs only require triggering the test resource itself.

The typical first-run workflow is:

1. Trigger the test resource (e.g., `make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase`).
2. The test enters `Pending` state because `install-ginkgo` is not yet Ready.
3. Trigger the dependency (`make dev-tilt-trigger RESOURCE=install-ginkgo`).
4. Once the dependency completes, the test unblocks and runs automatically.
5. On subsequent runs, only step 1 is needed — the dependency is already Ready.

#### E2E Test Runner Integration

Running Ginkgo E2E tests during local development is traditionally cumbersome: the CI script `hack/run-e2e-kind.sh` handles cluster creation, KWOK setup, Volcano installation, and Ginkgo invocations as a single pipeline, making it unusable against an already-running Tilt cluster. Developers would otherwise need to construct manual `ginkgo` commands with the correct suite paths, `--focus`/`--skip` flags, and `KUBECONFIG` plumbing.

The Tilt integration solves this through three mechanisms:

**1. `run-ginkgo-suite()` — centralized Ginkgo invocation.**

A function in `hack/run-e2e-kind.sh` centralizes all Ginkgo flag construction:

```bash
run-ginkgo-suite <suite_path> <default_focus> <default_skip> [ginkgo_flags...]
```

Every test suite (in both CI and Tilt) calls this function instead of invoking `ginkgo` directly. The function handles `--focus` and `--skip` flag assembly, ensuring consistent behavior across all execution contexts. Additional ginkgo flags (e.g., `--slow-spec-threshold`, `-p`) are passed through as variadic arguments.

**2. `E2E_LOCAL_ONLY=1` — skip bootstrapping for Tilt-managed clusters.**

When `E2E_LOCAL_ONLY=1` is set, `hack/run-e2e-kind.sh` skips:
- Kind cluster creation (`kind-up-cluster`)
- KWOK installation (`install-kwok-with-helm`)
- Volcano Helm installation (`install-volcano`)
- Ginkgo installation (`install-ginkgo-if-not-exist` — assumed pre-installed via Tilt's `install-ginkgo` resource)

The script proceeds directly to running the requested test suite against the existing cluster. This reduces test invocation from minutes to seconds.

**3. `E2E_FOCUS` and `E2E_SKIP` — runtime overrides for test selection.**

Two environment variables override the per-suite default focus and skip patterns:

```bash
# Run only "Gang scheduling" tests, skipping sig-tagged and Full Occupied tests
E2E_FOCUS="Gang scheduling" E2E_SKIP="\[sig-.*\]|Full Occupied" \
  E2E_LOCAL_ONLY=1 E2E_TYPE=SCHEDULINGBASE hack/run-e2e-kind.sh
```

When `E2E_FOCUS` or `E2E_SKIP` is set, it takes precedence over the suite's hardcoded defaults. When unset, the defaults apply unchanged. This lets developers narrow test scope without editing any files.

##### Focused test execution

The Tiltfile includes a pre-configured example resource for running a single test:

```
e2e-tests-schedulingbase-focused-example
  E2E_FOCUS = "Gang scheduling"    (default, overridable via env)
  E2E_SKIP  = "\[sig-.*\]|Full Occupied"  (default, overridable via env)
  Suite     = SCHEDULINGBASE
```

This resource demonstrates single-test execution within Tilt. Developers iterating on a specific feature can:

1. **Copy this resource pattern** in the Tiltfile, changing the `E2E_FOCUS` default to match their test.
2. **Override at runtime** by setting `E2E_FOCUS` and `E2E_SKIP` environment variables before starting Tilt (they are passed through to `hack/run-e2e-kind.sh`).
3. **Trigger from the Tilt UI or CLI** — `make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase-focused-example` — getting results for a single test in seconds rather than waiting for the entire suite.

This is particularly valuable for scheduler and controller development, where a full E2E suite run takes 10-30 minutes but a single focused test completes in under a minute.

##### CI and Tilt alignment

Both CI and Tilt call the same `hack/run-e2e-kind.sh` script with the same `run-ginkgo-suite()` function. The only difference is the environment:

| Aspect | CI (`run-e2e-kind.sh`) | Tilt (`E2E_LOCAL_ONLY=1`) |
|---|---|---|
| Cluster creation | `kind-up-cluster` | Skipped (cluster exists) |
| Volcano install | `install-volcano` | Skipped (Tilt manages it) |
| Ginkgo install | `install-ginkgo-if-not-exist` | Skipped (Tilt `install-ginkgo` resource) |
| Test invocation | `run-ginkgo-suite()` | `run-ginkgo-suite()` (identical) |
| Focus/skip | Per-suite defaults | Defaults or `E2E_FOCUS`/`E2E_SKIP` overrides |

This single-source-of-truth approach ensures that a test passing locally in Tilt will also pass in CI (and vice versa).

### Version Management

Tool versions follow a single-source-of-truth principle:

| Tool | Version Variable | Defined In |
|---|---|---|
| Kind | `KIND_VERSION` | Root `Makefile` |
| Tilt | `TILT_VERSION` | `hack/tilt/Makefile` |
| ctlptl | `CTLPTL_VERSION` | `hack/tilt/Makefile` |

The root `Makefile` already defines `KIND_VERSION` for CI usage. The Tilt Makefile requires it as a prerequisite (`ifndef KIND_VERSION ... $(error ...)`), ensuring consistency between CI and local development.

## Usage

### Quick Start

```bash
# Start the development environment (installs tools, creates cluster, starts Tilt)
make dev-up

# In another terminal, check resource status
make dev-tilt-status

# View logs for a specific component
make dev-tilt-logs RESOURCE=volcano-scheduler

# Wait for all resources to be ready
make dev-wait-ready
```

### Development Workflow

1. Run `make dev-up` to start Tilt.
2. Edit any Go source file under `pkg/`, `cmd/`, or `third_party/`.
3. Tilt automatically syncs the changed files into the running container.
4. The entrypoint script detects the sync, rebuilds the binary, and restarts the process.
5. Check logs in the Tilt UI (http://localhost:10350) or via `make dev-tilt-logs`.

### Running Tests

Test dependencies (`install-ginkgo`, `install-kwok`) must be triggered once per Tilt session before running e2e tests. After the first trigger they stay Ready for subsequent runs.

```bash
# First time: trigger test dependency installation (once per session)
make dev-tilt-trigger RESOURCE=install-ginkgo

# Run unit tests (no dependencies needed)
make dev-tilt-trigger RESOURCE=unit-tests

# Run a specific e2e suite
make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase

# If the test stays in Pending, trigger its dependency first
# (install-ginkgo for most suites, install-kwok additionally for e2e-tests-all)

# Run a focused single test (uses E2E_FOCUS/E2E_SKIP env vars)
make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase-focused-example

# Check test results
make dev-tilt-logs RESOURCE=e2e-tests-schedulingbase
```

### Teardown

```bash
# Stop Tilt but keep the cluster
make dev-down

# Full cleanup: stop Tilt, delete cluster, remove tool binaries
make dev-clean
```

### Available Make Targets

| Target | Description |
|---|---|
| `dev-up` | Install tools, create cluster, start Tilt |
| `dev-down` | Stop Tilt, leave cluster running |
| `dev-clean` | Stop Tilt, delete cluster and registry, remove tool binaries |
| `dev-status` | Show cluster and connection info |
| `dev-tilt-status` | Show status of all Tilt-managed resources |
| `dev-tilt-logs` | Show logs (all or `RESOURCE=<name>`) |
| `dev-tilt-describe` | Describe a Tilt resource (`RESOURCE=<name>`) |
| `dev-tilt-trigger` | Manually trigger a resource (`RESOURCE=<name>`) |
| `dev-wait-ready` | Block until all resources reach Ready state |
| `dev-install-kind` | Install Kind binary to `_output/bin` |
| `dev-install-tilt` | Install Tilt binary to `_output/bin` |
| `dev-install-ctlptl` | Install ctlptl binary to `_output/bin` |
| `dev-create-kind-cluster` | Create Kind cluster and registry via ctlptl |
| `dev-delete-kind-cluster` | Delete Kind cluster and registry |

## Design Decisions

### Why Kind over k3d

Kind (Kubernetes IN Docker) was chosen over k3d for the following reasons:

- Kind is the de facto standard for local Kubernetes development in the Kubernetes ecosystem.
- Volcano's CI already uses Kind for e2e testing, ensuring parity between local development and CI.
- Kind uses `kubeadm`-bootstrapped clusters with unmodified upstream Kubernetes, giving higher fidelity for testing scheduler behavior.
- k3d uses k3s (a trimmed-down distribution) which may mask issues related to full Kubernetes API behavior.

### Why ctlptl over raw Kind commands

ctlptl provides declarative cluster management with built-in local registry support:

- A single YAML file defines both the cluster and its registry, replacing multi-step imperative setup scripts.
- Idempotent operations: `ctlptl apply` only creates resources that do not already exist.
- Registry wiring is automatic — ctlptl configures Kind nodes to trust the local registry without manual containerd config patches.
- Cascade deletion (`--cascade=true`) ensures clean teardown of both cluster and registry.

### Why Tilt over Skaffold and Other Tools

Several tools exist for Kubernetes-native development workflows. This section compares
the options considered and explains why Tilt is the best fit for Volcano.

#### Comparison Matrix

| Capability | [Tilt](https://tilt.dev/) | [Skaffold](https://skaffold.dev/) | [DevSpace](https://devspace.sh/) | [Telepresence](https://www.telepresence.io/) |
|---|---|---|---|---|
| File-level live sync into running containers | Yes (`live_update`) | Partial (`sync` in v2, limited) | Yes (`sync`) | N/A (intercept-based) |
| In-container rebuild on sync | Yes (via custom entrypoint) | No (rebuilds image or restarts pod) | Yes (via hooks) | N/A |
| Native Helm chart rendering | Yes (`helm()` built-in) | Yes (`helm` deployer) | Yes (helm integration) | No |
| Resource dependency DAG | Yes (explicit `resource_deps`) | No | No | No |
| Web UI with live status | Yes (built-in at :10350) | No (CLI only) | Yes (optional UI) | No |
| Manual-trigger resources | Yes (`TRIGGER_MODE_MANUAL`) | No | No | No |
| Context safety guard | Yes (`allow_k8s_contexts`) | Yes (`kubeContext`) | Yes (`vars.DEVSPACE_CONTEXT`) | Partial |
| Starlark scripting | Yes (full Starlark) | No (YAML only) | No (YAML + hooks) | No |
| Active maintenance (2025) | Yes | Maintenance mode | Yes | Yes |

#### Why Tilt wins for Volcano

**1. File-granularity live sync without image rebuilds.**
Volcano's Go source tree is large (~500k+ lines across `pkg/`, `cmd/`, `staging/`).
A full Docker image rebuild on every change takes 30-90 seconds even with layer caching.
Tilt's `live_update` syncs only the changed `.go` files into the running container and
triggers an in-container `go build`, bringing incremental feedback down to 5-15 seconds.
Skaffold's sync support is limited to interpreted languages (Python, Node.js) — it
cannot trigger a recompilation step after syncing Go source files, so it falls back to
full image rebuilds for compiled languages.

**2. Resource dependency DAG matches Volcano's deployment ordering.**
Volcano has strict deployment dependencies: CRDs must exist before admission webhooks
register, the admission-init Job must complete before the admission deployment starts,
and namespaces must exist before anything else. Tilt's `resource_deps` models this
DAG explicitly. Skaffold deploys manifests in a flat sequence and has no built-in
dependency mechanism, requiring workarounds like init containers or manual ordering.

**3. Manual-trigger test resources.**
The Tiltfile registers unit tests and eight separate e2e test suites as on-demand
resources. Developers can trigger them from the Tilt UI or CLI without leaving the
development loop. Skaffold has no concept of manual-trigger resources — everything
in the pipeline runs automatically or not at all.

**4. Starlark scripting for complex orchestration.**
The Tiltfile uses Starlark (a Python-like language) to loop over components, share
Dockerfiles via build args, conditionally include monitoring resources, and compose
Helm values. Skaffold's YAML-only configuration requires verbose repetition for
parameterized builds and cannot express conditional logic.

**5. Skaffold's uncertain future.**
Google announced in late 2023 that Skaffold is in maintenance mode. While it remains
functional, new feature development has stopped. Tilt continues active development
under Docker, Inc.

#### Why not DevSpace?

DevSpace offers comparable sync and in-container build capabilities. However:

- DevSpace lacks a resource dependency DAG, which Volcano's deployment ordering requires.
- DevSpace's Helm integration is functional but less mature than Tilt's native `helm()` function, which handles value layering and set overrides naturally.
- DevSpace's configuration format (devspace.yaml) is less expressive than Starlark for looping over multiple components with shared build logic.
- Tilt has stronger adoption in the Kubernetes ecosystem (used by Cluster API, Crossplane, Cilium, and similar infrastructure projects), providing better community support and proven patterns for controller/operator development.

#### Why not Telepresence?

Telepresence takes a fundamentally different approach: instead of syncing code into
cluster containers, it intercepts traffic from the cluster and routes it to a locally
running process. This is powerful for debugging a single service but is a poor fit
for Volcano because:

- Volcano has three tightly coupled control-plane components (scheduler, controller-manager, webhook-manager) that need to run simultaneously. Telepresence intercepts work per-service, not per-cluster.
- Scheduler behavior depends on cluster state (nodes, pods, queues) that is difficult to replicate locally outside the cluster.
- The webhook-manager requires in-cluster TLS certificates generated by the admission-init Job, which cannot work with a local process.

### Why Tilt is ideal for Volcano specifically

Beyond the general tool comparison, Tilt aligns with Volcano's development model
in ways that are specific to Kubernetes scheduler/controller projects:

- **Multi-component coordination.** Volcano is not a single binary — it is three cooperating control-plane processes plus an admission init Job. Tilt's resource model treats each as a first-class resource with health checks, logs, and dependency ordering, giving developers visibility into the full system rather than one component at a time.
- **Scheduler testing requires real cluster state.** Volcano's scheduler makes decisions based on node resources, pod groups, queue configurations, and CRD state. Unlike a web application where you can mock backends, meaningful scheduler development requires a running cluster with actual Kubernetes objects. Tilt's Kind integration provides this with zero manual setup.
- **Helm chart is the source of truth.** Volcano is deployed via Helm in production. By rendering the same Helm chart for local development (with override values), Tilt ensures that template changes, RBAC rules, and ConfigMap updates are tested in the development loop rather than discovered at deploy time.
- **CI parity.** Volcano's CI already uses Kind clusters. Tilt reuses the same Kind version and cluster configuration, so issues caught locally reproduce in CI and vice versa.

### Why in-container builds

The `Dockerfile.tilt` runs builds inside the container rather than on the host:

- Eliminates the need for a local Go toolchain on the developer's machine.
- Ensures the build environment matches the container's OS and architecture exactly.
- `go mod download` is cached in the Docker layer, so only changed source files trigger recompilation.
- Live-update syncs source files and triggers `go build` inside the container, achieving sub-minute rebuild times for incremental changes.

## Limitations and Future Work

- **Linux-only tool downloads**: The Makefile downloads Linux-specific Tilt and ctlptl binaries. macOS and Windows developers would need to install these tools separately or extend the download logic.
- **No GPU support**: The Kind cluster does not configure GPU passthrough. GPU-dependent scheduling features cannot be tested locally.
- **Single cluster topology**: The current setup uses a fixed 1 control-plane + 4 worker node topology. A future enhancement could make node count configurable.
- **No agent-scheduler or vc-agent support**: Only the three core control-plane components are included. Adding agent-scheduler and vc-agent images would extend coverage to the full Volcano stack.

### Agentic Development Workflows

The Tilt-based workflow has an additional benefit for AI coding agent workflows.
When AI agents assist with Volcano development, the development environment's
safety boundaries become critical.

#### The kubectl and Helm risk for agents

AI coding agents that interact with Kubernetes clusters directly via `kubectl` and
`helm` face significant risks:

- **Destructive operations.** A single `kubectl delete` or `helm uninstall` can destroy
  cluster state. An agent that misinterprets a prompt could delete CRDs, wipe namespaces,
  or uninstall the system it is developing against.
- **Stateful side effects.** Unlike file edits (which can be reverted via `git checkout`),
  cluster mutations are stateful and often irreversible. Deleted PersistentVolumeClaims,
  evicted pods, and removed finalizers cannot be undone.
- **Credential exposure.** Agents with `kubectl` access inherit the user's kubeconfig,
  which may contain credentials for production clusters. A context switch error could
  direct destructive commands at the wrong cluster.
- **Unbounded blast radius.** `kubectl apply -f` on a malformed manifest, `helm upgrade`
  with incorrect values, or `kubectl patch` with a bad JSON path can cascade into
  cluster-wide failures.

#### How Tilt mitigates these risks

The Tilt-based workflow provides a safety layer that makes agent-assisted development
viable:

- **`allow_k8s_contexts` as a hard guard.** The Tiltfile explicitly lists allowed
  Kubernetes contexts (`kind-volcano-dev`). Tilt refuses to operate against any other
  context, making it impossible to accidentally target a production cluster.
- **Declarative-only mutations.** All cluster changes flow through Tilt's declarative
  resource model. There is no need for agents to run `kubectl apply` or `helm install`
  directly — Tilt handles deployment as a side effect of source file changes.
- **Make targets as the agent interface.** Agents interact with the development
  environment through Make targets (`dev-up`, `dev-down`, `dev-tilt-status`,
  `dev-tilt-logs`), which are safe, idempotent, and well-scoped. This eliminates
  the need to grant agents direct `kubectl` or `helm` access.
- **Reproducible teardown.** If an agent corrupts the cluster state, `make dev-clean`
  destroys the entire Kind cluster and registry, and `make dev-up` recreates it from
  scratch in minutes. The blast radius is bounded to a disposable local cluster.
- **Source-driven feedback loop.** Agents edit Go source files, Tilt syncs and rebuilds.
  The agent never needs to reason about Kubernetes resource lifecycles, manifest
  ordering, or Helm release state. The complexity is encapsulated in the Tiltfile.

This design means an AI coding agent can safely develop Volcano components by editing
source files and reading logs, without ever needing cluster-admin privileges or direct
access to Kubernetes APIs.
