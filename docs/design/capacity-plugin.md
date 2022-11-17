# Capacity Plugin

## Motivation

Capacity plugin is split from proportion, as a long time, capacity of queue works in proportion plugin, proportion plugin is a weight-base plugin, but capacity is not suitable with weight-base.There are contradictions between them.As the total weight increases, the capacity will slowly become invalid.

### Design

Capacity plugin will continue use Guarantee & Elastic attribute, Guarantee & Elastic are good features, but if Guarantee set too large, Capacity will also be affected, and Elastic can only play a better role when combined with ClusterAutoScaler, if Elastic use in a Fixed Capacity Cluster, may encounter the of pending task num explosion.

As is said above, some problems may be caused in some scenarios with Guarantee & Elastic, so Guarantee & Elastic will be a optional attribute. your can turn on/off them.

```
const (
	Guarantee = "GuaranteeEnable"
	Elastic   = "ElasticEnable"
)

type GuaranteeElasticEnable struct {
	ElasticEnable   bool
	GuaranteeEnable bool
	TotalGuarantee  *api.Resource
}
```

**Scheduler Conf**

`capacity.GuaranteeEnable` and `capacity.ElasticEnable` default is true

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
  - name: capacity
    arguments:
      capacity.GuaranteeEnable: true
      capacity.ElasticEnable: true
  - name: nodeorder
  - name: binpack
```

**Derserved**

```
for _, attr := range pp.queueOpts {
	attr.Deserved = attr.RealCapability
	attr.Deserved.MinDimensionResource(attr.Request, api.Zero)
	if pp.capacityEnable.GuaranteeEnable {
		attr.Deserved = helpers.Max(attr.Deserved, attr.Guarantee)
	}
	common.UpdateShare(attr)
	klog.V(5).Infof("Queue %s capacity <%s> realCapacity <%s> allocated <%s> request <%s> deserved <%s> inqueue <%s> elastic <%s>",
		attr.Name, attr.Capability.String(), attr.RealCapability.String(), attr.Allocated.String(), attr.Request.String(),
		attr.Deserved.String(), attr.Inqueue.String(), attr.Elastic.String())
}
```