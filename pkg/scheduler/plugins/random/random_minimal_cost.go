package random

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"math"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const (
	PluginName               = "random"
	TryNum                   = "try-num"
	DefaultTryNum            = 1000
	WeightCostWeight         = "weight-cost-weight"
	DefaultWeightCostWeight  = 1
	RequestCostWeight        = "request-cost-weight"
	DefaultRequestCostWeight = 1
	CostFunction             = "cost-function"
	DefaultCostFunction      = "eulerDistance"
)

type randomPlugin struct {
	totalResource  *api.Resource
	totalGuarantee *api.Resource
	queueOpts      map[api.QueueID]*queueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	tryNum            int
	weightCostWeight  int
	requestCostWeight int
	costFunction      string
}

type queueAttr struct {
	queueID api.QueueID
	name    string
	weight  int32
	share   float64

	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
	// elastic represents the sum of job's elastic resource, job's elastic = job.allocated - job.minAvailable
	elastic *api.Resource
	// inqueue represents the resource request of the inqueue job
	inqueue    *api.Resource
	capability *api.Resource
	// realCapability represents the resource limit of the queue, LessEqual capability
	realCapability *api.Resource
	guarantee      *api.Resource
}

// New return proportion action
func New(arguments framework.Arguments) framework.Plugin {
	return &randomPlugin{
		totalResource:   api.EmptyResource(),
		totalGuarantee:  api.EmptyResource(),
		queueOpts:       map[api.QueueID]*queueAttr{},
		pluginArguments: arguments,
	}
}

func (rp *randomPlugin) Name() string {
	return PluginName
}

func (rp *randomPlugin) OnSessionOpen(ssn *framework.Session) {

	rp.pluginArguments.GetInt(&rp.tryNum, TryNum)
	if rp.tryNum == 0 {
		rp.tryNum = DefaultTryNum
	}
	rp.pluginArguments.GetInt(&rp.tryNum, WeightCostWeight)
	if rp.weightCostWeight == 0 {
		rp.weightCostWeight = DefaultWeightCostWeight
	}
	rp.pluginArguments.GetInt(&rp.tryNum, RequestCostWeight)
	if rp.requestCostWeight == 0 {
		rp.requestCostWeight = DefaultRequestCostWeight
	}
	rp.pluginArguments.GetString(&rp.costFunction, CostFunction)
	if len(rp.costFunction) == 0 {
		rp.costFunction = DefaultCostFunction
	}

	// Prepare scheduling data for this session.
	rp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", rp.totalResource)
	for _, queue := range ssn.Queues {
		if len(queue.Queue.Spec.Guarantee.Resource) == 0 {
			continue
		}
		guarantee := api.NewResource(queue.Queue.Spec.Guarantee.Resource)
		rp.totalGuarantee.Add(guarantee)
	}
	klog.V(4).Infof("The total guarantee resource is <%v>", rp.totalGuarantee)
	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := rp.queueOpts[job.Queue]; !found {
			queue := ssn.Queues[job.Queue]
			attr := &queueAttr{
				queueID: queue.UID,
				name:    queue.Name,
				weight:  queue.Weight,

				deserved:  api.EmptyResource(),
				allocated: api.EmptyResource(),
				request:   api.EmptyResource(),
				elastic:   api.EmptyResource(),
				inqueue:   api.EmptyResource(),
				guarantee: api.EmptyResource(),
			}
			if len(queue.Queue.Spec.Capability) != 0 {
				attr.capability = api.NewResource(queue.Queue.Spec.Capability)
				if attr.capability.MilliCPU <= 0 {
					attr.capability.MilliCPU = math.MaxFloat64
				}
				if attr.capability.Memory <= 0 {
					attr.capability.Memory = math.MaxFloat64
				}
			}
			if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
				attr.guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
			}
			realCapability := rp.totalResource.Clone().Sub(rp.totalGuarantee).Add(attr.guarantee)
			if attr.capability == nil {
				attr.realCapability = realCapability
			} else {
				attr.realCapability = helpers.Min(realCapability, attr.capability)
			}
			rp.queueOpts[job.Queue] = attr
			klog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
		}

		attr := rp.queueOpts[job.Queue]
		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			attr.inqueue.Add(job.GetMinResources())
		}

		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			allocated := util.GetAllocatedResource(job)
			inqueued := util.GetInqueueResource(job, allocated)
			attr.inqueue.Add(inqueued)
		}
		attr.elastic.Add(job.GetElasticResources())
		klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
			attr.name, attr.allocated.String(), attr.request.String(), attr.inqueue.String(), attr.elastic.String())
	}

	for queueID, queueInfo := range ssn.Queues {
		if _, ok := rp.queueOpts[queueID]; !ok {
			metrics.UpdateQueueAllocated(queueInfo.Name, 0, 0)
		}
	}

	// Record metrics
	for _, attr := range rp.queueOpts {
		metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)
		metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory)
		metrics.UpdateQueueWeight(attr.name, attr.weight)
		queue := ssn.Queues[attr.queueID]
		metrics.UpdateQueuePodGroupInqueueCount(attr.name, queue.Queue.Status.Inqueue)
		metrics.UpdateQueuePodGroupPendingCount(attr.name, queue.Queue.Status.Pending)
		metrics.UpdateQueuePodGroupRunningCount(attr.name, queue.Queue.Status.Running)
		metrics.UpdateQueuePodGroupUnknownCount(attr.name, queue.Queue.Status.Unknown)
	}

	rp.Calculate()
	for _, attr := range rp.queueOpts {
		klog.V(4).Infof("The attributes of queue <%s> in proportion: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	ssn.AddQueueOrderFn(rp.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if rp.queueOpts[lv.UID].share == rp.queueOpts[rv.UID].share {
			return 0
		}

		if rp.queueOpts[lv.UID].share < rp.queueOpts[rv.UID].share {
			return -1
		}

		return 1
	})

	ssn.AddReclaimableFn(rp.Name(), func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}

		for _, reclaimee := range reclaimees {
			job := ssn.Jobs[reclaimee.Job]
			attr := rp.queueOpts[job.Queue]

			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.LessPartly(reclaimer.Resreq, api.Zero) {
				klog.V(3).Infof("Failed to allocate resource for Task <%s/%s> in Queue <%s>, not enough resource.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			if !allocated.LessEqual(attr.deserved, api.Zero) {
				allocated.Sub(reclaimee.Resreq)
				victims = append(victims, reclaimee)
			}
		}
		klog.V(4).Infof("Victims from proportion plugins are %+v", victims)
		return victims, util.Permit
	})

	ssn.AddOverusedFn(rp.Name(), func(obj interface{}) bool {
		queue := obj.(*api.QueueInfo)
		attr := rp.queueOpts[queue.UID]

		overused := attr.deserved.LessEqual(attr.allocated, api.Zero)
		metrics.UpdateQueueOverused(attr.name, overused)
		if overused {
			klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>, share <%v>",
				queue.Name, attr.deserved, attr.allocated, attr.share)
		}

		return overused
	})

	ssn.AddAllocatableFn(rp.Name(), func(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		attr := rp.queueOpts[queue.UID]

		free, _ := attr.deserved.Diff(attr.allocated, api.Zero)
		allocatable := candidate.Resreq.LessEqual(free, api.Zero)
		if !allocatable {
			klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>; Candidate <%v>: resource request <%v>",
				queue.Name, attr.deserved, attr.allocated, candidate.Name, candidate.Resreq)
		}

		return allocatable
	})

	ssn.AddJobEnqueueableFn(rp.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		queueID := job.Queue
		attr := rp.queueOpts[queueID]
		queue := ssn.Queues[queueID]
		// If no capability is set, always enqueue the job.
		if attr.realCapability == nil {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}
		minReq := job.GetMinResources()

		klog.V(5).Infof("job %s min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
			job.Name, minReq.String(), queue.Name, attr.realCapability.String(), attr.allocated.String(), attr.inqueue.String(), attr.elastic.String())
		// The queue resource quota limit has not reached
		inqueue := minReq.Add(attr.allocated).Add(attr.inqueue).Sub(attr.elastic).LessEqual(attr.realCapability, api.Infinity)
		klog.V(5).Infof("job %s inqueue %v", job.Name, inqueue)
		if inqueue {
			attr.inqueue.Add(job.GetMinResources())
			return util.Permit
		}
		ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "queue resource quota insufficient")
		return util.Reject
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := rp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			rp.updateShare(attr)

			klog.V(4).Infof("Random AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := rp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			rp.updateShare(attr)

			klog.V(4).Infof("Random EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
	})
}

func (rp *randomPlugin) OnSessionClose(ssn *framework.Session) {
	rp.totalResource = nil
	rp.totalGuarantee = nil
	rp.queueOpts = nil
}

func (rp *randomPlugin) updateShare(attr *queueAttr) {
	res := float64(0)

	// TODO(k82cn): how to handle fragment issues?
	for _, rn := range attr.deserved.ResourceNames() {
		share := helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn))
		if share > res {
			res = share
		}
	}

	attr.share = res
	metrics.UpdateQueueShare(attr.name, attr.share)
}
