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

