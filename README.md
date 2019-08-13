![volcano-logo](docs/images/volcano-logo.png)

-------

[![Build Status](https://travis-ci.org/volcano-sh/volcano.svg?branch=master)](https://travis-ci.org/volcano-sh/volcano)
[![Go Report Card](https://goreportcard.com/badge/github.com/volcano-sh/volcano)](https://goreportcard.com/report/github.com/volcano-sh/volcano)
[![RepoSize](https://img.shields.io/github/repo-size/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano)
[![Release](https://img.shields.io/github/release/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/releases)
[![LICENSE](https://img.shields.io/github/license/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/blob/master/LICENSE)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fvolcano-sh%2Fvolcano.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Fvolcano-sh%2Fvolcano?ref=badge_shield)   [![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/3012/badge)](https://bestpractices.coreinfrastructure.org/projects/3012)


Volcano is a batch system built on Kubernetes. It provides a suite of mechanisms currently missing from
Kubernetes that are commonly required by many classes of batch & elastic workload including:

1. machine learning/deep learning,
2. bioinformatics/genomics
3. other "big data" applications.

These types of applications typically run on generalized domain
frameworks like TensorFlow, Spark, PyTorch, MPI, etc, which Volcano integrates with.

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
    1. Support for specialized container runtimes like Singularity,
       with GPU accelerator extensions and enhanced security features.
4. Other
    1. Data locality awareness and intelligent scheduling
    2. Optimizations for data throughput, round-trip latency, etc.

Volcano builds upon a decade and a half of experience running a wide
variety of high performance workloads at scale using several systems
and platforms, combined with best-of-breed ideas and practices from
the open source community.

**NOTE**: the scheduler is built based on [kube-batch](https://github.com/kubernetes-sigs/kube-batch);
refer to [#241](https://github.com/volcano-sh/volcano/issues/241) and [#288](https://github.com/volcano-sh/volcano/pull/288) for more detail.

## Overall Architecture

![volcano](docs/images/volcano-intro.png)

## Talks

You can watch industry experts talking about Volcano in different International Conferences over [here.](https://volcano.sh/talk/)

## Quick Start Guide

### Prerequisites

- Kubernetes 1.12+ with CRD support


You can try volcano by one the following two ways.


### Install with YAML files

Install volcano on a existing Kubernetes cluster.

```
kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/master/installer/volcano-development.yaml
```

Enjoy! Volcano will create the following resources in `volcano-system` namespace.


```
NAME                                       READY   STATUS      RESTARTS   AGE
pod/volcano-admission-5bd5756f79-dnr4l     1/1     Running     0          96s
pod/volcano-admission-init-4hjpx           0/1     Completed   0          96s
pod/volcano-controllers-687948d9c8-nw4b4   1/1     Running     0          96s
pod/volcano-scheduler-94998fc64-4z8kh      1/1     Running     0          96s

NAME                                TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)   AGE
service/volcano-admission-service   ClusterIP   10.98.152.108   <none>        443/TCP   96s

NAME                                  READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/volcano-admission     1/1     1            1           96s
deployment.apps/volcano-controllers   1/1     1            1           96s
deployment.apps/volcano-scheduler     1/1     1            1           96s

NAME                                             DESIRED   CURRENT   READY   AGE
replicaset.apps/volcano-admission-5bd5756f79     1         1         1       96s
replicaset.apps/volcano-controllers-687948d9c8   1         1         1       96s
replicaset.apps/volcano-scheduler-94998fc64      1         1         1       96s

NAME                               COMPLETIONS   DURATION   AGE
job.batch/volcano-admission-init   1/1           48s        96s

```

### Install from code

If you have no kubernetes cluster, try one click install from code base:

```bash
./hack/local-up-volcano.sh
```


## Community, discussion, contribution, and support

You can reach the maintainers of this project at:

Slack Channel : https://volcano-sh.slack.com. (Signup [here](https://join.slack.com/t/volcano-sh/shared_invite/enQtNTU5NTU3NDU0MTc4LTgzZTQ2MzViNTFmNDg1ZGUyMzcwNjgxZGQ1ZDdhOGE3Mzg1Y2NkZjk1MDJlZTZhZWU5MDg2MWJhMzI3Mjg3ZTk))

Mailing List  : https://groups.google.com/forum/#!forum/volcano-sh

