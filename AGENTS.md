# AGENTS.md — Volcano

## Overview

Volcano is a CNCF incubating Kubernetes-native batch scheduling system for AI/ML/DL, big data, and HPC workloads. It extends `kube-scheduler` with gang scheduling, resource fairness, queue management, and advanced topology awareness.

## Tech Stack

- **Language:** Go 1.25.0
- **Module:** `volcano.sh/volcano`
- **Kubernetes:** k8s.io/\* v0.35.3 (k8s.io/kubernetes v1.35.3)
- **Frameworks:** controller-runtime, cobra/pflag, Ginkgo/Gomega (e2e)
- **Linting:** golangci-lint
- **Build:** Makefile, docker buildx (multi-arch: linux/amd64, linux/arm64)

## Directory Layout

```
cmd/                  # Entry points (7 binaries)
  scheduler/          #   vc-scheduler
  controller-manager/ #   vc-controller-manager
  webhook-manager/    #   vc-webhook-manager
  agent/              #   vc-agent (node-level colocation)
  agent-scheduler/    #   vc-agent-scheduler
  cli/                #   vcctl + subcommands (vcancel, vresume, etc.)
  network-qos/        #   network-qos CNI plugin
pkg/                  # Core library
  scheduler/          #   Scheduling framework (actions/ + plugins/)
  controllers/        #   Controllers (job, queue, podgroup, cronjob, etc.)
  webhooks/           #   Shared webhook logic (admission, router, schema)
staging/src/volcano.sh/apis/  # CRD types (batch, scheduling, flow, bus, etc.)
config/crd/           # Generated CRD YAML manifests
installer/            # Helm chart, Dockerfiles, all-in-one YAML
hack/                 # Build/test/release scripts
test/                 # E2E test suites
```

## Key Binaries

| Binary                  | Path                      | Purpose                                                                 |
| ----------------------- | ------------------------- | ----------------------------------------------------------------------- |
| `vc-scheduler`          | `cmd/scheduler/`          | Core batch scheduler with 8 actions + 20+ plugins                       |
| `vc-controller-manager` | `cmd/controller-manager/` | Manages all controllers (job, queue, podgroup, cronjob, jobflow, ...)   |
| `vc-webhook-manager`    | `cmd/webhook-manager/`    | Admission webhooks for all CRDs                                         |
| `vc-agent`              | `cmd/agent/`              | Node-level colocation agent + network-qos CNI                           |
| `vc-agent-scheduler`    | `cmd/agent-scheduler/`    | Agent-side scheduling for colocation                                    |
| `vcctl`                 | `cmd/cli/`                | CLI with subcommands (vcancel, vresume, vsuspend, vjobs, vqueues, vsub) |

## Architecture

```
                  +-------------------+
                  | vc-webhook-manager |
                  +---------+---------+
                            |
  +------------------+      |      +---------------------+
  | vc-controller-   |      |      |   vc-scheduler       |
  | manager          |<-----+----->|   (actions+plugins)  |
  | (job, queue, ...)|             |                     |
  +------------------+             +---------------------+
           |                                |
           v                                v
         Kubernetes API Server + CRDs
           ^
           |
  +------------------+
  |   vc-agent       |
  +------------------+
```

### Scheduler Architecture (`pkg/scheduler/`)

- **Actions** (8): allocate, preempt, reclaim, backfill, enqueue, gangpreempt, gangreclaim, shuffle
- **Plugins** (20+): binpack, capacity, cdp, conformance, deviceshare, drf, extender, gang, numaaware, overcommit, predicates, priority, proportion, rescheduling, sla, tdm, usage, and more
- **Framework:** Session management, event hooks, statement management

### Controllers (`pkg/controllers/`)

- job (state machine: pending→running→terminating→...), queue, podgroup, cronjob, jobflow/jobtemplate, sharding, hypernode, colocationconfig, garbagecollector

## Development Commands

```bash
make all                    # Build all binaries
make vc-scheduler           # Build single binary
make unit-test              # Run unit tests
make lint                   # Run golangci-lint
make verify                 # gofmt + generated code verification
make generate-code          # Regenerate code (deepcopy, clients, etc.)
make manifests              # Regenerate CRD YAMLs
make e2e                    # E2E tests (requires kind cluster)
make images                 # Build Docker images (multi-arch)
make generate-yaml          # Generate all-in-one installer YAML
make clean                  # Clean build output
```

## Code Conventions

- **License header:** Apache 2.0 header on every Go file (see existing files for template).
- **Naming:** camelCase Go conventions. Exported types PascalCase.
- **Error handling:** Return errors, prefer `fmt.Errorf("...: %w", err)` for wrapping.
- **Testing:** Use standard `testing` package for unit tests (`*_test.go` alongside source). E2E uses Ginkgo/Gomega.
- **Logging:** Use `k8s.io/klog/v2`.
- **CRD types:** Defined in `staging/src/volcano.sh/apis/`, generated with `controller-gen`.
- **Imports:** Group standard library, third-party, and internal imports separated by blank lines.
- **Code generation:** Run `make generate-code` after modifying API types.
- **Comments:** Go-style `// Comment` (not block comments except for license headers).

## Code Generation

- `make generate-code` runs `hack/update-gencode.sh` (deepcopy, client, informer, lister).
- `make manifests` regenerates CRD YAMLs under `config/crd/`.
- Generated files are checked in; run and commit them after API changes.

## Code Quality

### Linting (`make lint`)

- Run `make lint` which invokes `golangci-lint`.
- **Enabled linters:** `govet`, `depguard`, `ineffassign`, `staticcheck`, `unused`, `whitespace`.
- **Formatters:** `gofmt`, `goimports` (with `volcano.sh` as local prefix for import grouping).
- **Excluded paths:** vendor, test, example, third_party.
- Disabled/enabled staticcheck checks are configured in `.golangci.yml` under `linters.settings.staticcheck.checks`.

### Import Ordering

Imports must be grouped and sorted: standard library → third-party → `volcano.sh/` internal packages, separated by blank lines. `goimports` with `local-prefixes: volcano.sh` enforces this.

### Formatting

- All Go files must be formatted with `gofmt`. Enforced via `make verify` (`hack/verify-gofmt.sh`).
- No trailing whitespace; enforced by `whitespace` linter.

### Deprecation Checks (`depguard`)

- `k8s.io/klog` is blocked — use `k8s.io/klog/v2` instead.
- `io/ioutil` is blocked — use `io` or `os` packages instead (deprecated since Go 1.16).

### License Compliance

- License linting via `make lint-licenses` using `config/license-lint.yaml`.
- **Allowed licenses:** Apache-2.0, MIT, BSD, ISC, and others listed in the config. GPL-family licenses are restricted.
- `make mirror-licenses` mirrors all third-party licenses into `licenses/` directory.

### Security

- CodeQL analysis in CI (`.github/workflows/codeql-analysis.yml`).
- OpenSSF Scorecard tracking (`.github/workflows/scorecards.yml`).
- Dependabot configured for Go modules and GitHub Actions.

## Testing

- **Unit tests:** `make unit-test` — runs all non-e2e tests. Tests are `*_test.go` files next to source.
- **E2E tests:** `make e2e` — bootstraps kind cluster, builds images, runs test suites. Individual suites: `make e2e-test-schedulingbase`, `make e2e-test-schedulingaction`, `make e2e-test-jobp`, `make e2e-test-jobseq`, `make e2e-test-vcctl`, `make e2e-test-cronjob`, `make e2e-test-dra`, `make e2e-test-hypernode`, `make e2e-test-admission-webhook`, `make e2e-test-shardingcontroller`, `make e2e-test-gangevict`, etc.
- **Ginkgo/Gomega** is used for E2E test framework.

## CI/CD

GitHub Actions in `.github/workflows/` (25 workflows):

- Code verification, CodeQL, license linting
- 14 E2E test workflows
- Docker image build + multi-arch push
- Release pipeline
- Dependabot for dependency updates

## Common Tasks

### Adding a new API type

1. Define types in `staging/src/volcano.sh/apis/pkg/apis/<group>/<version>/`
2. Register with scheme in `staging/src/volcano.sh/apis/pkg/apis/<group>/<version>/register.go`
3. Run `make generate-code && make manifests`
4. Add controller logic in `pkg/controllers/`
5. Add webhook validation in `cmd/webhook-manager/`

### Adding a new scheduler plugin

1. Create plugin in `pkg/scheduler/plugins/<name>/`
2. Implement `framework.Plugin` interface
3. Register in `pkg/scheduler/plugins/` imports

### Adding a new scheduler action

1. Create action in `pkg/scheduler/actions/<name>/`
2. Implement `framework.Action` interface
3. Register in `pkg/scheduler/actions/` imports
