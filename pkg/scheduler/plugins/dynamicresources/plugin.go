package dynamicresources

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/capabilities/dynamicresources"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

const (
	PluginName = "dynamicresources"
)

// Since "classic" DRA, which also called control plane controller DRA will be withdrawn in v1.32, we only implement the new structed parameters DRA
// related extension points, including PreEnqueue, PreFilter, Filter, Reserve, PreBind and Unreserve.
// Currently, PostFilter has not been implemented yet. If PostFilter truly needed, we may add a new extension point called PostPredicateFn when PrePredicateFn or PredicateFn fail.
// There are very rare scenarios when PostFilter needed. Only such following scenarios: Pod has two Claims that require PreBind, the first claim PreBind succeeded,
// but the second Claim PreBind failed (e.g., the api-server down), therefore the pod was not scheduled successfully and will be scheduled again.
// In the next scheduling cycle, the node for which the first claim allocated is no longer available somehow. At this time, the PostFilter will
// be called to try to remove the Allocation and ReservedFor of the claim.
type dynamicResourcesPlugin struct {
	*dynamicresources.DynamicResources
}

func New(arguments framework.Arguments) framework.Plugin {
	return &dynamicResourcesPlugin{}
}

func (d *dynamicResourcesPlugin) Name() string {
	return PluginName
}

func (d *dynamicResourcesPlugin) RegisterPrePredicateFn(ssn *framework.Session) {
	ssn.AddPrePredicateFn(d.Name(), func(task *api.TaskInfo) error {
		// 1. Check each ResourceClaim for the pod exists
		// PreEnqueue is used to verify whether a Pod can be added to scheduler queue, but Volcano uses PodGroup as the granularity
		// to determine whether it can be added to a Queue, so it is more appropriate to place PreEnqueue in PrePredicate here.
		status := d.PreEnqueue(context.TODO(), task.Pod)
		if !status.IsSuccess() {
			return fmt.Errorf("failed to check each ResourceClaim for the pod exists with err: %s", strings.Join(status.Reasons(), "."))
		}

		// 2. Run Prefilter
		// Init cycle state for pod to share it with other extension points
		state := k8sframework.NewCycleState()
		state.SkipFilterPlugins = sets.New[string]()
		_, status = d.PreFilter(context.TODO(), state, task.Pod)
		switch status.Code() {
		case k8sframework.Skip:
			// If PreFilter returns skip, then Filter needs to skip.
			state.SkipFilterPlugins.Insert(d.Name())
		case k8sframework.Error, k8sframework.UnschedulableAndUnresolvable:
			return status.AsError()
		default:
		}
		// In PrePredicate, needs to initialize the cycleState and then store into CycleStatsMap
		ssn.CycleStatesMap.Store(task.UID, state)

		return nil
	})
}

func (d *dynamicResourcesPlugin) RegisterPredicateFn(ssn *framework.Session) {
	ssn.AddPredicateFn(d.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		v, exist := ssn.CycleStatesMap.Load(task.UID)
		if !exist {
			return api.NewFitError(task, node, "scheduling state is not exist")
		}
		state := v.(*k8sframework.CycleState)
		if state.SkipFilterPlugins.Has(d.Name()) {
			return nil
		}
		nodeInfo, exist := ssn.NodeMap[node.Name]
		if !exist {
			return api.NewFitError(task, node, "node info not found")
		}
		status := d.Filter(context.TODO(), state, task.Pod, nodeInfo)
		switch status.Code() {
		case k8sframework.Error, k8sframework.UnschedulableAndUnresolvable:
			return status.AsError()
		default:
		}

		return nil
	})
}

func (d *dynamicResourcesPlugin) RegisterPreBindFn(ssn *framework.Session) {
	ssn.AddPreBindFns(d.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		v, exist := ssn.CycleStatesMap.Load(task.UID)
		if !exist {
			return api.NewFitError(task, node, "scheduling state is not exist")
		}
		state := v.(*k8sframework.CycleState)
		status := d.PreBind(context.TODO(), state, task.Pod, node.Name)
		switch status.Code() {
		case k8sframework.Error, k8sframework.UnschedulableAndUnresolvable:
			return status.AsError()
		default:
		}

		return nil
	})
}

func (d *dynamicResourcesPlugin) RegisterUnPreBindFn(ssn *framework.Session) {
	ssn.AddUnPreBindFns(d.Name(), func(task *api.TaskInfo, node *api.NodeInfo) {
		v, exist := ssn.CycleStatesMap.Load(task.UID)
		if !exist {
			klog.Errorf("scheduling context of task <%s/%s> is not exist", task.Namespace, task.Name)
		}
		state := v.(*k8sframework.CycleState)
		d.Unreserve(context.TODO(), state, task.Pod, node.Name)
	})
}

func (d *dynamicResourcesPlugin) RegisterEventHandler(ssn *framework.Session) {
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			v, exist := ssn.CycleStatesMap.Load(event.Task.UID)
			if !exist {
				event.Err = fmt.Errorf("scheduling context of task <%s/%s> is not exist", event.Task.Namespace, event.Task.Name)
			}
			state := v.(*k8sframework.CycleState)
			status := d.Reserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
			switch status.Code() {
			case k8sframework.Error, k8sframework.UnschedulableAndUnresolvable:
				event.Err = status.AsError()
			default:
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			v, exist := ssn.CycleStatesMap.Load(event.Task.UID)
			if !exist {
				event.Err = fmt.Errorf("scheduling context of task <%s/%s> is not exist", event.Task.Namespace, event.Task.Name)
			}
			state := v.(*k8sframework.CycleState)
			d.Unreserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		},
	})
}

func (d *dynamicResourcesPlugin) OnSessionOpen(ssn *framework.Session) {
	featureGates := feature.Features{
		EnableDynamicResourceAllocation: utilFeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation),
	}
	handle := k8s.NewFrameworkHandle(ssn.NodeMap, ssn.KubeClient(), ssn.InformerFactory(),
		k8s.WithResourceClaimCache(ssn.GetResourceClaimCache()))
	plugin, _ := dynamicresources.NewDRAPlugin(context.TODO(), nil, handle, featureGates)
	draPlugin := plugin.(*dynamicresources.DynamicResources)
	d.DynamicResources = draPlugin

	ssn.BindContextEnabledPlugins = append(ssn.BindContextEnabledPlugins, PluginName)
	d.RegisterPrePredicateFn(ssn)
	d.RegisterPredicateFn(ssn)
	d.RegisterPreBindFn(ssn)
	d.RegisterUnPreBindFn(ssn)
	d.RegisterEventHandler(ssn)
}

func (d *dynamicResourcesPlugin) OnSessionClose(ssn *framework.Session) {}
