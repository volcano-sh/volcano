/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/util/podutils"
	"k8s.io/kubernetes/test/e2e/framework"
)

// This file is copied from k8s.io/kubernetes/test/e2e/framework/statefulset/wait.go and fixtures.go

const (
	// StatefulSetPoll is a poll interval for StatefulSet tests
	StatefulSetPoll = 1 * time.Second
	// StatefulSetTimeout is a timeout interval for StatefulSet operations
	StatefulSetTimeout = 1 * time.Minute
)

// WaitForRunning waits for numPodsRunning in ss to be Running and for the first
// numPodsReady ordinals to be Ready.
func WaitForRunning(ctx context.Context, c clientset.Interface, numPodsRunning, numPodsReady int32, ss *appsv1.StatefulSet) {
	pollErr := wait.PollUntilContextTimeout(ctx, StatefulSetPoll, StatefulSetTimeout, true,
		func(ctx context.Context) (bool, error) {
			podList := GetPodList(ctx, c, ss.Namespace, ss.Spec.Selector)
			SortStatefulPods(podList)
			if int32(len(podList.Items)) < numPodsRunning {
				framework.Logf("Found %d stateful pods, waiting for %d", len(podList.Items), numPodsRunning)
				return false, nil
			}
			if int32(len(podList.Items)) > numPodsRunning {
				return false, fmt.Errorf("too many pods scheduled, expected %d got %d", numPodsRunning, len(podList.Items))
			}
			for _, p := range podList.Items {
				shouldBeReady := getStatefulPodOrdinal(&p) < int(numPodsReady)
				isReady := podutils.IsPodReady(&p)
				desiredReadiness := shouldBeReady == isReady
				framework.Logf("Waiting for pod %v to enter %v - Ready=%v, currently %v - Ready=%v", p.Name, v1.PodRunning, shouldBeReady, p.Status.Phase, isReady)
				if p.Status.Phase != v1.PodRunning || !desiredReadiness {
					return false, nil
				}
			}
			return true, nil
		})
	if pollErr != nil {
		framework.Failf("Failed waiting for pods to enter running: %v", pollErr)
	}
}

// WaitForRunningAndReady waits for numStatefulPods in ss to be Running and Ready.
func WaitForRunningAndReady(ctx context.Context, c clientset.Interface, numStatefulPods int32, ss *appsv1.StatefulSet) {
	WaitForRunning(ctx, c, numStatefulPods, numStatefulPods, ss)
}

type statefulPodsByOrdinal []v1.Pod

func (sp statefulPodsByOrdinal) Len() int {
	return len(sp)
}

func (sp statefulPodsByOrdinal) Swap(i, j int) {
	sp[i], sp[j] = sp[j], sp[i]
}

func (sp statefulPodsByOrdinal) Less(i, j int) bool {
	return getStatefulPodOrdinal(&sp[i]) < getStatefulPodOrdinal(&sp[j])
}

// SortStatefulPods sorts pods by their ordinals
func SortStatefulPods(pods *v1.PodList) {
	sort.Sort(statefulPodsByOrdinal(pods.Items))
}

var statefulPodRegex = regexp.MustCompile("(.*)-([0-9]+)$")

func getStatefulPodOrdinal(pod *v1.Pod) int {
	ordinal := -1
	subMatches := statefulPodRegex.FindStringSubmatch(pod.Name)
	if len(subMatches) < 3 {
		return ordinal
	}
	if i, err := strconv.ParseInt(subMatches[2], 10, 32); err == nil {
		ordinal = int(i)
	}
	return ordinal
}
