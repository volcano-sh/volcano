## v0.3

### Issues:

  * #499 Added Statement for eviction in batch. (@k82cn)
  * #491 Update nodeInfo once node was cordoned (@jiaxuanzhou)
  * #490 Add version options for kube-batch (@jiaxuanzhou)
  * #489 Added Statement. (@k82cn)
  * #480 Added Pipelined Pods as valid tasks. (@k82cn)
  * #468 Detailed 'unschedulable' events (@adam-marek)
  * #470 Fixed event recorder schema to include definitions for non kube-batch entities (@adam-marek)
  * #466 Ordered Job by creation time. (@k82cn)
  * #465 Enable PDB based gang scheduling with discrete queues. (@adam-marek)
  * #464 Order gang scheduled jobs based on time of arrival (@adam-marek)
  * #455 Check node spec for unschedulable as well (@jeefy)
  * #443 Checked pod group unschedulable event. (@k82cn)
  * #442 Checked allowed pod number on node. (@k82cn)
  * #440 Updated node info if taints and lables changed. (@k82cn)
  * #438 Using jobSpec for queue e2e. (@k82cn)
  * #433 Added backfill action for BestEffort pods. (@k82cn)
  * #418 Supported Pod Affinity/Anti-Affinity Predicate. (@k82cn)
  * #417 fix BestEffort for overused (@chenyangxueHDU)
  * #414 Added --schedule-period (@k82cn)

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

