# HyperNode Controller: Standalone Module and Deployment

## Introduction

The HyperNode controller (network topology discovery, HyperNode CR reconciliation, and `NodeCount` status) is implemented as an independent Go module under the Volcano repository:

- **Module path**: `volcano.sh/hypernode`
- **Staging directory**: `staging/src/volcano.sh/hypernode/`

This allows operators to **build and deploy only the HyperNode controller** (`vc-hypernode-controller`) without shipping the rest of Volcano’s controllers, while keeping the default behavior: **Volcano controller-manager** continues to register the same logic via a thin adapter.

Functional behavior, ConfigMap format, and discovery sources are unchanged from [HyperNode Auto Discovery](./hyperNode-auto-discovery.md) and the [user guide](../user-guide/how_to_use_hypernode_auto_discovery.md).

## Goals

1. **Independent binary**: Compile `vc-hypernode-controller` from `volcano.sh/hypernode` with minimal dependencies (no full `vc-controller-manager` tree required at build time for consumers who vendor only this module).
2. **Backward compatibility**: Existing installs that run `vc-controller-manager` with `hyperNode-controller` enabled behave as before.
3. **Single source of truth for node matching**: Scheduler-side `GetMembers` delegates to `volcano.sh/hypernode/pkg/nodematch` so label/regex/exact semantics stay aligned with the controller.

## Repository Layout

```
staging/src/volcano.sh/hypernode/
├── go.mod                    # module volcano.sh/hypernode; replace volcano.sh/apis => ../apis
├── cmd/hypernode-controller/ # standalone entrypoint (flags, leader election, healthz)
├── pkg/
│   ├── api/                  # discovery config types, discoverer registry
│   ├── config/               # ConfigMap loader
│   ├── discovery/            # manager, label/ufm/... discoverers
│   ├── hypernode/            # Controller (reconcile, informers, queues)
│   ├── nodematch/            # MemberSelector → node names (shared with scheduler via volcano root)
│   ├── utils/                # CR create/update/delete helpers
│   ├── signals/              # shutdown context for the standalone binary
│   └── testutil/             # test helpers for HyperNode CRs
```

Volcano **root** `go.mod`:

```text
require volcano.sh/hypernode v0.0.0
replace volcano.sh/hypernode => ./staging/src/volcano.sh/hypernode
```

The in-tree adapter that plugs the module into controller-manager:

```text
pkg/controllers/hypernode/register.go   # registers framework.Controller → hn.Controller
```

## Deployment Modes

### Mode A: Bundled in Volcano controller-manager (default)

- **When to use**: Standard Volcano install; one process runs all selected controllers.
- **Behavior**: Blank import of `volcano.sh/volcano/pkg/controllers/hypernode` in `cmd/controller-manager/main.go` registers `hyperNode-controller` with the shared `framework`.
- **Controller gate name**: `hyperNode-controller` (unchanged; use `--controllers` to enable or disable).

### Mode B: Standalone `vc-hypernode-controller`

- **When to use**: You want topology discovery and HyperNode lifecycle **without** running job/queue/podgroup/… controllers in the same binary (smaller blast radius, separate upgrade cycle, or dedicated RBAC).
- **Binary**: `vc-hypernode-controller` (see [Build](#build)).
- **Leader election**: Default lease object name is **`vc-hypernode-controller`** (distinct from `vc-controller-manager`) so the two processes do not contend for the same lease **name**—but see [Important: avoid duplicate reconciliation](#important-avoid-duplicate-reconciliation).

### Important: avoid duplicate reconciliation

**Do not** run **both**:

- `vc-controller-manager` with `hyperNode-controller` **enabled**, and  
- `vc-hypernode-controller` **against the same cluster**,

or two controllers will compete to reconcile the same HyperNodes and discovery pipeline.

**Recommended**:

- **Standalone only**: disable HyperNode in controller-manager, e.g.  
  `--controllers=-hyperNode-controller` (together with your other controller gates), **or**
- **Bundled only**: do not deploy `vc-hypernode-controller`.

## Build

From the **Volcano repository root**:

```bash
make vc-hypernode-controller
# Output: _output/bin/vc-hypernode-controller
```

From inside the **module** (e.g. local development):

```bash
cd staging/src/volcano.sh/hypernode
go build -o vc-hypernode-controller ./cmd/hypernode-controller
```

Building the full Volcano controller-manager still includes HyperNode via `register.go`; no extra flag is required.

## Container image

Volcano release and CI build the same image naming convention as other components:

- **Image**: `${IMAGE_PREFIX}/vc-hypernode-controller:${TAG}` (defaults: `volcanosh/vc-hypernode-controller` and `latest` / release tag).
- **Dockerfile**: `installer/dockerfile/hypernode-controller/Dockerfile` (multi-stage build from repo root, same pattern as `vc-controller-manager`).
- **Build locally** (single platform, load into Docker):

  ```bash
  make vc-hypernode-controller-image
  ```

- **Build all release images** (including HyperNode):

  ```bash
  make images
  ```

`make release` runs `make images` before packaging; registry pushes use the same `IMAGE_PREFIX` / `TAG` / `BUILDX_OUTPUT_TYPE` as existing Volcano images (e.g. Docker Hub and GHCR workflows).

## Run (standalone binary)

Typical flags (see `--help` for the full set):

| Area | Notes |
|------|--------|
| API access | `--kubeconfig` / `$KUBECONFIG`, or in-cluster config |
| Leader election | Component-base flags (e.g. `--leader-elect`, `--leader-elect-resource-name`, namespace) |
| Health | `--enable-healthz`, `--healthz-address` (default listens on `:11252`) |
| TLS for health | `--ca-cert-file`, `--tls-cert-file`, `--tls-private-key-file` (all three required if used) |

Example (development):

```bash
./vc-hypernode-controller \
  --kubeconfig=$HOME/.kube/config \
  --leader-elect=true \
  --leader-elect-resource-namespace=volcano-system
```

Configuration for discovery remains the same as today: ConfigMap named `{release}-controller-configmap` in the namespace derived from env / defaults (see [user guide](../user-guide/how_to_use_hypernode_auto_discovery.md)).

## RBAC and ServiceAccount

The standalone binary needs API permissions equivalent to what the HyperNode controller used inside `vc-controller-manager` (HyperNode CR, Nodes, ConfigMaps, Secrets used by discoverers, etc.). The project’s default install manifests are oriented toward a single controller-manager Deployment; **if you deploy only `vc-hypernode-controller`**, create a `ServiceAccount`, `ClusterRole`, and `ClusterRoleBinding` that grant those permissions and attach the same ConfigMap/Secret wiring as your controller install.

(Reuse or split the existing controller-manager RBAC in your distribution as appropriate—exact YAML is environment-specific.)

## Relationship to the scheduler

`pkg/scheduler/api/hyper_node_info.go` **`GetMembers`** forwards to:

```text
volcano.sh/hypernode/pkg/nodematch.NodeNamesForSelector
```

So selector-to-node resolution in scheduling matches the controller’s `NodeCount` logic.

## Tests

- **HyperNode module**: `cd staging/src/volcano.sh/hypernode && go test ./...`
- **Volcano root `unit-test`**: includes the above path after volcano’s own packages (see `Makefile`).

## CI (GitHub Actions)

- **`docker_images.yaml`** runs `make images` and `make save-images`; **`vc-hypernode-controller`** is included in both targets, so CI builds and archives the image like other Volcano components.
- **`code_verify.yaml` / `release.yaml`** run `make lint`, `make verify`, and `sudo make unit-test`.  
  - `unit-test` runs HyperNode tests via `cd staging/src/volcano.sh/hypernode && go test ./...`.  
  - `lint` runs root `golangci-lint` **and** `go vet ./...` inside the HyperNode module.  
- **`verify-gofmt.sh`** scans repository `*.go` files (excluding vendor), including `staging/src/volcano.sh/hypernode/`.  
- **E2E** (`e2e_hypernode.yaml`, etc.) still use the bundled controller-manager image unless you change the test harness; standalone image is for optional operator deployments.

## References

- Design: [HyperNode Auto Discovery](./hyperNode-auto-discovery.md)
- Usage: [How to use HyperNode auto discovery](../user-guide/how_to_use_hypernode_auto_discovery.md)
- Topology-aware scheduling context: [Network Topology Aware Scheduling](./Network%20Topology%20Aware%20Scheduling.md)
