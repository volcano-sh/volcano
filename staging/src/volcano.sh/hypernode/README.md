# volcano.sh/hypernode

Go module containing the **HyperNode controller**: network topology discovery integration, HyperNode CR reconciliation, and related helpers.

This code lives under Volcano’s monorepo at `staging/src/volcano.sh/hypernode/`. It is **not** a separate GitHub repository; release and review follow the main [volcano-sh/volcano](https://github.com/volcano-sh/volcano) workflow.

## Module overview

| Path | Purpose |
|------|---------|
| `cmd/hypernode-controller/` | Standalone process: flags, leader election, health server hookup |
| `pkg/hypernode/` | Core `Controller` and Options |
| `pkg/discovery/` | Discovery manager and source implementations (UFM, label, …) |
| `pkg/config/` | Load `networkTopologyDiscovery` from the controller ConfigMap |
| `pkg/nodematch/` | `NodeNamesForSelector` — resolve `MemberSelector` to node names; shared with scheduler `GetMembers` |
| `pkg/api/` | Shared discovery config types and discoverer registration |
| `pkg/utils/` | Kubernetes CR helpers |

The root Volcano module imports this module via `replace` and registers the controller with `vc-controller-manager` using `pkg/controllers/hypernode/register.go`.

## Build

```bash
# From Volcano repo root
make vc-hypernode-controller
make vc-hypernode-controller-image   # OCI image: ${IMAGE_PREFIX}/vc-hypernode-controller:${TAG}

# Or inside this directory
go build -o vc-hypernode-controller ./cmd/hypernode-controller
```

`go.mod` uses `replace volcano.sh/apis => ../apis` for local API development, consistent with other staging modules.

## Test

```bash
go test ./...
```

## Documentation

See [docs/design/hypernode-standalone-controller.md](../../../../docs/design/hypernode-standalone-controller.md) (deployment modes, coexistence with controller-manager, RBAC notes).
