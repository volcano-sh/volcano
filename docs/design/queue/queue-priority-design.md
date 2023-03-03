# Volcano Priority For Queue

@[zbbkeepgoing](https://github.com/zbbkeepgoing); Feb 10, 2023


## Motivation

Currently, Volcano has Job and Task level `PriorityClass` settings, but lacks Queue `PriorityClass` configuration.
Queue's `PriorityClass` can play a role in the following scenarios.

1. Submitters of tasks and jobs can't determine their `PriorityClass`, they only know to submit to one of the queues.
2. The queue Priority capability can reduce the complexity of the job and the task submitter to a certain extent.
   After the queue has priority, the task can properly adjust the priority according to its own task situation.
3. The queue priority capability can help task that in high-priority queue to allocate resources first, which is very
   suitable in the scenario of dividing the business importance on the queue.
4. In the process of queue's reclaiming, `PriorityClass` can be used to help high-priority queue to reclaim the resources of other queues first.

## Consideration
### Impact QueueOrderFn in session_plugin.go
The sorting of the queue is determined by three plugins: Priority (the Queue's PriorityClass we are currently designing),
proportion, and drf. there are three situation in scheduler configuration.
1. Priority plugin in the first. If priority setting of all queues are scatter, every queue has different priority,
   QueueOrderFn basically only uses `PriorityClass` as the sorting rule, DRF and Proportion are difficult to play a role，
   Unless the queues have the same priority. Another point is that regardless of whether the priority setting is scattered or not,
   it is easy to cause low-priority queues to starve to death.
2. Priority plugin in the middle. It is better than the first situation where low priority starves to death，
   But it will also cause a problem, whether is drf or proportion who is at the end ,it will be difficult to play a role.
   In addition, it's difficult for priority plugin to play a role in this case, because the previous plugin(drf or proportion)
   can basically determine the order.
3. Priority plugin in the end. In this case, Priority basically cannot play a role, because the previous plugin(drf and proportion)
   can basically determine the queue order, unless the `Share` of queue  calculated by DRF or Proportion is 0 or 1.

### Better QueueOrderFn -> QueueScoreOrderFn
The above configuration is suitable for whose only consider on one or two kinds of queue sorting requirements, or one of then is
the main sorting strategy, and the other is the secondary sorting strategy.

Here, propose a strategy that takes into account all queue sorting in three plugin, similar to calculating `NodeScore`,
use three plugin to calculate the score of each queue. **Top score has highest-priority**. user can customize weight of three plugins
to meet their preference for a certain priority.

* Queue's Score Of Priority

  `Score = (PriorityClass of queue - Minimum PriorityClass)/(Maximum PriorityClass - Minimum PriorityClass) * Weight of Priority Plugin`

* Queue's Score Of DRF

  `Score = (1 - node.attr.share/node.weight) * Weight of DRF Plugin`

* Queue's Score Of Proportion

  `Score = (1 - queue.share) * Weight of Proportion Plugin`

`Total Score = Priority's Score + DRF's Score + Proportion's Score`

#### QueueScoreOrderFn example：
```
Maximum PriorityClass = 100
Minimum PriorityClass = 0
Priority Weight = 1
DRF Weight = 1
Proportion Weight = 1
```
|        | Priority | DRF  Share | Proportion Share | Priority Score | DRF Score | Proportion Score | Total Score |
|--------|----------|------------|------------------|----------------|-----------|------------------|-------------|
| QueueA | 40       | 0.3        | 1.2              | 0.4            | 0.7       | -0.2             | 0.9         |
| QueueB | 80       | 0.4        | 0.5              | 0.8            | 0.6       | 0.5              | 1.9         |
| QueueC | 0        | 0.5        | 0.3              | 0              | 0.5       | 0.7              | 1.2         |

QueueScoreOrder： **Queue B > Queue C > Queue A**

### Impact priority of job

Due to the Priority of queue, therefore, **if priority not setting in job, the job priority will inherit the priority of queue**.

*Through the idea of QueueScore, here is a reflection：*

At present, all of enqueue、 allocate or reclaim actions are sort the jobs in the queue after the queue is sorted.
If the following situation occur.

There are two queue:

Queue A(priority 10) has A1 Job(default priority 10), A2 Job(priority 12).

Queue B(priority 8) has B1 Job(default priority 8), B2 Job(priority 13).

Current logic of the three actions are as follows:
* A Queue
    * A2 Job
    * A1 Job
* B Queue
    * B2 Job
    * B1 Job

Try to think another way, if we think job is the base unit in scheduler, priority of job maybe more important than priority of queue.

New logic of the three actions are as follows:
* B2 Job
* A2 Job
* A1 Job
* B1 Job

And the QueueOrderFn in drf or proportion will be a factor affecting JobOrder.
Of course, the perspectives of these two methods are different, but there is no problem in essence.

## Design
### API
```
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: q1
spec:
  reclaimable: true
  weight: 1
  priorityClassName: system-cluster-critical
```

The priority configuration of the queue follows the native PriorityClass of Kubernetes.

### Scheduler configuration
```
actions: "enqueue, allocate, backfill"
configurations:
- name: enqueue:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1 # key: QueueScoreOrder.pluginName.weight
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
- name: allocate:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
- name: reclaim:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
tiers:
- plugins:
  - name: priority
  - name: gang
    enablePreemptable: false
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
  - name: binpack
```


## Implementation
**pseudo code**

### QueueScoreOrder
```go
// /volcano/pkg/scheduler/framework/session_plugins.go
// QueueScoreOrderFn invoke queueScore function of the plugins
func (ssn *Session) QueueScoreOrderFn(l, r interface{}, action string) bool {
	args := GetArgOfActionFromConf(ssn.Configurations, action)
	lTotalScore := 0
	rTotalScore := 0
	queueOrderWeight := 0
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledQueueScoreOrder) {
				continue
			}
			qof, found := ssn.queueScoreFns[plugin.Name]
			if !found {
				continue
			}
			lScore, rScore := qof(l, r)
			args.GetInt(&queueOrderWeight, fmt.Sprintf("QueueScoreOrder.%s.weight", plugin.Name))
			lTotalScore += lScore * queueOrderWeight
			rTotalScore += rScore * queueOrderWeight
		}
	}
	if lTotalScore != rTotalScore {
		return lTotalScore > rTotalScore
	}
	// If no queue order funcs, order queue by CreationTimestamp first, then by UID.
	lv := l.(*api.QueueInfo)
	rv := r.(*api.QueueInfo)
	if lv.Queue.CreationTimestamp.Equal(&rv.Queue.CreationTimestamp) {
		return lv.UID < rv.UID
	}
	return lv.Queue.CreationTimestamp.Before(&rv.Queue.CreationTimestamp)
}
```

```go
// /volcano/pkg/scheduler/actions/enqueue/enqueue.go
func (enqueue *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Enqueue ...")
	defer klog.V(3).Infof("Leaving Enqueue ...")
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	QueueScoreOrderEnable := false
	framework.GetArgOfActionFromConf(ssn.Configurations, enqueue.Name()).GetBool(&QueueScoreOrderEnable, "QueueScoreOrderEnable")
	if QueueScoreOrderEnable {
		queues = util.NewPriorityQueue(ssn.QueueScoreOrderFn)
	}
	....
```
```go
// /volcano/pkg/scheduler/plugins/priority/priority.go
	queueScoreFn := func(l, r interface{}) (float32, float32) {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)
		maxPriorityClass = ssn.PriorityClass["max"]
		minPriorityClass = ssn.PriorityClass["min"]
		return (lv.Priority - minPriorityClass) / (maxPriorityClass - minPriorityClass),
			(rv.Priority - minPriorityClass) / (maxPriorityClass - minPriorityClass),
	}
	ssn.AddQueueScoreFn(pp.Name(), queueScoreFn)
```
### Job'Priority inherits Queue's Priority
```go
// /volcano/pkg/scheduler/cache/cache.go
            priName := value.PodGroup.Spec.PriorityClassName
			if priorityClass, found := sc.PriorityClasses[priName]; found {
				value.Priority = priorityClass.Value
			} else {
				if priorityClass, found := sc.PriorityClasses[snapshot.Queues[value.Queue].Queue.Spec.PriorityClassName]; found {
					value.Priority = priorityClass.Value
				}
			}
```    
# 中文
## 动机
目前来说，Volcano具备了Job和Task级别的优先级设置，但是缺乏Queue的优先级配置，Queue优先级可以在以下场景中发挥作用：
1. 任务和Job的提交者并不能决定其优先级，他们只知道往其中一个队列提交。
2. 队列优先级能力可以一定程度降低Job和任务提交者的复杂度，队列具备优先级后，任务可以根据自身任务情况适当调整优先级即可。
3. 队列优先级能力能够帮助该队列中的资源更优先分配资源，在基于队列去划分业务重要程度的场景下非常合适。
4. 队列回收过程中可以利用`PriorityClass`优先考虑优先级高的队列去回收其他队列的资源。
## 影响面
### 对session_plugins.go中QueueOrderFn的影响：
队列的排序由Priority(当前我们设计的Queue's PriorityClass)、proportion、drf三个plugin来决定。这里在Scheduler的配置上有三种主要情况。
1. Priority Plugin的PriorityClass在最前，如果此时所有队列的优先级设置的比较分散，每个队列基本上是不一样的优先级，
   此时QueueOrderFn基本上只会以PriorityClass来作为排序的规则。 而后面的DRF和Proportion很难发挥作用，除非队列的优先级相同。
   另外一点是，不论优先级设置是否分散，都容易导致低优先级的队列饿死。
2. Priority Plugin的PriorityClass在Proportion和DRF的中间，这种情况相比于第一种情况低优先级饿死的情况会好些，
   但是一样会导致一个问题，无论是DRF还是Proportion谁在最后面，都很难发挥作用。另外这种情况下Priority也很难发挥作用，因为在前面的DRF和Proportion基本上就能决定顺序。
3. Priority Plugin的PriorityClass在最后，这种情况下，Priority基本上无法发挥作用，因为排在前面的DRF和proportion基本上就能决定出来队列顺序，
   除非是DRF和Proportion计算的Share都为0或者1。
### 更好的QueueOrderFn -> QueueScoreOrderFn。
以上的配置适合那些仅考虑一种或两种队列排序的需求，或者其中一种为主排序策略，另外的为辅排序策略的需求。
在这里提出一种兼顾三种排序的策略，类似于计算NodeScore的方式利用三种优先级计算每个队列得分，得分最高的队列具有最高的优先级。
而用户可以自定义这三个plugin的占比，以满足对某种优先级的倾向性。
* Priority的PriorityClass分数计算
  `Score = (PriorityClass of queue - Minimum PriorityClass)/(Maximum PriorityClass - Minimum PriorityClass) * Weight of Priority Plugin`
* DRF的QueueOrder分数计算
  `Score = (1 - node.attr.share/node.weight) * Weight of DRF Plugin`
* Proportion的QueueOrder分数计算
  `Score = (1 - queue.share) * Weight of Proportion Plugin`
  `Total Score = Priority's Score + DRF's Score + Proportion's Score`
#### QueueScoreOrderFn 例子：
```
Maximum PriorityClass = 100
Minimum PriorityClass = 0
Priority Weight = 1
DRF Weight = 1
Proportion Weight = 1
```
|        | Priority | DRF  Share | Proportion Share | Priority Score | DRF Score | Proportion Score | Total Score |
|--------|----------|------------|------------------|----------------|-----------|------------------|-------------|
| QueueA | 40       | 0.3        | 1.2              | 0.4            | 0.7       | -0.2             | 0.9         |
| QueueB | 80       | 0.4        | 0.5              | 0.8            | 0.6       | 0.5              | 1.9         |
| QueueC | 0        | 0.5        | 0.3              | 0              | 0.5       | 0.7              | 1.2         |
QueueScoreOrder： Queue B > Queue C > Queue A
### 对Job的PriorityClass的影响：
由于引入Queue的PriorityClass，因此，对**于默认没有PriorityClass的Job自然是继承Queue的PriorityClass**。
*通过对QueueScore的想法，这里提出一个思考：*
目前无论是Enqueue、Allocate、Reclaim几个Action中遵循的都是队列排序完后，进行队列内的Job排序。如果出现一下情况，当前的Action逻辑还可以理解为正常么？
如果两者都有Priority，A队列(优先级10)中有A1 Job(优先级10)，A2 Job(优先级12). B队列(优先级8)中有B1 Job(优先级8)，B2 Job(优先级13).
按照现在三个Action的逻辑都是如下：
* A Queue
    * A2 Job
    * A1 Job
* B Queue
    * B2 Job
    * B1 Job
      但是如果作为一个调度实体来说，Job是一个调度实体，那B2 Job的优先级是否比A队列中两个Job优先级还高呢？更早执行调度逻辑。
* B2 Job
* A2 Job
* A1 Job
* B1 Job
  而drf或proportion中的QueueOrderFn会是影响JobOrder的一个因素。当然这两种方式的角度不同，但从本质来说都没有问题，只是应用场景不同。
## 设计
### API
```
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: q1
spec:
  reclaimable: true
  weight: 1
  priorityClassName: system-cluster-critical
```
队列的优先级配置沿用了Kubernetes原生的PriorityClass。
### Scheduler 配置
```
actions: "enqueue, allocate, backfill"
configurations:
- name: enqueue:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1 # key: QueueScoreOrder.pluginName.weight
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
- name: allocate:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
- name: reclaim:
  arguments:
    QueueScoreOrderEnable: true
    QueueScoreOrder.priority.weight: 1
    QueueScoreOrder.drf.weight: 1
    QueueScoreOrder.proportion.weight: 1
tiers:
- plugins:
  - name: priority
  - name: gang
    enablePreemptable: false
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
  - name: binpack
```
## Implementation
**pseudo code**
### QueueScoreOrder
```go
// /volcano/pkg/scheduler/framework/session_plugins.go
// QueueScoreOrderFn invoke queueScore function of the plugins
func (ssn *Session) QueueScoreOrderFn(l, r interface{}, action string) bool {
	args := GetArgOfActionFromConf(ssn.Configurations, action)
	lTotalScore := 0
	rTotalScore := 0
	queueOrderWeight := 0
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledQueueScoreOrder) {
				continue
			}
			qof, found := ssn.queueScoreFns[plugin.Name]
			if !found {
				continue
			}
			lScore, rScore := qof(l, r)
			args.GetInt(&queueOrderWeight, fmt.Sprintf("QueueScoreOrder.%s.weight", plugin.Name))
			lTotalScore += lScore * queueOrderWeight
			rTotalScore += rScore * queueOrderWeight
		}
	}
	if lTotalScore != rTotalScore {
		return lTotalScore > rTotalScore
	}
	// If no queue order funcs, order queue by CreationTimestamp first, then by UID.
	lv := l.(*api.QueueInfo)
	rv := r.(*api.QueueInfo)
	if lv.Queue.CreationTimestamp.Equal(&rv.Queue.CreationTimestamp) {
		return lv.UID < rv.UID
	}
	return lv.Queue.CreationTimestamp.Before(&rv.Queue.CreationTimestamp)
}
```
```go
// /volcano/pkg/scheduler/actions/enqueue/enqueue.go
func (enqueue *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Enqueue ...")
	defer klog.V(3).Infof("Leaving Enqueue ...")
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	QueueScoreOrderEnable := false
	framework.GetArgOfActionFromConf(ssn.Configurations, enqueue.Name()).GetBool(&QueueScoreOrderEnable, "QueueScoreOrderEnable")
	if QueueScoreOrderEnable {
		queues = util.NewPriorityQueue(ssn.QueueScoreOrderFn)
	}
	....
```
```go
// /volcano/pkg/scheduler/plugins/priority/priority.go
	queueScoreFn := func(l, r interface{}) (float32, float32) {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)
		maxPriorityClass = ssn.PriorityClass["max"]
		minPriorityClass = ssn.PriorityClass["min"]
		return (lv.Priority - minPriorityClass) / (maxPriorityClass - minPriorityClass),
			(rv.Priority - minPriorityClass) / (maxPriorityClass - minPriorityClass),
	}
	ssn.AddQueueScoreFn(pp.Name(), queueScoreFn)
```
### Job'Priority inherits Queue's Priority
```go
// /volcano/pkg/scheduler/cache/cache.go
            priName := value.PodGroup.Spec.PriorityClassName
			if priorityClass, found := sc.PriorityClasses[priName]; found {
				value.Priority = priorityClass.Value
			} else {
				if priorityClass, found := sc.PriorityClasses[snapshot.Queues[value.Queue].Queue.Spec.PriorityClassName]; found {
					value.Priority = priorityClass.Value
				}
			}
```