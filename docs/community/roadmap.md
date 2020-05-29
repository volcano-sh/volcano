# Volcano Roadmap

## v1.0 (Planned on June 30)

The major target of this release to make Volcano more stable for product.

### Stability and Resilience

Improve test coverage. In v1.0, more test cases will be added to improve Volcano stability for product.

### Preemption/Reclaim Enhancement

Preemption and Reclaim are two import features for resource sharing; there're two actions for now, but unstable. In v1.0, those two features are going to be enhanced for elastic workload, e.g. stream job, bigdata batch job.

### GPU Share ([#624](https://github.com/volcano-sh/volcano/issues/624))

A better performance has its cost, including GPU; and there are several scenarios that a Pod can not consume one GPU, e.g. inference workload, dev environment. One of solutions is to support GPU share, including related enhancement to both scheduler and kubelet.

### Integrate with Apache Flink

Flink is a widely used for Stateful Computations over Data Streams, but flink on kubernetes has some gaps now.

### Integrate with argo to support job dependencies

Investigate to cooperate with argo to support job dependencies.

### Support running MindSpore jobs

[MindSpore](https://www.mindspore.cn/) is a deep learning training and inference framework, support running MindSpore training with volcano job.

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
* GPU Topology ([#623](https://github.com/volcano-sh/volcano/issues/623))
* Virtual Kubelet ([#524](https://github.com/volcano-sh/volcano/issues/524))
* ......
