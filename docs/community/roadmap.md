# Volcano Roadmap

## v1.0(Released on July 8, 2020)

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

## v1.2(Released on Feb 27, 2021)
### Queue Resource Reservation(Delay)
* Description: Support reserve specified resource for queue without restart Volcano.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1101
* Owner: @hudson741@Thor-wl

### Fair Scheduling For Jobs Of Same Priority And Different Queue
* Description: Schedule jobs of same priority but from different queue accord to create time.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1098
* Owner: @alcorj-mizar

### Differentiated Scheduling Strategies For Different Queue
* Description: Support configure actions and plugins for different queues.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1035
* Owner: @sresthas

### Support Hierarchy Queue(Delay)
* Description: Support Hierarchy Queue algorithm.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1033
* Owner: @My-pleasure

### Job PriorityClassName Update
* Description: Support update vcjob priorityClassName update when job has not been scheduled.
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1097
* Owner: @merryzhou

### Status Message Enhanced For CRD(Delay)
* Description: Provide more status detail for CRD status when use CLI such job fail reason.
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1094
* Owner:@mikechengwei

### Support MinAvailable For Task
* Description: Support MinAvailable for task
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/988
* Owner: @shinytang6

## v1.3(Released on May 27, 2021)
### Task-Topology
* Description: Support task topology scheduling
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1349

### Support multiple scheduler
* Description: Support multiple scheduler by admission controller.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1322
* Owner: @Thor-wl @zen-xu

### Stability and Resilience
* Description: Improve the UT/E2E test coverage and add the stress test to improve stability.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1284
* Owner: @rudeigerc

### Volcano Device Plugin enhancement
* Description: Support container using multiples GPU as well as part of GPU card.
* Priority: High
* Issue: https://github.com/volcano-sh/devices/issues/12
* Owner: @peiniliu

## v1.4(Released on Sep 18, 2021)
### Support NUMA-Awareness scheduling in Volcano
* Description: Support NUMA-Awareness scheduling in Volcano.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1707
* Owner: @huone1 @william-wang

### Support multi-scheduler by admission controller
* Description: Use default scheduler for system daemon and Volcano scheduler for biz workload.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1322
* Owner: @huone1 @william-wang

### Support scheduling with proportion of resources
* Description: Add scheduling policy with proportion of resources.
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1368
* Owner: @king-jingxiang

### Enhance the resource comparison functions for various of scenarios
* Description: Improve the Fundamental comparison functions
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1525
* Owner: @Thor-wl

### System Stability Enhancement
* Description: Add UT/E2E to cover more scenarios and add basic stress test.
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1284
* Owner: @rudeigerc

## v1.5(Released on Feb 20, 2022)
### Support Hierarchy Queue(Delay)
* Description: Support Hierarchy Queue algorithm.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1033
* Owner: @Thor-wl

### Support Volcano scheduler in Spark community(Delay)
* Description: Support Volcano scheduler in Spark community.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1704
* Owner: @william-wang @Yikun

### Monitoring: Cluster Resource(Delay)
* Description: Support monitoring metrics at cluster level
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1586
* Owner: @yanglilangqun @Tammy-kunyu

### Task Dag scheduling
* Description: Support Dag for task level
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1627
* Owner: @hwdef @shinytang6 @Thor-wl

### Support configuration hot update(Delay)
* Description: Add hot update for Volcano components arguments.
* Issue: https://github.com/volcano-sh/volcano/issues/1326

## v1.6(To Be Released around May 15, 2022)
### Support Dynamic Scheduling Based on Realtime Load
* Description: Support dynamic scheduling based on realtime load.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1777
* Owner: @william-wang

### Support Rescheduling Based on Realtime Load
* Description: Support rescheduling based on realtime load.
* Priority: High
* Issue: https://github.com/volcano-sh/volcano/issues/1777
* Owner: @Thor-wl

### Support Elastic Scheduling
* Description: Support elastic scheduling for workloads.
* Priority: High
* Issue: TO BE ADDED
* Owner: @qiankunli @Thor-wl

## Later (To be updated)
### Monitoring: 
* Description: Support monitoring metrics at queue and job level
* Priority: Middle
* Issue: https://github.com/volcano-sh/volcano/issues/1586
* Owner: @yanglilangqun @Tammy-kunyu

### Improve resource calculation accuracy
* Description: Support high accurate resource calculation.
* Issue: https://github.com/volcano-sh/volcano/issues/1196

### Support job backfill
* Description: Add backfill functionality to improve the resource utilization.

### Improve the Autoscaling enficiency
* Description: Combine the Autoscaler and scheduler to improve the scaling efficiency.