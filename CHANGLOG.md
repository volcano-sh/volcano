## v0.4

### Notes:

  * Gang-scheduling/Coscheduling by PDB is depreciated.

### Issues:

  * [#534](https://github.com/kubernetes-sigs/kube-batch/pull/534) Removed old design doc to avoid confusion. ([@k82cn](https://github.com/k82cn))
  * [#536](https://github.com/kubernetes-sigs/kube-batch/pull/536) Migrate from Godep to dep ([@Jeffwan](https://github.com/Jeffwan))
  * [#533](https://github.com/kubernetes-sigs/kube-batch/pull/533) The design doc of PodGroup Phase in Status. ([@k82cn](https://github.com/k82cn))
  * [#540](https://github.com/kubernetes-sigs/kube-batch/pull/540) Examine all pending tasks in a job ([@Jeffwan](https://github.com/Jeffwan))
  * [#544](https://github.com/kubernetes-sigs/kube-batch/pull/544) The design doc of "Dynamic Plugin Configuration" ([@k82cn](https://github.com/k82cn))
  * [#525](https://github.com/kubernetes-sigs/kube-batch/pull/525) Added Phase and Conditions to PodGroup.Status struct ([@Zyqsempai](https://github.com/Zyqsempai))
  * [#547](https://github.com/kubernetes-sigs/kube-batch/pull/547) Fixed status protobuf id. ([@k82cn](https://github.com/k82cn))
  * [#549](https://github.com/kubernetes-sigs/kube-batch/pull/549) Added name when register plugin. ([@k82cn](https://github.com/k82cn))
  * [#550](https://github.com/kubernetes-sigs/kube-batch/pull/550) Added SchedulerConfiguration type. ([@k82cn](https://github.com/k82cn))
  * [#551](https://github.com/kubernetes-sigs/kube-batch/pull/551) Upgrade k8s dependencies to v1.13 ([@Jeffwan](https://github.com/Jeffwan))
  * [#535](https://github.com/kubernetes-sigs/kube-batch/pull/535) Add Unschedulable PodCondition for pods in pending ([@Jeffwan](https://github.com/Jeffwan))
  * [#552](https://github.com/kubernetes-sigs/kube-batch/pull/552) Multi tiers for plugins. ([@k82cn](https://github.com/k82cn))
  * [#548](https://github.com/kubernetes-sigs/kube-batch/pull/548) Add e2e test for different task resource requests ([@Jeffwan](https://github.com/Jeffwan))
  * [#556](https://github.com/kubernetes-sigs/kube-batch/pull/556) Added VolumeScheduling. ([@k82cn](https://github.com/k82cn))
  * [#558](https://github.com/kubernetes-sigs/kube-batch/pull/558) Reduce verbosity level for recurring logs ([@mateusz-ciesielski](https://github.com/mateusz-ciesielski))
  * [#560](https://github.com/kubernetes-sigs/kube-batch/pull/560) Update PodGroup status. ([@k82cn](https://github.com/k82cn))
  * [#565](https://github.com/kubernetes-sigs/kube-batch/pull/565) Removed reclaim&preempt by default. ([@k82cn](https://github.com/k82cn))

## v0.3

### Issues:

  * [#499](https://github.com/kubernetes-sigs/kube-batch/pull/499) Added Statement for eviction in batch. ([@k82cn](http://github.com/k82cn))
  * [#491](https://github.com/kubernetes-sigs/kube-batch/pull/491) Update nodeInfo once node was cordoned ([@jiaxuanzhou](http://github.com/jiaxuanzhou))
  * [#490](https://github.com/kubernetes-sigs/kube-batch/pull/490) Add version options for kube-batch ([@jiaxuanzhou](http://github.com/jiaxuanzhou))
  * [#489](https://github.com/kubernetes-sigs/kube-batch/pull/489) Added Statement. ([@k82cn](http://github.com/k82cn))
  * [#480](https://github.com/kubernetes-sigs/kube-batch/pull/480) Added Pipelined Pods as valid tasks. ([@k82cn](http://github.com/k82cn))
  * [#468](https://github.com/kubernetes-sigs/kube-batch/pull/468) Detailed 'unschedulable' events ([@adam-marek](http://github.com/adam-marek))
  * [#470](https://github.com/kubernetes-sigs/kube-batch/pull/470) Fixed event recorder schema to include definitions for non kube-batch entities ([@adam-marek](http://github.com/adam-marek))
  * [#466](https://github.com/kubernetes-sigs/kube-batch/pull/466) Ordered Job by creation time. ([@k82cn](http://github.com/k82cn))
  * [#465](https://github.com/kubernetes-sigs/kube-batch/pull/465) Enable PDB based gang scheduling with discrete queues. ([@adam-marek](http://github.com/adam-marek))
  * [#464](https://github.com/kubernetes-sigs/kube-batch/pull/464) Order gang scheduled jobs based on time of arrival ([@adam-marek](http://github.com/adam-marek))
  * [#455](https://github.com/kubernetes-sigs/kube-batch/pull/455) Check node spec for unschedulable as well ([@jeefy](http://github.com/jeefy))
  * [#443](https://github.com/kubernetes-sigs/kube-batch/pull/443) Checked pod group unschedulable event. ([@k82cn](http://github.com/k82cn))
  * [#442](https://github.com/kubernetes-sigs/kube-batch/pull/442) Checked allowed pod number on node. ([@k82cn](http://github.com/k82cn))
  * [#440](https://github.com/kubernetes-sigs/kube-batch/pull/440) Updated node info if taints and lables changed. ([@k82cn](http://github.com/k82cn))
  * [#438](https://github.com/kubernetes-sigs/kube-batch/pull/438) Using jobSpec for queue e2e. ([@k82cn](http://github.com/k82cn))
  * [#433](https://github.com/kubernetes-sigs/kube-batch/pull/433) Added backfill action for BestEffort pods. ([@k82cn](http://github.com/k82cn))
  * [#418](https://github.com/kubernetes-sigs/kube-batch/pull/418) Supported Pod Affinity/Anti-Affinity Predicate. ([@k82cn](http://github.com/k82cn))
  * [#417](https://github.com/kubernetes-sigs/kube-batch/pull/417) fix BestEffort for overused ([@chenyangxueHDU](http://github.com/chenyangxueHDU))
  * [#414](https://github.com/kubernetes-sigs/kube-batch/pull/414) Added --schedule-period ([@k82cn](http://github.com/k82cn))

## v0.2

### Features

| Name                             | Version      | Notes                                                    |
| -------------------------------- | ------------ | -------------------------------------------------------- |
| Gang-scheduling/Coscheduling     | Experimental | [Doc](https://github.com/kubernetes/community/pull/2337) |
| Preemption/Reclaim               | Experimental |                                                          |
| Task Priority within Job         | Experimental |                                                          |
| Queue                            | Experimental |                                                          |
| DRF for Job sharing within Queue | Experimental |                                                          |


### Docker Images

```shell
docker pull kubesigs/kube-batch:v0.2
```

