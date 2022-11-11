package common

import (
	"k8s.io/klog"
	"math"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

type GuaranteeElasticEnable struct {
	ElasticEnable   bool
	GuaranteeEnable bool
	TotalGuarantee  *api.Resource
}

func EnableGuaranteeElastic(args framework.Arguments, ssn *framework.Session, guaranteeKey, elasticKey string) *GuaranteeElasticEnable {
	capacityEnable := &GuaranteeElasticEnable{
		GuaranteeEnable: true,
		ElasticEnable:   true,
		TotalGuarantee:  api.EmptyResource(),
	}
	args.GetBool(&capacityEnable.GuaranteeEnable, guaranteeKey)
	args.GetBool(&capacityEnable.ElasticEnable, elasticKey)
	klog.V(4).Infof("Enable guarantee in capacity plugin: <%v>, Enable elastic in capacity: <%v>", capacityEnable.GuaranteeEnable, capacityEnable.ElasticEnable)
	if capacityEnable.GuaranteeEnable {
		for _, queue := range ssn.Queues {
			if len(queue.Queue.Spec.Guarantee.Resource) == 0 {
				continue
			}
			guaranteeTmp := api.NewResource(queue.Queue.Spec.Guarantee.Resource)
			capacityEnable.TotalGuarantee.Add(guaranteeTmp)
		}
		klog.V(4).Infof("The total Guarantee resource is <%v>", capacityEnable.TotalGuarantee)
	}
	return capacityEnable
}

func GenerateQueueCommonInfo(queue *api.QueueInfo, job *api.JobInfo, queueOpts map[api.QueueID]*QueueAttr,
	totalResource *api.Resource, guaranteeElasticEnable *GuaranteeElasticEnable) {
	attr := &QueueAttr{
		QueueID: queue.UID,
		Name:    queue.Name,

		Deserved:  api.EmptyResource(),
		Allocated: api.EmptyResource(),
		Request:   api.EmptyResource(),
		Elastic:   api.EmptyResource(),
		Inqueue:   api.EmptyResource(),
		Guarantee: api.EmptyResource(),
	}
	if len(queue.Queue.Spec.Capability) != 0 {
		attr.Capability = api.NewResource(queue.Queue.Spec.Capability)
		if attr.Capability.MilliCPU <= 0 {
			attr.Capability.MilliCPU = math.MaxFloat64
		}
		if attr.Capability.Memory <= 0 {
			attr.Capability.Memory = math.MaxFloat64
		}
	}
	realCapability := totalResource
	if guaranteeElasticEnable.GuaranteeEnable {
		if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
			attr.Guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
		}
		// if total guarantee is beyond total resource？ how to fix。
		realCapability = realCapability.Sub(guaranteeElasticEnable.TotalGuarantee).Add(attr.Guarantee)
	}
	if attr.Capability == nil {
		attr.RealCapability = realCapability
	} else {
		attr.RealCapability = helpers.Min(realCapability, attr.Capability)
	}
	queueOpts[job.Queue] = attr
	klog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
}

func GenerateQueueResource(attr *QueueAttr, job *api.JobInfo, guaranteeElasticEnable *GuaranteeElasticEnable) {
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) {
			for _, t := range tasks {
				attr.Allocated.Add(t.Resreq)
				attr.Request.Add(t.Resreq)
			}
		} else if status == api.Pending {
			for _, t := range tasks {
				attr.Request.Add(t.Resreq)
			}
		}
	}

	if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
		attr.Inqueue.Add(job.GetMinResources())
	}

	// calculate inqueue resource for running jobs
	// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
	// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
	if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
		job.PodGroup.Spec.MinResources != nil &&
		int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
		allocated := util.GetAllocatedResource(job)
		inqueued := util.GetInqueueResource(job, allocated)
		attr.Inqueue.Add(inqueued)
	}

	// only calculate elastic resource when elastic enable
	if guaranteeElasticEnable.ElasticEnable {
		attr.Elastic.Add(job.GetElasticResources())
	}
	klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
		attr.Name, attr.Allocated.String(), attr.Request.String(), attr.Inqueue.String(), attr.Elastic.String())

}

type QueueAttr struct {
	QueueID api.QueueID
	Name    string
	Weight  int32
	Share   float64

	Deserved  *api.Resource
	Allocated *api.Resource
	Request   *api.Resource
	// Elastic represents the sum of job's Elastic resource, job's Elastic = job.allocated - job.minAvailable
	Elastic *api.Resource
	// Inqueue represents the resource Request of the Inqueue job
	Inqueue    *api.Resource
	Capability *api.Resource
	// RealCapability represents the resource limit of the queue, LessEqual capability
	RealCapability *api.Resource
	Guarantee      *api.Resource
}

func UpdateShare(attr *QueueAttr) {
	res := float64(0)

	// TODO(k82cn): how to handle fragment issues?
	for _, rn := range attr.Deserved.ResourceNames() {
		share := helpers.Share(attr.Allocated.Get(rn), attr.Deserved.Get(rn))
		if share > res {
			res = share
		}
	}

	attr.Share = res
	metrics.UpdateQueueShare(attr.Name, attr.Share)
}

func RecordMetrics(queues map[api.QueueID]*api.QueueInfo, queueAttrs map[api.QueueID]*QueueAttr) {
	// Record metrics
	for queueID, queueInfo := range queues {
		if _, ok := queueAttrs[queueID]; !ok {
			metrics.UpdateQueueAllocatedMetrics(queueInfo.Name, &api.Resource{}, &api.Resource{}, &api.Resource{}, &api.Resource{})
		} else {
			attr := queueAttrs[queueID]
			metrics.UpdateQueueAllocatedMetrics(attr.Name, attr.Allocated, attr.Capability, attr.Guarantee, attr.RealCapability)
			metrics.UpdateQueueRequest(attr.Name, attr.Request.MilliCPU, attr.Request.Memory)
			metrics.UpdateQueueWeight(attr.Name, attr.Weight)
			queue := queues[attr.QueueID]
			metrics.UpdateQueuePodGroupStatusCount(attr.Name, queue.Queue.Status)
		}
	}
}
