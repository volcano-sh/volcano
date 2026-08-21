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

	v1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/hintutil"
)

// CapacityHintProvider implements QueueingHints for the capacity plugin.
//
// It is intentionally stateless. Precise sibling-Queue filtering requires the
// shared incremental Queue hierarchy/accounting model discussed in #5565;
// QueueingHints must not introduce a second cache-side hierarchy solely for
// this plugin. Until that model exists, resource-release events are matched
// against the rejection's recorded Queue path without resolving sibling Queue
// ancestry.
type CapacityHintProvider struct{}

var _ api.HintProvider = (*CapacityHintProvider)(nil)

// EventsToRegister implements api.HintProvider. Queue and Node hints inspect
// whether the change can relax the recorded rejection. Pod and PodGroup hints
// additionally require resource release from a Queue in the recorded rejection
// path.
func (p *CapacityHintProvider) EventsToRegister(_ context.Context) ([]api.ClusterEventWithHint, error) {
	return []api.ClusterEventWithHint{
		// Only an update can relax a Queue object used by the rejected path.
		{Event: api.ClusterEvent{Resource: api.QueueEvent, ActionType: fwk.Update}, HintFn: queueHint},
		// A consuming PodGroup can release quota on update or deletion.
		{Event: api.ClusterEvent{Resource: api.PodGroupEvent, ActionType: fwk.Update | fwk.Delete}, HintFn: podGroupHint},
		// Node capacity is global, so capacity shares the topology-free Node hint.
		{Event: api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add | fwk.UpdateNodeAllocatable}, HintFn: hintutil.NodeHint},
		// Pod deletion or scale-down can release usage charged to the Queue path.
		{Event: api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete | fwk.UpdatePodScaleDown}, HintFn: podHint},
	}, nil
}

// queueWasCheckedForRejection reports whether queueName is the rejected Job's
// Queue or one of the ancestors checked by capacity. It is used for Queue object
// updates, where the updated Queue itself must have participated in the previous
// rejection.
func queueWasCheckedForRejection(rejection api.Rejection, queueName string, jobQueue api.QueueID) bool {
	qid := api.QueueID(queueName)
	// Older or non-hierarchical rejections may not record a path; in that case
	// only the Job's own Queue is known to be relevant.
	if len(rejection.Queues) == 0 {
		return qid == jobQueue
	}
	// Hierarchical capacity records every Queue whose quota rejected the Job.
	for _, q := range rejection.Queues {
		if q == qid {
			return true
		}
	}
	return false
}

// queueHint wakes when an updated Queue participated in the rejection and the
// update can loosen one of the admission or capacity constraints it enforced.
func queueHint(job *api.JobInfo, rejection api.Rejection, oldObj, newObj any) (api.HintResult, error) {
	// Queue Update should carry the new object. Unknown payloads fail open.
	newQueue, ok := newObj.(*scheduling.Queue)
	if !ok || newQueue == nil {
		return api.HintWakeup, nil
	}
	if !queueWasCheckedForRejection(rejection, newQueue.Name, job.Queue) {
		return api.HintSkip, nil
	}
	// The old object is required to prove that a relevant constraint relaxed.
	oldQueue, ok := oldObj.(*scheduling.Queue)
	if !ok || oldQueue == nil {
		return api.HintWakeup, nil
	}
	// Changes that only tighten quota or touch unrelated fields cannot help.
	if queueCapacityRelaxed(oldQueue, newQueue) {
		return api.HintWakeup, nil
	}
	return api.HintSkip, nil
}

// queueCapacityRelaxed reports whether a Queue update can invalidate a previous
// capacity rejection:
//
//   - opening the Queue removes the explicit admission/allocation block;
//   - changing Parent changes the ancestor path and inherited effective quota;
//     the provider cannot determine whether the new path is looser without the
//     other Queue objects, so it wakes conservatively;
//   - increasing capability raises a hard quota limit, while removing a
//     capability dimension makes that dimension unlimited;
//   - increasing guarantee can raise the Queue's realCapability;
//   - increasing deserved can raise the Queue's fair resource share.
func queueCapacityRelaxed(oldQueue, newQueue *scheduling.Queue) bool {
	// Reopening removes the Queue-state admission block.
	if oldQueue.Status.State != scheduling.QueueStateOpen && newQueue.Status.State == scheduling.QueueStateOpen {
		return true
	}
	// A parent change replaces the inherited quota path; without the shared
	// hierarchy we cannot prove whether the replacement is tighter or looser.
	if oldQueue.Spec.Parent != newQueue.Spec.Parent {
		return true
	}
	// These are the three resource bounds used by capacity admission/allocation.
	return hintutil.ResourceLimitRelaxed(oldQueue.Spec.Capability, newQueue.Spec.Capability) ||
		hintutil.ResourceListIncreased(oldQueue.Spec.Guarantee.Resource, newQueue.Spec.Guarantee.Resource) ||
		hintutil.ResourceListIncreased(oldQueue.Spec.Deserved, newQueue.Spec.Deserved)
}

// podGroupHint wakes a rejected Job when an Inqueue/Running PodGroup releases
// quota from a Queue recorded in the capacity rejection. A release from a
// sibling Queue is intentionally not inferred here: doing that reliably needs
// the shared Queue hierarchy/accounting model tracked by #5565.
func podGroupHint(job *api.JobInfo, rejection api.Rejection, oldObj, newObj any) (api.HintResult, error) {
	if newObj == nil {
		// Delete releases quota only if the old PodGroup consumed it in a Queue
		// that participated in this Job's rejection.
		pg, ok := oldObj.(*api.PodGroup)
		if !ok || pg == nil {
			return api.HintWakeup, nil
		}
		if queueWasCheckedForRejection(rejection, pg.Spec.Queue, job.Queue) && hintutil.PodGroupConsumesQuota(pg.Status.Phase) {
			return api.HintWakeup, nil
		}
		return api.HintSkip, nil
	}

	// Update needs both snapshots to determine the old Queue and phase change.
	oldPg, oldOK := oldObj.(*api.PodGroup)
	newPg, newOK := newObj.(*api.PodGroup)
	if !oldOK || oldPg == nil || !newOK || newPg == nil {
		return api.HintWakeup, nil
	}
	if !queueWasCheckedForRejection(rejection, oldPg.Spec.Queue, job.Queue) {
		return api.HintSkip, nil
	}
	// The old Queue benefits when the PodGroup stops consuming there or moves.
	if hintutil.PodGroupReleasedQuota(oldPg, newPg) {
		return api.HintWakeup, nil
	}
	return api.HintSkip, nil
}

// podHint wakes a rejected Job when deletion or in-place scale-down releases
// resources from a Queue recorded in the capacity rejection. A release from a
// Queue outside the recorded path is skipped. When the Pod carries no queue
// identity the hint fails open and wakes, so only vcjob Pods are filtered
// precisely; other Pods keep the queue on their PodGroup, not on the Pod.
func podHint(job *api.JobInfo, rejection api.Rejection, oldObj, _ any) (api.HintResult, error) {
	// Both deletion and scale-down release resources from the old Pod snapshot.
	pod, ok := oldObj.(*v1.Pod)
	if !ok || pod == nil {
		return api.HintWakeup, nil
	}
	queue := podQueue(pod)
	// Pods that inherit Queue through PodGroup cannot be scoped safely here.
	if queue == "" {
		return api.HintWakeup, nil
	}
	if queueWasCheckedForRejection(rejection, queue, job.Queue) {
		return api.HintWakeup, nil
	}
	return api.HintSkip, nil
}

// podQueue returns the Pod's queue when it can be read directly from the Pod.
// The vcjob controller stamps volcano.sh/queue-name on every managed Pod; a
// non-vcjob Pod only carries scheduling.volcano.sh/queue-name when the user set
// it to select a queue. Pods that instead inherit their queue from the PodGroup
// return "", and podHint treats that as unknown.
func podQueue(pod *v1.Pod) string {
	// vcjob-managed Pods always carry the batch queue annotation.
	if queue := pod.Annotations[batch.QueueNameKey]; queue != "" {
		return queue
	}
	// Standalone Pods may opt into a Queue through the scheduling annotation.
	return pod.Annotations[schedulingv1beta1.QueueNameAnnotationKey]
}
