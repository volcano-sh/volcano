This document helps you get started using the Volcano code base. If you follow this guide and find a problem, please take a few minutes to update this file.

- [Tilt-based local development](#tilt-based-local-development)
- [Building the code](#building-the-code)
- [Building docker images](#building-docker-images)
- [Building a specific docker image](#building-a-specific-docker-image)
- [Building the Volcano manifests](#building-the-volcano-manifests)
- [Cleaning outputs](#cleaning-outputs)
- [Running tests](#running-tests)
- [Auto-formatting source code](#auto-formatting-source-code)
- [Running the verification](#running-the-verification)
- [Adding dependencies](#adding-dependencies)
- [About testing](#about-testing)

## Cloning the Code

You will need to clone the main `volcano` repo to `$GOPATH/src/volcano.sh/volcano` for the below commands to work correctly.

## Building the Code

To build all Volcano components for your host architecture, go to the source root and run:

```bash
make image_bins
```

The binaries will be generated at `.../src/volcano.sh/volcano/_output/bin/linux/amd64/`.

If we just run `make` as below:

```bash
make
```

Then the binaries would be generated at `.../src/volcano.sh/volcano/_output/bin/`.

To build a specific component for your host architecture, go to the source root and run `make <component name>`:

```bash
make vc-scheduler
```

## Building Docker Images

Build the containers in your local Docker cache:

```bash
make images
```

To build cross-platform images:

```bash
make images DOCKER_PLATFORMS="linux/amd64,linux/arm64" BUILDX_OUTPUT_TYPE=registry IMAGE_PREFIX=[yourregistry]
```

## Building a Specific Component

If you want to make a local change and test some component, say `vc-controller-manager`, you could do:

Under `volcano.sh/volcano` repo:

```bash
pwd
```

The path should be:

```bash
.../src/volcano.sh/volcano
```

Set up environment variables HUB and TAG:

```bash
export HUB=docker.io/yourrepo
export TAG=citadel
```

Make some local change to the code, then build `vc-controller-manager`:

```bash
make vc-controller-manager
```

## Building the Volcano Manifests

Use the following command to build the deploy YAML files:

```bash
make generate-yaml
```

## Cleaning Outputs

You can delete any build artifacts with:

```bash
make clean
```

## Running Tests

### Running Unit Tests

You can run all the available unit tests with:

```bash
make unit-test
```

### Running E2E Tests

You can run all the available e2e tests with:

```bash
make vcctl
make images
make e2e
```

If you want to run e2e tests in an existing cluster with Volcano deployed, run the following:

```bash
export VC_BIN=<path-to-vcctl-binary> (e.g., .../src/volcano.sh/volcano/_output/bin/)
KUBECONFIG=${KUBECONFIG} go test ./test/e2e
```

## Auto-Formatting Source Code

You can automatically format the source code to follow our conventions by going to the top of the repo and entering:

```bash
./hack/update-gofmt.sh
```

## Running the Verification

You can run all the verification we require on your local repo by going to the top of the repo and entering:

```bash
make verify
```

## Adding Dependencies

Volcano uses [Go Modules](https://go.dev/blog/migrating-to-go-modules) to manage its dependencies. If you want to add or update a dependency, run:

```bash
go get dependency-name@version
go mod tidy
go mod vendor
```

Note: Go's module system, introduced in Go 1.11, provides an official dependency management solution built into the `go` command. Make sure `GO111MODULE` env is not `off` before using it.

## About Testing

Before sending pull requests, you should at least make sure your changes have passed both unit tests and verification. We only merge pull requests when **all** tests are passing.

- Unit tests should be fully hermetic
  - Only access resources in the test binary.
- All packages and any significant files require unit tests.
- Unit tests are written using the standard Go testing package.
- The preferred method of testing multiple scenarios or input is [table-driven testing](https://go.dev/wiki/TableDrivenTests).
- Concurrent unit test runs must pass.

## Tilt-Based Local Development

For iterative development with live-reloading, Volcano provides a Tilt-based workflow
that eliminates the manual build-push-deploy cycle. One command sets up everything:

```bash
make dev-up
```

This installs Kind, Tilt, and ctlptl into `_output/bin`, creates a local Kind cluster
with a Docker registry, deploys Volcano via Helm, and starts Tilt for live-reloading.
No local Go toolchain is required — all builds happen inside containers.

After `dev-up` completes, edit any Go source file under `pkg/`, `cmd/`, or
`third_party/`. Tilt syncs the changes into the running container and the entrypoint
script rebuilds the binary and restarts the process automatically.


### Tips and Best Practices

**First-run startup time.** The initial `make dev-up` takes roughly 20-25 minutes
because Docker builds the Go compiler image layers and compiles all Volcano binaries
from scratch. The next builds will be somewhat faster due to the cached layers.

**Avoid unnecessary image rebuilds across branches.** Tilt rebuilds container images
whenever the tracked source files differs from the last build **at startup**. If you switch branches
frequently, you can avoid long rebuilds by keeping the cluster and Tilt running on
your base branch (e.g. `master`) and only switching branches _after_ the initial
build completes:

1. Start from a clean state on your base branch:
   ```bash
   git checkout master
   make dev-up          # builds images and caches layers
   ```
2. Switch to your feature branch while Tilt is running:
   ```bash
   git checkout my-feature-branch
   ```
3. Tilt detects the changed files and performs an incremental live-update (syncing
   only the changed sources into the running container) instead of a full image
   rebuild. This brings branch-switch turnaround down to seconds.
4. After you finished the work save it and bring down the tilt environment.
   ```bash
   git commit -m "Let's save this work"
   make dev-down
   ```
5. **BEFORE** you continue the work go back to master.
   If you stay on the `my-feature-branch` tilt will do a full ~20-25 minute rebuild.
   ```bash
   git checkout master
   make dev-up
   ```
6. Go back to feature branch if tilt is running. (no rebuild)
   ```bash
   git checkout my-feature-branch
   ```

**Keep `dev-down` vs `dev-clean` straight.** Use `make dev-down` to stop Tilt while
keeping the Kind cluster alive — this lets you inspect the cluster with `kubectl`
and restart Tilt later without re-creating the cluster. Use `make dev-clean` only
when you want a full teardown.

### Viewing Status and Logs

```bash
# Open the Tilt web UI
# (automatically available at http://localhost:10350 after dev-up)

# Check resource status from the terminal
make dev-tilt-status

# View logs for a specific component
make dev-tilt-logs RESOURCE=volcano-scheduler

# Describe a resource in detail
make dev-tilt-describe RESOURCE=volcano-scheduler

# Wait until all resources are ready
make dev-wait-ready
```

### Running Tests via Tilt

Test resources are registered in Tilt with manual triggers so they do not run
automatically. Test dependencies (`install-ginkgo`, `install-kwok`) must be
triggered once per Tilt session — after the first trigger they stay Ready for all
subsequent test runs.

```bash
# First time only: install test dependencies (once per Tilt session)
make dev-tilt-trigger RESOURCE=install-ginkgo
# install-kwok is additionally needed for e2e-tests-all

# Run unit tests (no dependencies needed)
make dev-tilt-trigger RESOURCE=unit-tests

# Run a specific e2e suite
make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase

# If the test stays in Pending state, its dependency (install-ginkgo) has
# not been triggered yet — trigger it and the test will unblock automatically

# Run a focused single test
make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase-focused-example

# Check test output
make dev-tilt-logs RESOURCE=e2e-tests-schedulingbase
```

### Teardown

```bash
# Stop Tilt but keep the Kind cluster running (for manual kubectl inspection)
make dev-down

# Full cleanup: stop Tilt, delete cluster and registry, remove tool binaries
make dev-clean
```

### All Development Targets

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

For design rationale and architecture details, see the
[Tilt-based development design proposal](../design/tilt-based-development.md).

