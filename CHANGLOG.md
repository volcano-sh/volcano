## v0.4.2

### Notes:
 * Added Balanced Resource Priority
 * Support Plugin Arguments
 * Support Weight for Node order plugin

### Issues:
		
 * [#636](https://github.com/kubernetes-sigs/kube-batch/pull/636)	add balanced resource priority	([@DeliangFan](https://github.com/DeliangFan))
 * [#638](https://github.com/kubernetes-sigs/kube-batch/pull/638)	Take init containers into account when getting pod resource request	([@hex108](https://github.com/hex108))
 * [#639](https://github.com/kubernetes-sigs/kube-batch/pull/639)	Support Plugin Arguments	([@thandayuthapani](https://github.com/thandayuthapani))
 * [#640](https://github.com/kubernetes-sigs/kube-batch/pull/640)	Support Weight for NodeOrder Plugin	([@thandayuthapani](https://github.com/thandayuthapani))
 * [#642](https://github.com/kubernetes-sigs/kube-batch/pull/642)	Add event when task is scheduled	([@Rajadeepan](https://github.com/Rajadeepan))
 * [#643](https://github.com/kubernetes-sigs/kube-batch/pull/643)	Return err in Allocate if any error occurs	([@hex108](https://github.com/hex108))
 * [#644](https://github.com/kubernetes-sigs/kube-batch/pull/644)	Fix Typos	([@TommyLike](https://github.com/TommyLike))
 * [#645](https://github.com/kubernetes-sigs/kube-batch/pull/645)	Order task by CreationTimestamp first, then by UID	([@hex108](https://github.com/hex108))
 * [#647](https://github.com/kubernetes-sigs/kube-batch/pull/647)	In allocate, skip adding Job if its queue is not found	([@hex108](https://github.com/hex108))
 * [#649](https://github.com/kubernetes-sigs/kube-batch/pull/649)	Preempt lowest priority task first	([@hex108](https://github.com/hex108))
 * [#651](https://github.com/kubernetes-sigs/kube-batch/pull/651)	Return err in functions of session.go if any error occurs	([@hex108](https://github.com/hex108))
 * [#652](https://github.com/kubernetes-sigs/kube-batch/pull/652)	Change run option SchedulePeriod's type to make it clear	([@hex108](https://github.com/hex108))
 * [#655](https://github.com/kubernetes-sigs/kube-batch/pull/655)	Do graceful eviction using default policy	([@hex108](https://github.com/hex108))
 * [#658](https://github.com/kubernetes-sigs/kube-batch/pull/658)	Address helm install error in tutorial.md	([@hex108](https://github.com/hex108))
 * [#660](https://github.com/kubernetes-sigs/kube-batch/pull/660)	Fix sub exception in reclaim and preempt	([@TommyLike](https://github.com/TommyLike))
 * [#666](https://github.com/kubernetes-sigs/kube-batch/pull/666)	Fix wrong caculation for deserved in proportion plugin	([@zionwu](https://github.com/zionwu))
 * [#671](https://github.com/kubernetes-sigs/kube-batch/pull/671)	Change base image to alphine to reduce image size	([@hex108](https://github.com/hex108))
 * [#673](https://github.com/kubernetes-sigs/kube-batch/pull/673)	Do not create PodGroup and Job for task whose scheduler is not kube-batch	([@hex108](https://github.com/hex108))


## v0.4.1

### Notes:

  * Added NodeOrder Plugin
  * Added Conformance Plugin
  * Removed namespaceAsQueue feature
  * Supported Pod without PodGroup
  * Added performance metrics for scheduling
  
### Issues:

  * [#632](https://github.com/kubernetes-sigs/kube-batch/pull/632) Set schedConf to defaultSchedulerConf if failing to readSchedulerConf ([@hex108](https://github.com/hex108))
  * [#631](https://github.com/kubernetes-sigs/kube-batch/pull/631) Add helm hook crd-install to fix helm install error ([@hex108](https://github.com/hex108))
  * [#584](https://github.com/kubernetes-sigs/kube-batch/pull/584) Replaced FIFO by workqueue. ([@k82cn](https://github.com/k82cn))
  * [#585](https://github.com/kubernetes-sigs/kube-batch/pull/585) Removed invalid error log. ([@k82cn](https://github.com/k82cn))
  * [#594](https://github.com/kubernetes-sigs/kube-batch/pull/594) Fixed Job.Clone issue. ([@k82cn](https://github.com/k82cn))
  * [#600](https://github.com/kubernetes-sigs/kube-batch/pull/600) Fixed duplicated queue in preempt action. ([@k82cn](https://github.com/k82cn))
  * [#596](https://github.com/kubernetes-sigs/kube-batch/pull/596) Set default PodGroup for Pods. ([@k82cn](https://github.com/k82cn))
  * [#587](https://github.com/kubernetes-sigs/kube-batch/pull/587) Adding node priority ([@thandayuthapani](https://github.com/thandayuthapani))
  * [#607](https://github.com/kubernetes-sigs/kube-batch/pull/607) Fixed flaky test. ([@k82cn](https://github.com/k82cn))
  * [#610](https://github.com/kubernetes-sigs/kube-batch/pull/610) Updated log level. ([@k82cn](https://github.com/k82cn))
  * [#609](https://github.com/kubernetes-sigs/kube-batch/pull/609) Added conformance plugin. ([@k82cn](https://github.com/k82cn))
  * [#604](https://github.com/kubernetes-sigs/kube-batch/pull/604) Moved default implementation from pkg. ([@k82cn](https://github.com/k82cn))
  * [#613](https://github.com/kubernetes-sigs/kube-batch/pull/613) Removed namespaceAsQueue. ([@k82cn](https://github.com/k82cn))
  * [#615](https://github.com/kubernetes-sigs/kube-batch/pull/615) Handled minAvailable task everytime. ([@k82cn](https://github.com/k82cn))
  * [#603](https://github.com/kubernetes-sigs/kube-batch/pull/603) Added PriorityClass to PodGroup. ([@k82cn](https://github.com/k82cn))
  * [#618](https://github.com/kubernetes-sigs/kube-batch/pull/618) Added JobPriority e2e. ([@k82cn](https://github.com/k82cn))
  * [#622](https://github.com/kubernetes-sigs/kube-batch/pull/622) Update doc & deployment. ([@k82cn](https://github.com/k82cn))
  * [#626](https://github.com/kubernetes-sigs/kube-batch/pull/626) Fix helm deployment ([@thandayuthapani](https://github.com/thandayuthapani))
  * [#625](https://github.com/kubernetes-sigs/kube-batch/pull/625) Rename PrioritizeFn to NodeOrderFn ([@thandayuthapani](https://github.com/thandayuthapani))
  * [#592](https://github.com/kubernetes-sigs/kube-batch/pull/592) Add performance metrics for scheduling ([@Jeffwan](https://github.com/Jeffwan))

## v0.4

### Notes:

  * Gang-scheduling/Coscheduling by PDB is depreciated.
  * Scheduler configuration format and related start up parameter is updated, refer to [example](https://github.com/kubernetes-sigs/kube-batch/blob/release-0.4/example/kube-batch-conf.yaml) for detail.

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

