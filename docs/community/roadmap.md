# Volcano Roadmap

Volcano is Kubernetes native batch system to support different domain frameworks, e.g. kubeflow, Spark, Flink; and several enhancements are required to achieve its target. As it's hard to complete those features in one or two release, this document gives an overall roadmap of Volcano in coming release. If any comments/suggestion, please feel free to open an issue.

## v0.4 (Q1 2020)

The major target of this release is to support heterogeneous hardware, e.g. GPU.

### GPU Topology ([#623](https://github.com/volcano-sh/volcano/issues/623))

Lots of batch workloads are using GPU to improve the performance. Because of `nvLink`, the topology of GPU is one of important factors to the performance. Overall, it's better for scheduler to dispatch the Pod the node which have available `nvLink`s, and kubelet dispatch the Pod to the target GPUs.

### GPU Share ([#624](https://github.com/volcano-sh/volcano/issues/624))

A better performance has its cost, including GPU; and there are several scenarios that a Pod can not consume one GPU, e.g. inference workload, dev environment. One of solutions is to support GPU share, including related enhancement to both scheduler and kubelet.

### Virtual Kubelet ([#524](https://github.com/volcano-sh/volcano/issues/524))

Virtual kubelet is also one of solution to support heterogeneous hardware; for example, build a GPU cluster with traditional batch system and dispatch the workload through virtual kubelet. There maybe several tasks in one job, the scheduler should be enhanced to dispatch them to the same virtual kubelet to avoid network impact.

## v1.0 (Q2 2020)

The major target of this release to make Volcano more stable for product.

### Improve test coverage

For now (v0.3), there are 70 e2e test cases for Volcano, including cli e2e; but that's not enough for product environment. In v1.0, more e2e test cases will be added to improve Volcano stability for product.

### Preemption/Reclaim Enhancement

Preemption and Reclaim are two import features for resource sharing; there're two actions for now, but unstable. In v1.0, those two features are going to be enhanced for elastic workload, e.g. stream job, bigdata batch job.

## v1.\*

After v1.0,  the `RFVE` (Request For Volcano Enhancement) will drive the roadmap. The Release/PM team will help  to identify the RFVEs that are going to be released. The candidate `RFVE`s are listed as follow:

* Hierarchical Queue
* Reservation
* Backfill
* Execution Time Estimation
* Job dependency
* Plugins per Queue
* Data Management
* Data Aware Scheduling
* ......
