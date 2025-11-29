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
	"time"

	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	schedfwk "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/clock"
	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

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

// Less is the function used by the activeQ heap algorithm to sort pods. Currently, we use the same logic as PrioritySort plugin.
func Less(pInfo1, pInfo2 schedfwk.QueuedPodInfo) bool {
	p1 := corev1helpers.PodPriority(pInfo1.GetPodInfo().GetPod())
	p2 := corev1helpers.PodPriority(pInfo2.GetPodInfo().GetPod())
	return (p1 > p2) || (p1 == p2 && pInfo1.GetTimestamp().Before(pInfo2.GetTimestamp()))
}
