# kube-arbitrator

[![Build Status](https://travis-ci.org/kubernetes-incubator/kube-arbitrator.svg?branch=master)](https://travis-ci.org/kubernetes-incubator/kube-arbitrator)
[![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes-incubator/kube-arbitrator)](https://goreportcard.com/report/github.com/kubernetes-incubator/kube-arbitrator)

`kube-arbitrator` is batch system built on Kubernetes, providing mechanisms for the applications which would like to run batch jobs in Kubernetes.

`kube-arbitrator` builds upon a decade and a half of experience on running batch workloads at scale using several systems, e.g. [LSF](https://www.ibm.com/us-en/marketplace/hpc-workload-management), [Symphony](https://www.ibm.com/us-en/marketplace/analytics-workload-management), combined with best-of-breed ideas and practices from the community.

Refer to [tutorial](doc/usage/tutorial.md) on how to use `kube-arbitrator` to run batch job in Kubernetes

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- Slack: #kubernetes-dev
- Mailing List: https://groups.google.com/forum/#!forum/kubernetes-dev

## Kubernetes Incubator

This is a [Kubernetes Incubator project](https://github.com/kubernetes/community/blob/master/incubator.md). The project was established 2017-07-01. The incubator team for the project is:

- Sponsor: Joe Beda ([@jbeda](https://github.com/jbeda))
- Champion: Timothy St. Clair ([@timothysc](https://github.com/timothysc))
- SIG: sig-scheduling, sig-bigdata

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
