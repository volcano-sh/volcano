/*
Copyright 2026 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hintprovider

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/resourcefit"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

type ResourceFitHintProvider struct{}

func (p *ResourceFitHintProvider) EventsToRegister(context.Context) ([]unschedulable.EventWithHint, error) {
	podActionType := fwk.Delete
	if utilfeature.DefaultFeatureGate.Enabled(features.InPlacePodVerticalScaling) {
		podActionType |= fwk.UpdatePodScaleDown
	}
	return []unschedulable.EventWithHint{
		{
			Event:       fwk.ClusterEvent{Resource: fwk.Pod, ActionType: podActionType},
			JobKeysFn:   resourcefit.PodReleaseJobKeys,
			EventKeysFn: resourcefit.PodReleaseEventKeys,
			HintFn:      resourceFitPodHint,
		},
		{
			Event:       fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.UpdateNodeAllocatable},
			JobKeysFn:   resourcefit.NodeGrowthJobKeys,
			EventKeysFn: resourcefit.NodeGrowthEventKeys,
			HintFn:      resourceFitNodeHint,
		},
		{
			Event:       fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add},
			JobKeysFn:   resourcefit.NodeAddJobKeys,
			EventKeysFn: resourcefit.NodeAddEventKeys,
			HintFn:      resourceFitNodeHint,
		},
	}, nil
}

func resourceFitPodHint(job *api.JobInfo, rejection unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok || oldPod == nil {
		return unschedulable.HintWakeup, fmt.Errorf("expected old object to be *v1.Pod, got %T", oldObj)
	}

	if newObj == nil {
		// 1. Deleting a rejected task changes the Job and triggers a retry.
		if rejectionIncludesPod(rejection, oldPod) {
			return unschedulable.HintWakeup, nil
		}

		// 2. Deleting an unrelated pending or terminated Pod frees no node
		// resources; deleting a scheduled Pod may free requested resources.
		if oldPod.Spec.NodeName == "" || podTerminated(oldPod) {
			return unschedulable.HintSkip, nil
		}
		return resourceReleaseHint(job, rejection, api.GetPodResourceRequest(oldPod), api.EmptyResource()), nil
	}

	newPod, ok := newObj.(*v1.Pod)
	if !ok || newPod == nil {
		return unschedulable.HintWakeup, fmt.Errorf("expected new object to be *v1.Pod, got %T", newObj)
	}
	if !utilfeature.DefaultFeatureGate.Enabled(features.InPlacePodVerticalScaling) {
		return unschedulable.HintSkip, nil
	}

	// 3. Scaling down an unrelated pending Pod does not affect node resources.
	if oldPod.Spec.NodeName == "" && !rejectionIncludesPod(rejection, oldPod) {
		return unschedulable.HintSkip, nil
	}

	// 4. For an in-place scale-down, retry only when the Pod request decreased
	// in a resource dimension requested by a rejected task.
	return resourceReleaseHint(job, rejection, api.GetPodResourceRequest(oldPod), api.GetPodResourceRequest(newPod)), nil
}

func resourceFitNodeHint(job *api.JobInfo, rejection unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	newNode, ok := newObj.(*v1.Node)
	if !ok || newNode == nil {
		return unschedulable.HintWakeup, fmt.Errorf("expected new object to be *v1.Node, got %T", newObj)
	}
	// 1. A newly added Node triggers a retry when its allocatable resources can
	// satisfy at least one rejected task's initial resource request.
	if oldObj == nil {
		return newNodeFitHint(job, rejection, api.NewResource(newNode.Status.Allocatable)), nil
	}
	oldNode, ok := oldObj.(*v1.Node)
	if !ok || oldNode == nil {
		return unschedulable.HintWakeup, fmt.Errorf("expected old object to be *v1.Node, got %T", oldObj)
	}

	// 2. A Node update triggers a retry only when requested allocatable
	// resources increase.
	return allocatableIncreaseHint(job, rejection, api.NewResource(oldNode.Status.Allocatable), api.NewResource(newNode.Status.Allocatable)), nil
}

// newNodeFitHint reports whether a newly added Node's allocatable resources can
// satisfy at least one rejected task's initial resource request. Missing Job,
// rejection, or resource data returns HintWakeup.
func newNodeFitHint(job *api.JobInfo, rejection unschedulable.Rejection, newAllocatable *api.Resource) unschedulable.HintResult {
	if job == nil || len(rejection.Tasks) == 0 || newAllocatable == nil {
		return unschedulable.HintWakeup
	}
	for _, taskID := range rejection.Tasks {
		task := job.Tasks[taskID]
		if task == nil {
			return unschedulable.HintWakeup
		}
		if task.InitResreq == nil {
			return unschedulable.HintWakeup
		}
		if task.InitResreq.LessEqual(newAllocatable, api.Zero) {
			return unschedulable.HintWakeup
		}
	}
	return unschedulable.HintSkip
}

// resourceReleaseHint reports whether a Pod event releases a resource requested
// by at least one rejected task:
//   - job contains the current TaskInfo and each task's InitResreq.
//   - rejection.Tasks identifies the tasks that failed resource fit.
//   - oldPodRequest is the event Pod's request before the change.
//   - newPodRequest is the event Pod's request after the change. It is empty for
//     deletion and termination events.
//
// The released quantity is not compared with the task request because the
// rejection does not record the idle resources available on each Node. For
// example, a task requesting 2 CPUs may have failed on a Node with 1 idle CPU;
// releasing 1 CPU can make the task fit even though the released quantity is
// less than the task request. The next scheduling cycle performs the complete
// resource-fit check. Missing Job or rejection data returns HintWakeup.
func resourceReleaseHint(job *api.JobInfo, rejection unschedulable.Rejection, oldPodRequest, newPodRequest *api.Resource) unschedulable.HintResult {
	if job == nil || len(rejection.Tasks) == 0 {
		return unschedulable.HintWakeup
	}
	for _, taskID := range rejection.Tasks {
		task := job.Tasks[taskID]
		if task == nil {
			return unschedulable.HintWakeup
		}
		if requestedResourceDecreased(task.InitResreq, oldPodRequest, newPodRequest) {
			return unschedulable.HintWakeup
		}
	}
	return unschedulable.HintSkip
}

// allocatableIncreaseHint reports whether a Node update may satisfy at least one
// rejected task's initial resource request:
//   - job contains the current TaskInfo and each task's InitResreq.
//   - rejection.Tasks identifies the tasks that failed resource fit.
//   - oldAllocatable and newAllocatable are the Node's total allocatable
//     resources before and after the update. They do not include task
//     allocations.
//
// A task triggers HintWakeup only when it fits within newAllocatable and one of
// its requested resource dimensions increased. The event does not include the task state required to calculate
// FutureIdle, so the next scheduling cycle performs the complete resource-fit
// check. Missing Job, rejection, or resource data returns HintWakeup.
func allocatableIncreaseHint(job *api.JobInfo, rejection unschedulable.Rejection, oldAllocatable, newAllocatable *api.Resource) unschedulable.HintResult {
	if job == nil || len(rejection.Tasks) == 0 || oldAllocatable == nil || newAllocatable == nil {
		return unschedulable.HintWakeup
	}
	for _, taskID := range rejection.Tasks {
		task := job.Tasks[taskID]
		if task == nil {
			return unschedulable.HintWakeup
		}
		if task.InitResreq == nil {
			return unschedulable.HintWakeup
		}
		if task.InitResreq.LessEqual(newAllocatable, api.Zero) && requestedResourceIncreased(task.InitResreq, oldAllocatable, newAllocatable) {
			return unschedulable.HintWakeup
		}
	}
	return unschedulable.HintSkip
}

// requestedResourceDecreased reports whether oldValue is greater than newValue
// in at least one resource dimension with a positive request. The requested
// quantity is not compared with the size of the decrease.
func requestedResourceDecreased(request, oldValue, newValue *api.Resource) bool {
	return requestedResourceIncreased(request, newValue, oldValue)
}

// requestedResourceIncreased reports whether newValue is greater than oldValue
// in at least one resource dimension with a positive request. Missing input
// returns true.
func requestedResourceIncreased(request, oldValue, newValue *api.Resource) bool {
	if request == nil || oldValue == nil || newValue == nil {
		return true
	}
	if request.MilliCPU > 0 && newValue.MilliCPU > oldValue.MilliCPU {
		return true
	}
	if request.Memory > 0 && newValue.Memory > oldValue.Memory {
		return true
	}
	for name, quantity := range request.ScalarResources {
		if quantity > 0 && newValue.ScalarResources[name] > oldValue.ScalarResources[name] {
			return true
		}
	}
	return false
}

// podTerminated reports whether the Pod no longer consumes node resources.
func podTerminated(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

// rejectionIncludesPod reports whether the event Pod is one of the tasks that
// produced this resource-fit rejection.
func rejectionIncludesPod(rejection unschedulable.Rejection, pod *v1.Pod) bool {
	taskID := api.TaskID(pod.UID)
	for _, rejectedTaskID := range rejection.Tasks {
		if rejectedTaskID == taskID {
			return true
		}
	}
	return false
}
