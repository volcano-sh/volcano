<a href="https://volcano.sh/">
    <img src="https://raw.githubusercontent.com/volcano-sh/volcano/master/docs/images/volcano-horizontal-color.png"/>
</a>

-------

[![Build Status](https://travis-ci.org/volcano-sh/volcano.svg?branch=master)](https://travis-ci.org/volcano-sh/volcano)
[![Go Report Card](https://goreportcard.com/badge/github.com/volcano-sh/volcano)](https://goreportcard.com/report/github.com/volcano-sh/volcano)
[![RepoSize](https://img.shields.io/github/repo-size/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano)
[![Release](https://img.shields.io/github/release/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/releases)
[![LICENSE](https://img.shields.io/github/license/volcano-sh/volcano.svg)](https://github.com/volcano-sh/volcano/blob/master/LICENSE)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/3012/badge)](https://bestpractices.coreinfrastructure.org/projects/3012)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/volcano-sh/volcano/badge)](https://scorecard.dev/viewer/?uri=github.com/volcano-sh/volcano)
[![Gurubase](https://img.shields.io/badge/Gurubase-Ask%20Volcano%20Guru-006BFF)](https://gurubase.io/g/volcano)


[Volcano](https://volcano.sh/) is a batch system built on Kubernetes. It provides a suite of mechanisms that are commonly required by
many classes of batch & elastic workload including: machine learning/deep learning, bioinformatics/genomics and
other "big data" applications. These types of applications typically run on generalized domain frameworks like
TensorFlow, Spark, Ray, PyTorch, MPI, etc, which Volcano integrates with.

Volcano builds upon a decade and a half of experience running a wide
variety of high performance workloads at scale using several systems
and platforms, combined with best-of-breed ideas and practices from
the open source community.

Until June 2021, Volcano has been widely used around the world at a variety of industries such as Internet/Cloud/Finance/
Manufacturing/Medical. More than 20 companies or institutions are not only end users but also active contributors. Hundreds
of contributors are taking active part in the code commit/PR review/issue discussion/docs update and design provision. We
are looking forward to your participation.

**NOTE**: the scheduler is built based on [kube-batch](https://github.com/kubernetes-sigs/kube-batch);
refer to [#241](https://github.com/volcano-sh/volcano/issues/241) and [#288](https://github.com/volcano-sh/volcano/pull/288) for more detail.

![cncf_logo](docs/images/cncf-logo.png)

Volcano is an incubating project of the [Cloud Native Computing Foundation](https://cncf.io/) (CNCF). Please consider joining the CNCF if you are an organization that wants to take an active role in supporting the growth and evolution of the cloud native ecosystem. 

## Overall Architecture

![volcano](docs/images/volcano-architecture.png)

## Talks

- [Intro: Kubernetes Batch Scheduling @ KubeCon 2019 EU](https://sched.co/MPi7)
- [Volcano 在 Kubernetes 中运行高性能作业实践 @ ArchSummit 2019](https://archsummit.infoq.cn/2019/shenzhen/presentation/1817)
- [Volcano：基于云原生的高密计算解决方案 @ Huawei Connection 2019](https://e.huawei.com/cn/material/event/HC/09099dce0070415e9f26ada51b2216d7)
- [Improving Performance of Deep Learning Workloads With Volcano @ KubeCon 2019 NA](https://sched.co/UaZi)
- [Batch Capability of Kubernetes Intro @ KubeCon 2019 NA](https://sched.co/Uajv)
- [Optimizing Knowledge Distillation Training With Volcano @ KubeCon 2021 EU](https://www.youtube.com/watch?v=cDPGmhVcj7Y&t=143s)
- [Exploration About Mixing Technology of Online Services and Offline Jobs Based On Volcano @ KubeCon 2021 China](https://www.youtube.com/watch?v=daqkUlT5ReY)
- [Volcano - Cloud Native Batch System for AI, BigData and HPC @ KubeCon 2022 EU](https://www.youtube.com/watch?v=wjy35HfIP_k)
- [How to Leverage Volcano to Improve the Resource Utilization of AI Pharmaceuticals, Autonomous Driving, and Smart Buildings @ KubeCon 2023 EU](https://www.youtube.com/watch?v=ujHDV5xteqU)
- [Run Your AI Workloads and Microservices on Kubernetes More Easily and Efficiently @ KubeCon 2023 China](https://www.youtube.com/watch?v=OO7zpyf7fgs)
- [Optimize LLM Workflows with Smart Infrastructure Enhanced by Volcano @ KubeCon 2024 China](https://www.youtube.com/watch?v=77Qn1-I-muQ)
- [How Volcano Enable Next Wave of Intelligent Applications @ KubeCon 2024 China](https://www.youtube.com/watch?v=IzR7zJQ8vMw)
- [Leverage Topology Modeling and Topology-Aware Scheduling to Accelerate LLM Training @ KubeCon 2024 China](https://www.youtube.com/watch?v=IB54LHQQ8lI)


## Ecosystem

- [spark-operator](https://www.kubeflow.org/docs/components/spark-operator/user-guide/volcano-integration/)
- [kubeflow/training-operator](https://www.kubeflow.org/docs/components/training/user-guides/job-scheduling/)
- [kubeflow/arena](https://github.com/kubeflow/arena/blob/master/docs/training/volcanojob/volcanojob.md)
- [Horovod/MPI](https://github.com/volcano-sh/volcano/tree/master/example/integrations/mpi)
- [paddlepaddle](https://github.com/volcano-sh/volcano/tree/master/example/integrations/paddlepaddle)
- [cromwell](https://github.com/broadinstitute/cromwell/blob/develop/docs/backends/Volcano.md)
- [KubeRay](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/volcano.html)

## Quick Start Guide

### Prerequisites

- Kubernetes 1.12+ with CRD support


You can try Volcano by one of the following two ways.

Note: 
* For Kubernetes v1.17+ use CRDs under config/crd/bases (recommended)
* For Kubernetes versions < v1.16 use CRDs under config/crd/v1beta1 (deprecated)

### Install with YAML files

Install Volcano on an existing Kubernetes cluster. This way is both available for x86_64 and arm64 architecture.

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

### Install via helm

To install official release, please visit [helm-charts](https://github.com/volcano-sh/helm-charts) for details.

```bash
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```

Install from source code for developers:

```bash
helm install volcano installer/helm/chart/volcano --namespace volcano-system --create-namespace

# list helm release
helm list -n volcano-system
```

### Install from code

If you don't have a kubernetes cluster, try one-click install from code base:

```bash
./hack/local-up-volcano.sh
```

This way is only available for x86_64 temporarily.

### Install monitoring system

If you want to get prometheus and grafana volcano dashboard after volcano installed, try following commands:

```bash
make TAG=latest generate-yaml
kubectl create -f _output/release/volcano-monitoring-latest.yaml
```

### Install dashboard

Please follow the guide [Volcano Dashboard](https://github.com/volcano-sh/dashboard#volcano-dashboard) to install volcano dashboard.

## Kubernetes compatibility

|                       | Kubernetes 1.17 | Kubernetes 1.18 | Kubernetes 1.19 | Kubernetes 1.20 | Kubernetes 1.21 | Kubernetes 1.22 | Kubernetes 1.23 | Kubernetes 1.24 | Kubernetes 1.25 | Kubernetes 1.26 | Kubernetes 1.27 | Kubernetes 1.28 | Kubernetes 1.29 |Kubernetes 1.30 |Kubernetes 1.31 |
|-----------------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|---------------|---------------|
| Volcano v1.6          | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               | -               | -               |-              |-              |
| Volcano v1.7          | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               |_              |_              |
| Volcano v1.8          | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               |-              |_              |
| Volcano v1.9          | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               |-              |_              |
| Volcano v1.10         | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               |✓              |_              |
| Volcano HEAD (master) | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               |✓              |✓              |

Key:
* `✓` Volcano and the Kubernetes version are exactly compatible.
* `+` Volcano has features or API objects that may not be present in the Kubernetes version.
* `-` The Kubernetes version has features or API objects that Volcano can't use.


## Meeting

Community weekly meeting for Asia: 15:00 - 16:00 (UTC+8) Friday. ([Convert to your timezone.](https://www.thetimezoneconverter.com/?t=10%3A00&tz=GMT%2B8&))

Community biweekly meeting for America: 08:30 - 09:30 (UTC-8) Thursday. ([Convert to your timezone.](https://www.thetimezoneconverter.com/?t=10%3A00&tz=GMT%2B8&))

Community meeting for Europe is ongoing on demand now. If you have some ideas or topics to discuss, please leave message
in the [slack](https://cloud-native.slack.com/archives/C011GJDQS0N). Maintainers will contact with you and book an open meeting for that.

Resources:
- [Meeting notes and agenda](https://docs.google.com/document/d/1YLbF8zjZBiR9PbXQPB22iuc_L0Oui5A1lddVfRnZrqs/edit)
- [Meeting link](https://zoom.us/j/91804791393)
- [Meeting Calendar](https://calendar.google.com/calendar/b/1/embed?src=volcano.sh.bot@gmail.com) | [Subscribe](https://calendar.google.com/calendar/b/1?cid=dm9sY2Fuby5zaC5ib3RAZ21haWwuY29t)

## Contact

If you have any question, feel free to reach out to us in the following ways:

[Volcano Slack Channel](https://cloud-native.slack.com/archives/C011GJDQS0N) | [Join](https://slack.cncf.io/)

[Mailing List](https://groups.google.com/forum/#!forum/volcano-sh)

Wechat: Add WeChat account `k8s2222` (华为云小助手2号) to let her pull you into the group.
