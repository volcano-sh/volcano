/*
Copyright 2021 The Volcano Authors.

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

package cache

import (
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	schedfwk "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/clock"

	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

const (
	// AnnotationSchedulingPriority is used to mark the internal scheduling priority of a pod
	// Higher value means higher priority in the scheduling queue
	AnnotationSchedulingPriority = "volcano.sh/scheduling-priority"
	// AnnotationSchedulingPriorityReason records why the pod gets this priority
	AnnotationSchedulingPriorityReason = "volcano.sh/scheduling-priority-reason"
	// AnnotationSchedulingPriorityTimestamp records when the priority was set
	AnnotationSchedulingPriorityTimestamp = "volcano.sh/scheduling-priority-timestamp"
)

// Scheduling priority levels - higher value means higher priority
const (
	SchedulingPriorityUrgent int = 100 // For urgent retry scenarios (e.g., binding conflict)
	SchedulingPriorityHigh   int = 50  // For high priority scenarios
	SchedulingPriorityNormal int = 0   // Default priority
)

// RequeueOptions contains options for requeuing a pod with custom priority
type RequeueOptions struct {
	Priority int
	Reason   string // Reason for requeue
}

type schedulerOptions struct {
	clock                             clock.WithTicker
	podInitialBackoffSeconds          int64
	podMaxBackoffSeconds              int64
	podMaxInUnschedulablePodsDuration time.Duration
}

// TODO: these default values can be overwritten by config file or command line args
var defaultSchedulerOptions = schedulerOptions{
	clock:                             clock.RealClock{},
	podInitialBackoffSeconds:          int64(k8sschedulingqueue.DefaultPodInitialBackoffDuration.Seconds()),
	podMaxBackoffSeconds:              int64(k8sschedulingqueue.DefaultPodMaxBackoffDuration.Seconds()),
	podMaxInUnschedulablePodsDuration: k8sschedulingqueue.DefaultPodMaxInUnschedulablePodsDuration,
}

// SetPodSchedulingPriority sets the scheduling priority for a pod (in-memory only, not persisted)
// This is used to control the order in which pods are popped from the scheduling queue.
func SetPodSchedulingPriority(pod *v1.Pod, opts RequeueOptions) {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[AnnotationSchedulingPriority] = strconv.Itoa(opts.Priority)
	if opts.Reason != "" {
		pod.Annotations[AnnotationSchedulingPriorityReason] = opts.Reason
	}
	pod.Annotations[AnnotationSchedulingPriorityTimestamp] = strconv.FormatInt(time.Now().UnixNano(), 10)
}

// GetPodSchedulingPriority extracts the scheduling priority from pod annotations
// Returns SchedulingPriorityNormal (0) if not set
func GetPodSchedulingPriority(pod *v1.Pod) int {
	if pod.Annotations == nil {
		return SchedulingPriorityNormal
	}
	priorityStr, ok := pod.Annotations[AnnotationSchedulingPriority]
	if !ok {
		return SchedulingPriorityNormal
	}
	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		return SchedulingPriorityNormal
	}
	return priority
}

// Less is the function used by the activeQ heap algorithm to sort pods.
// Priority order:
// 1. Internal scheduling priority (set via annotations, e.g., for urgent retry scenarios such as binding conflict)
// 2. Pod priority class (from PodSpec)
// 3. Timestamp (earlier is higher priority)
func Less(pInfo1, pInfo2 schedfwk.QueuedPodInfo) bool {
	pod1 := pInfo1.GetPodInfo().GetPod()
	pod2 := pInfo2.GetPodInfo().GetPod()

	// 1. Compare internal scheduling priority (from annotations)
	schedulingPriority1 := GetPodSchedulingPriority(pod1)
	schedulingPriority2 := GetPodSchedulingPriority(pod2)

	if schedulingPriority1 != schedulingPriority2 {
		return schedulingPriority1 > schedulingPriority2
	}

	// 2. If scheduling priorities are equal, compare pod priority class
	podPriority1 := corev1helpers.PodPriority(pod1)
	podPriority2 := corev1helpers.PodPriority(pod2)

	if podPriority1 != podPriority2 {
		return podPriority1 > podPriority2
	}

	// 3. If pod priorities are also equal, compare timestamp (FIFO for same priority)
	return pInfo1.GetTimestamp().Before(pInfo2.GetTimestamp())
}
