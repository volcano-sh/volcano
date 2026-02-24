This document helps you get started developing code for Volcano.
If you follow this guide and find some problem, please take
a few minutes to update this file.

Volcano components only have few external dependencies you
need to set up before being able to build and run the code.

- [Quick Start with Tilt (Recommended)](#quick-start-with-tilt-recommended)
- [Setting up Go](#setting-up-go)
- [Setting up Docker](#setting-up-docker)
- [Setting up Kubernetes](#setting-up-kubernetes)
- [Setting up a personal access token](#setting-up-a-personal-access-token)
- [What's next?](#whats-next)


## Quick Start with Tilt (Recommended)

The fastest way to get a full Volcano development environment running is with the
Tilt-based workflow. It provisions a Kind cluster with a local registry and deploys
all Volcano components with live-reload — a single command gets you from zero to a
running cluster:

```bash
make dev-up
```

This installs Kind, Tilt, and ctlptl into `_output/bin` (no system-wide installs),
creates a 5-node Kind cluster with a local Docker registry, deploys Volcano via Helm,
and starts Tilt for live-reloading. Edit any Go file and the running component rebuilds
automatically inside the container.

**Prerequisites:** Docker and Make. No local Go toolchain is required for building
and running Volcano — builds happen inside containers. However, installing Go locally
is recommended for running unit tests, using code analysis tools, and working with
the project long-term. See [Setting up Go](#setting-up-go) below.

**Note:** The first run takes approximately 20-25 minutes while Docker builds all
Go images from scratch. Subsequent runs are much faster thanks to layer caching.
See the [Tips and Best Practices](./development.md#tips-and-best-practices) section
for ways to speed up branch-switching workflows.

For the full workflow reference (teardown, logs, tests, status), see the
[Tilt-based development section](./development.md#tilt-based-local-development) in the
development guide. For architecture and design rationale, see the
[design proposal](../design/tilt-based-development.md).


## Setting up Go

All Volcano components are written in the [Go](https://golang.org) programming language.
To build, you'll need a Go development environment. If you haven't set up a Go development
environment, please follow [these instructions](https://golang.org/doc/install)
to install the Go tools.

The required go version can be tracked down from the Dockerfiles, search for "golang:" from the root:

```
grep -rh 'golang:' installer/dockerfile/ --include='Dockerfile*' | grep -oP 'golang:\K[0-9]+\.[0-9]+\.[0-9]+' | tail -1
```

## Setting up Docker

Volcano has a Docker build system for creating and publishing Docker images.
To leverage that you will need:

- **Docker platform:** To download and install Docker follow [these instructions](https://docs.docker.com/install/).

- **Docker Hub ID:** If you do not yet have a Docker ID account you can follow [these steps](https://docs.docker.com/docker-id/) to create one. This ID will be used in a later step when setting up the environment variables. Or You can use the free mirror repository service [ghcr.io](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry) provided by GitHub.

## Setting up Kubernetes

We require Kubernetes version 1.12 or higher with CRD support.

If you aren't sure which Kubernetes platform is right for you, see [Picking the Right Solution](https://kubernetes.io/docs/setup/).

* [Installing Kubernetes with Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/)

* [Installing Kubernetes with kops](https://kubernetes.io/docs/setup/production-environment/tools/kops/)

* [Installing Kubernetes with kind](https://kind.sigs.k8s.io/)

## Setting up a personal access token

This is only necessary for core contributors in order to push changes to the main repos.
You can make pull requests without two-factor authentication
but the additional security is recommended for everyone.

To be part of the Volcano organization, we require two-factor authentication, and
you must setup a personal access token to enable push via HTTPS. Please follow
[these instructions](https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/)
for how to create a token.
Alternatively you can [add your SSH keys](https://help.github.com/articles/adding-a-new-ssh-key-to-your-github-account/).

## What's next?

Once you've set up the prerequisites, continue with [Using the Code Base](./development.md)
for more details about how to build & test Volcano.
