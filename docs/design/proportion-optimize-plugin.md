# Proportion Optimize Plugin#

## Motivation

Proportion Optimize plugin is Optimized from proportion. Please check [Capacity Plugin Design](./capacity-plugin.md) to understand the problem of capacity and weight in the original proportion plugin. Even if Capacity is split, however, due to the characteristics of weight, the deserved resources of each Queue will be diluted, as a result, there is a strong contradiction between guarantee and weight.

## Design

Like [Capacity Plugin Design](./capacity-plugin.md) , Guarantee & Elastic will be a optional attribute. your can turn on/off them.

```
const (
	Guarantee    = "proportion_optimize.GuaranteeEnable"
	Elastic      = "proportion_optimize.ElasticEnable"
)

type GuaranteeElasticEnable struct {
	ElasticEnable   bool
	GuaranteeEnable bool
	TotalGuarantee  *api.Resource
}
```

#### Fix contradiction between guarantee and weight

Balancing Deserved and Guarantee due to weight dilution is not easy, Any new queue must be carefully set Guarantee and Weight to ensure that the Proportion does not go wrong. Therefore, proposed two compromise mechanisms here.

**Guarantee priority**

If Guarantee priority, the weight of queue will be update to meet the deserved can be larger than the guarantee in any dimension. Of course, the change of weight will be guaranteed to have the smallest impact.

```
// if PriorityType is guarantee
// if any dimension in guarantee is beyond deserved，try to make weight of queue meet to max(dimension in guarantee/deserved)
if pp.proportionOptimizeEnable.GuaranteeEnable && pp.priorityType == PriorityTypeGuarantee {
	pp.tryToMeetWeight(remaining)
	for _, attr := range pp.queueOpts {
		klog.V(4).Infof("After meet weight, The weight of queue <%s> in proportion_optimize: Weight <%v>",
			attr.Name, attr.Weight)
	}
}
func (pp *proportionOptimizePlugin) tryToMeetWeight(remaining *api.Resource) {
	totalWeight := int32(0)
	for _, attr := range pp.queueOpts {
		totalWeight += attr.Weight
	}
	for _, attr := range pp.queueOpts {
		weightDeserved := remaining.Clone().Multi(float64(attr.Weight) / float64(totalWeight))
		if weightDeserved.LessPartly(attr.Guarantee, api.Zero) {
			// need update weight
			attr.Weight = remaining.MaxWeight(attr.Guarantee)
		}
	}
}
```

**Weight Prioriy**

If Weight priority, Guarantee will not affect the deserved calculation stage. After deserved calculation, it will make up for those dimensions that cannot reach the Guaranty in Deserved.

```
// if PriorityType is weight.
// try to make dimension of deserved is less guarantee meet to guarantee
if pp.proportionOptimizeEnable.GuaranteeEnable && pp.priorityType == PriorityTypeWeight {
	pp.tryToMeetGuarantee(remaining)
	for _, attr := range pp.queueOpts {
		common.UpdateShare(attr)
		klog.V(4).Infof("After meet guarantee, The attributes of queue <%s> in proportion_optimize: deserved <%v>, share <%0.2f>",
			attr.Name, attr.Deserved, attr.Share)
	}
}

func (pp *proportionOptimizePlugin) tryToMeetGuarantee(remaining *api.Resource) {
	// for cpu
	cpuQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
		lv := l.(*common.QueueAttr)
		rv := r.(*common.QueueAttr)
		return (lv.Guarantee.MilliCPU - lv.Deserved.MilliCPU) < (rv.Guarantee.MilliCPU - rv.Deserved.MilliCPU)
	})
	for _, attr := range pp.queueOpts {
		if attr.Deserved.MilliCPU < attr.Guarantee.MilliCPU {
			cpuQueue.Push(attr)
		}
	}
	if remaining.MilliCPU > 0 {
		for {
			if cpuQueue.Empty() {
				break
			}
			cpuAvg := remaining.MilliCPU / float64(cpuQueue.Len())
			attr := cpuQueue.Pop().(*common.QueueAttr)
			if attr.Guarantee.MilliCPU-attr.Deserved.MilliCPU >= cpuAvg {
				remaining.MilliCPU -= cpuAvg
				attr.Deserved.MilliCPU += cpuAvg
			} else {
				remaining.MilliCPU = remaining.MilliCPU - (attr.Guarantee.MilliCPU - attr.Deserved.MilliCPU)
				attr.Deserved.MilliCPU = attr.Guarantee.MilliCPU
			}
		}
	}
	// for memory
	memoryQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
		lv := l.(*common.QueueAttr)
		rv := r.(*common.QueueAttr)
		return (lv.Guarantee.Memory - lv.Deserved.Memory) < (rv.Guarantee.Memory - rv.Deserved.Memory)
	})
	for _, attr := range pp.queueOpts {
		if attr.Deserved.Memory < attr.Guarantee.Memory {
			memoryQueue.Push(attr)
		}
	}
	if remaining.Memory > 0 {
		for {
			if memoryQueue.Empty() {
				break
			}
			memoryAvg := remaining.Memory / float64(memoryQueue.Len())
			attr := memoryQueue.Pop().(*common.QueueAttr)
			if attr.Guarantee.Memory-attr.Deserved.Memory >= memoryAvg {
				remaining.Memory -= memoryAvg
				attr.Deserved.Memory += memoryAvg
			} else {
				remaining.Memory = remaining.Memory - (attr.Guarantee.Memory - attr.Deserved.Memory)
				attr.Deserved.Memory = attr.Guarantee.Memory
			}
		}
	}
	// for others
	for name, _ := range remaining.ScalarResources {
		otherQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
			lv := l.(*common.QueueAttr)
			rv := r.(*common.QueueAttr)
			return (lv.Guarantee.ScalarResources[name] - lv.Deserved.ScalarResources[name]) < (rv.Guarantee.ScalarResources[name] - rv.Deserved.ScalarResources[name])
		})
		for _, attr := range pp.queueOpts {
			if attr.Deserved.ScalarResources[name] < attr.Guarantee.ScalarResources[name] {
				otherQueue.Push(attr)
			}
		}
		if remaining.ScalarResources[name] > 0 {
			for {
				if otherQueue.Empty() {
					break
				}
				otherAvg := remaining.ScalarResources[name] / float64(otherQueue.Len())
				attr := otherQueue.Pop().(*common.QueueAttr)
				if attr.Guarantee.ScalarResources[name]-attr.Deserved.ScalarResources[name] >= otherAvg {
					remaining.ScalarResources[name] -= otherAvg
					attr.Deserved.ScalarResources[name] += otherAvg
				} else {
					remaining.ScalarResources[name] = remaining.ScalarResources[name] - (attr.Guarantee.ScalarResources[name] - attr.Deserved.ScalarResources[name])
					attr.Deserved.ScalarResources[name] = attr.Guarantee.ScalarResources[name]
				}
			}
		}
	}
}
```

**Scheduler Conf**

`proportion_optimize.GuaranteeEnable` and `proportion_optimize.ElasticEnable` default is true

`proportion_optimize.PriorityType` default is Weight

```
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
  - name: proportion_optimize
    arguments:
      proportion_optimize.GuaranteeEnable: true
      proportion_optimize.ElasticEnable: true
      proportion_optimize.PriorityType: Weight
  - name: nodeorder
  - name: binpack
```