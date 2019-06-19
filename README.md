![volcano-logo](docs/images/volcano-logo.png)

-------

[![Build Status](https://travis-ci.org/volcano-sh/volcano.svg?branch=master)](https://travis-ci.org/volcano-sh/volcano)
[![Go Report Card](https://goreportcard.com/badge/github.com/volcano-sh/volcano)](https://goreportcard.com/report/github.com/volcano-sh/volcano)
[![RepoSize](https://img.shields.io/github/repo-size/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano)
[![Release](https://img.shields.io/github/release/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/releases)
[![LICENSE](https://img.shields.io/github/license/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/blob/master/LICENSE)


Volcano is a batch system built on Kubernetes. It provides a suite of mechanisms currently missing from
Kubernetes that are commonly required by many classes of batch & elastic workload including:

1. machine learning/deep learning,
2. bioinformatics/genomics, and 
3. other "big data" applications.

These types of applications typically run on generalized domain
frameworks like Tensorflow, Spark, PyTorch, MPI, etc, which Volcano integrates with.

Some examples of the mechanisms and features that Volcano adds to Kubernetes are:

1. Job management extensions and improvements, e.g:
    1. Multi-pod jobs
    2. Lifecycle management extensions including suspend/resume and
       restart.
    3. Improved error handling
    4. Indexed jobs
    5. Task dependencies
2. Scheduling extensions, e.g:
    1. Co-scheduling
    2. Fair-share scheduling
    3. Queue scheduling
    4. Preemption and reclaims
    5. Reservations and backfills
    6. Topology-based scheduling
3. Runtime extensions, e.g:
    1. Support for specialized continer runtimes like Singularity,
       with GPU accelerator extensions and enhanced security features.
4. Other
    1. Data locality awareness and intelligent scheduling
    2. Optimizations for data throughput, round-trip latency, etc.

Volcano builds upon a decade and a half of experience running a wide
variety of high performance workloads at scale using several systems
and platforms, combined with best-of-breed ideas and practices from
the open source community.

## Overall Architecture

![volcano](docs/images/volcano-intro.png)

## Quick Start Guide

The easiest way to deploy Volcano is to use the Helm chart.

### Pre-requisites

First of all, clone the repo to your local path:

```
# mkdir -p $GOPATH/src/volcano.sh/
# cd $GOPATH/src/volcano.sh/
# git clone --recursive https://github.com/volcano-sh/volcano.git
```

### 1. Volcano Image

Official images are available on [DockerHub](https://hub.docker.com/u/volcanosh), however you can
build them locally with the command:

```
cd $GOPATH/src/volcano.sh/volcano
make images

## Verify your images
# docker images
REPOSITORY                      TAG                 IMAGE ID            CREATED             SIZE
volcanosh/vk-admission     latest              a83338506638        8 seconds ago       41.4MB
volcanosh/vk-kube-batch    latest              faa3c2a25ac3        9 seconds ago       49.6MB
volcanosh/vk-controllers   latest              7b11606ebfb8        10 seconds ago      44.2MB

``` 

**NOTE**: You need ensure the images are correctly loaded in your kubernetes cluster, for
example, if you are using [kind cluster](https://github.com/kubernetes-sigs/kind), 
try command ```kind load docker-image <image-name>:<tag> ``` for each of the images.

### 2. Helm charts

Secondly, install helm chart.

```
helm install installer/helm/chart/volcano --namespace <namespace> --name <specified-name>

For eg :
helm install installer/helm/chart/volcano --namespace volcano-trial --name volcano-trial

```

To Verify your installation run the following commands:

```
#1. Verify the Running Pods
# kubectl get pods --namespace <namespace> 
NAME                                                READY   STATUS    RESTARTS   AGE
<specified-name>-admission-84fd9b9dd8-9trxn          1/1     Running   0          43s
<specified-name>-controllers-75dcc8ff89-42v6r        1/1     Running   0          43s
<specified-name>-scheduler-b94cdb867-89pm2           1/1     Running   0          43s
<specified-name>--admission-init-qbtmb               0/1     Completed 0          43s

#2. Verify the Services
# kubectl get services --namespace <namespace> 
NAME                                     TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)   AGE
<specified-name>-admission-service       ClusterIP   10.105.78.53   <none>        443/TCP   91s

```


## Community, discussion, contribution, and support

You can reach the maintainers of this project at:

Slack: [#volcano-sh](http://t.cn/Efa7LKx)

Mailing List: https://groups.google.com/forum/#!forum/volcano-sh
