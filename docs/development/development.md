This document helps you get started using the Volcano code base.
If you follow this guide and find some problem, please take
a few minutes to update this file.

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


## Cloning the code

You will need to clone the main `volcano` repo to `$GOPATH/src/volcano.sh/volcano` for
the below commands to work correctly.

## Building the code

To build volcano all components for your host architecture, go to
the source root and run:

```bash
make image_bins
```
the binaries will be generated at .../src/volcano.sh/volcano/_output/bin/linux/amd64/
but if we just make as below

```bash
make
```
then the binaries would be generated at .../src/volcano.sh/volcano/_output/bin/

To build a specific component for your host architecture, go to
the source root and run `make <component name>`:

```bash
make vc-scheduler
```


## Building docker images

Build the containers in your local docker cache:

```bash
make images
```

## Building a specific docker image

If you want to make a local change and test some component, say `vc-controller-manager`, you
could do:

Under volcano.sh/volcano repo

```bash
pwd
```
The path should be

```bash
.../src/volcano.sh/volcano
```

Set up environment variables HUB and TAG by
```bash
export HUB=docker.io/yourrepo
export TAG=citadel
```

Make some local change of the code, then build `vc-controller-manager`

```bash
make image.vc-controller-manager
```

## Building the Volcano manifests

Use the following command to build the deploy yaml files:

```bash
make generate-yaml
```

## Cleaning outputs

You can delete any build artifacts with:

```bash
make clean
```

## Running tests

### Running unit tests

You can run all the available unit tests with:

```bash
make unit-test
```

### Running e2e tests

You can run all the available e2e tests with:

```bash
make vcctl
make images
make e2e
```

If you want to run e2e test in a existing cluster with volcano deployed, run the following:

```bash
export VC_BIN= need to set vcctl binary path (eg:.../src/volcano.sh/volcano/_output/bin/)
KUBECONFIG=${KUBECONFIG} go test ./test/e2e
```

## Auto-formatting source code

You can automatically format the source code to follow our conventions by going to the
top of the repo and entering:

```bash
./hack/update-gofmt.sh
```

## Running the verification

You can run all the verification we require on your local repo by going to the top of the repo and entering:

```bash
make verify
```

## Adding dependencies

Volcano uses [Go Modules](https://blog.golang.org/migrating-to-go-modules) to manage its dependencies.
If you want to add or update a dependency, running:

```bash
go get dependency-name@version
go mod tidy
go mod vendor
```

Note: Go's module system, introduced in Go 1.11, provides an official dependency management solution built into the go command.
      Make sure `GO111MODULE` env is not `off` before using it.

## About testing

Before sending pull requests you should at least make sure your changes have
passed both unit and the verification. We only merge pull requests when
**all** tests are passing.

- Unit tests should be fully hermetic
  - Only access resources in the test binary.
- All packages and any significant files require unit tests.
- Unit tests are written using the standard Go testing package.
- The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
- Concurrent unit test runs must pass.
