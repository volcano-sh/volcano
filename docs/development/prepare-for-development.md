This document helps you get started developing code for Volcano.
If you follow this guide and find some problem, please take
a few minutes to update this file.

Volcano components only have few external dependencies you
need to set up before being able to build and run the code.

- [Setting up Go](#setting-up-go)
- [Setting up Docker](#setting-up-docker)
- [Other dependencies](#other-dependencies)
- [Setting up Kubernetes](#setting-up-kubernetes)
- [Setting up personal access token](#setting-up-a-personal-access-token)

## Setting up Go

All Volcano components are written in the [Go](https://golang.org) programming language.
To build, you'll need a Go development environment. If you haven't set up a Go development
environment, please follow [these instructions](https://golang.org/doc/install)
to install the Go tools.

Volcano currently builds with Go 1.14

## Setting up Docker

Istio has a Docker build system for creating and publishing Docker images.
To leverage that you will need:

- **Docker platform:** To download and install Docker follow [these instructions](https://docs.docker.com/install/).

- **Docker Hub ID:** If you do not yet have a Docker ID account you can follow [these steps](https://docs.docker.com/docker-id/) to create one. This ID will be used in a later step when setting up the environment variables.


## Setting up Kubernetes

We require Kubernetes version 1.12 or higher with CRD support.

If you aren't sure which Kubernetes platform is right for you, see [Picking the Right Solution](https://kubernetes.io/docs/setup/).

* [Installing Kubernetes with Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/)

* [Installing Kubernetes with kops](https://kubernetes.io/docs/setup/production-environment/tools/kops/)

* [Installing Kubernetes with kind](https://kind.sigs.k8s.io/)

### Setting up a personal access token

This is only necessary for core contributors in order to push changes to the main repos.
You can make pull requests without two-factor authentication
but the additional security is recommended for everyone.

To be part of the Volcano organization, we require two-factor authentication, and
you must setup a personal access token to enable push via HTTPS. Please follow
[these instructions](https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/)
for how to create a token.
Alternatively you can [add your SSH keys](https://help.github.com/articles/adding-a-new-ssh-key-to-your-github-account/).

### What's next?

Once you've set up the prerequisites, continue with [Using the Code Base](./development.md)
for more details about how to build & test Volcano.