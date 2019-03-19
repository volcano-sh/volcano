# Volcano

[![Build Status](https://travis-ci.com/volcano-sh/volcano.svg?token=sstuqFE81ukmNz9cEEtd&branch=master)](https://travis-ci.com/volcano-sh/volcano) [![slack](https://img.shields.io/badge/Volcano-%23SLACK-red.svg)](https://volcano-sh.slack.com/messages/CGET876H5/) 

Volcano is a Kubernetes-based system for high performance workload, providing mechanisms for applications
which would like to run high performance workload leveraging Kubernetes, e.g. Tensorflow, Spark, MPI.

Volcano builds upon a decade and a half of experience on running high performance workload workloads at scale
using several systems, combined with best-of-breed ideas and practices from the open source community.

## Overall Architecture

![volcano](docs/images/volcano-intro.png)

## Installation

The easiest way to use Volcano is to use the Helm [chart](https://github.com/helm/charts/tree/master/incubator/sparkoperator).


### 1. Volcano Image
Official images now are not available on DockerHub, however you can build them locally with command:
```
make docker
``` 
**NOTE**: You need ensure the images are correctly loaded in your kubernetes cluster, for
example, if you are using [kind cluster](https://github.com/kubernetes-sigs/kind), 
try command ```kind load docker-image <image-name>:<tag> ``` for each of the images.

### 2. Helm charts
First of all, clone repo to your local path
```
# mkdir -p $GOPATH/src/volcano.sh/
# cd $GOPATH/src/volcano.sh/
# git clone https://github.com/volcano-sh/volcano.git
```
Volcano introduces a helm plugin **gen-admission-secret** to generate valid cert (stored in k8s secret) for
admission service, therefore you are required to install & execute the plugin before installing the chart.
```
#1. Install helm plugin
helm plugin install installer/chart/volcano/plugins/gen-admission-secret

#2. Generate secret within service name
helm gen-admission-secret --service <specified-name>-admission-service --namespace <namespace>

#3. Install helm chart
helm install installer/chart/volcano --namespace <namespace> --name <specified-name>
```
The **<specified-name>** used in the two commands above should be identical.





## Community, discussion, contribution, and support

You can reach the maintainers of this project at:

Slack: [#volcano-sh](http://t.cn/Efa7LKx)

Mailing List: https://groups.google.com/forum/#!forum/volcano-sh
