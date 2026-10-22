This document helps you get started using the Volcano code base. If you follow this guide and find a problem, please take a few minutes to update this file.

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
- Concurrent unit test runs must pass.

### Unit-test standards

These standards apply to all contributors when adding or modifying unit tests,
including AI-assisted changes.

#### Prefer table-driven tests

Use [table-driven tests](https://go.dev/wiki/TableDrivenTests) to group related
scenarios into named test cases and use `t.Run` for subtests.
Keep common setup and assertions in the test body instead of duplicating them
for each scenario. A simple single-case test does not need an artificial table.

#### Exercise the changed function

Use white-box knowledge of the implementation to cover relevant success, error,
and boundary paths. Call the changed function with realistic inputs and check
its results and effects through its normal execution paths.

Tests of isolated fragments or extracted helpers do not replace coverage of the
changed function. Do not split production code solely for testing or inject
intermediate values that bypass the logic under test.

#### Keep setup clean and realistic

Use normal initialization and existing test helpers. Do not arbitrarily mutate
struct fields, fabricate invalid internal states, or add test-only injection
hooks just to reach a branch. Valid struct literals and fakes at dependency
boundaries are fine when the function's own logic still runs. Keep helpers small
and make each test's inputs, action, and expected result easy to follow.

#### Avoid arbitrary repetition

Do not run the same deterministic check a fixed number of times without a
reason. Iterating over test cases or constructing meaningful fixture data is
fine.

Repeated execution is acceptable when outcomes can vary between runs, such as
an intermittent ordering or concurrency failure. Explain why repetition is
needed and why the bound is appropriate. Prefer controlled ordering or
synchronization to make the test reliable, and assert the required behavior on
every iteration. Never retry until the test passes or ignore failed iterations.

#### Avoid magic values

Make the meaning of test inputs and expected values clear. Reuse existing
constants and fixtures where they represent the same concept, and give shared
values meaningful names. Keep straightforward case-specific literals close to
their cases; do not introduce unrelated hard-coded values or turn every literal
into a constant.

#### Extend existing tests and follow their naming conventions

Check the existing test file and case table before adding a test. Add a case to
the existing test when it covers the same function or behavior. Create a new
test function or `_test.go` file only when the scenario does not fit the existing
organization.

Follow the package's naming convention, normally `Test<FunctionOrMethod>`, and
describe individual scenarios in subtest names. Avoid names tied to a one-off
target, patch, issue number, or coverage goal.
