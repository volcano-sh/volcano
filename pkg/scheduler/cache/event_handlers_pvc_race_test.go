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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// TestAddPodWithUnresolvedPVCCachesTaskForResync verifies the full
// recovery path when a Pod ADD races ahead of its PVC ADD:
//
//  1. addPod caches the task and enqueues it for resync instead of
//     permanently dropping the pod.
//  2. Once the PVC arrives on the informer, one processResyncTask
//     tick refreshes the task via syncTask and removes it from
//     errTasks — the pod is now visible to the scheduler.
//
// Mirrors the DRA counterpart TestAddPodWithUnresolvedResourceClaimTemplateCachesTaskForResync,
// extended with the end-to-end recovery assertion.
func TestAddPodWithUnresolvedPVCCachesTaskForResync(t *testing.T) {
	sc := newMockSchedulerCache("volcano")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-with-pvc",
			Namespace: "default",
			UID:       types.UID("pod-with-pvc-uid"),
			Annotations: map[string]string{
				schedulingv1beta1.KubeGroupNameAnnotationKey: "pg-with-pvc",
			},
		},
		Spec: v1.PodSpec{
			SchedulerName: "volcano",
			Volumes: []v1.Volume{
				{
					Name: "scratch",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "eventual-pvc",
						},
					},
				},
			},
		},
	}

	// Seed the pod in the fake kube-client so syncTask's later
	// Pods(ns).Get(name) call finds it on the retry tick.
	_, err := sc.kubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	assert.NoError(t, err)

	sc.informerFactory.Start(ctx.Done())
	for informer, synced := range sc.informerFactory.WaitForCacheSync(ctx.Done()) {
		assert.Truef(t, synced, "informer %v failed to sync", informer)
	}

	// Phase 1: PVC ADD has not arrived yet. addPod must defer to resync.
	err = sc.addPod(pod)
	assert.NoError(t, err, "addPod must defer to the retry loop when the PVC is missing from the informer cache")

	job, found := sc.Jobs[schedulingapi.JobID("default/pg-with-pvc")]
	assert.True(t, found, "job must be added to sc.Jobs so processResyncTask can find its task by UID")
	if assert.NotNil(t, job) {
		_, found = job.Tasks[schedulingapi.TaskID("pod-with-pvc-uid")]
		assert.True(t, found, "task must be added to job.Tasks so getTaskByUID succeeds on the retry tick")
	}
	assert.Equal(t, 1, sc.errTasks.Len(), "one entry must be enqueued on errTasks so processResyncTask visits this task")

	// Phase 2: PVC arrives on the informer. processResyncTask should
	// refresh the task via syncTask and remove it from errTasks.
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eventual-pvc",
			Namespace: "default",
		},
	}
	_, err = sc.kubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Wait for the PVC informer to observe the new PVC.
	err = wait.PollUntilContextTimeout(ctx, 20*time.Millisecond, 2*time.Second, true, func(context.Context) (bool, error) {
		_, err := sc.pvcInformer.Lister().PersistentVolumeClaims("default").Get("eventual-pvc")
		return err == nil, nil
	})
	assert.NoError(t, err, "PVC never appeared on the informer lister")

	// Drive one iteration of the resync worker. This pops the taskKey,
	// calls syncTask (fresh kubeClient Get → NewTaskInfo → PVC lookup
	// now succeeds), and Forgets the taskKey on success.
	sc.processResyncTask()

	assert.Equal(t, 0, sc.errTasks.Len(), "task must be removed from errTasks once the PVC has synced and syncTask succeeds")
}
