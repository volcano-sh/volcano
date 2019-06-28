/*
Copyright 2018 The Kubernetes Authors.

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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/apis/utils"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

const (
	shadowPodGroupKey = "volcano/shadow-pod-group"
)

func shadowPodGroup(pg *v1alpha1.PodGroup) bool {
	if pg == nil {
		return true
	}

	_, found := pg.Annotations[shadowPodGroupKey]

	return found
}

func createShadowPodGroup(pod *v1.Pod) *v1alpha1.PodGroup {
	jobID := api.JobID(utils.GetController(pod))
	if len(jobID) == 0 {
		jobID = api.JobID(pod.UID)
	}

	return &v1alpha1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pod.Namespace,
			Name:      string(jobID),
			Annotations: map[string]string{
				shadowPodGroupKey: string(jobID),
			},
		},
		Spec: v1alpha1.PodGroupSpec{
			MinMember: 1,
		},
	}
}

// responsibleForPod returns true if the pod has asked to be scheduled by the given scheduler.
func responsibleForPod(pod *v1.Pod, schedulerName string) bool {
	return schedulerName == pod.Spec.SchedulerName
}
