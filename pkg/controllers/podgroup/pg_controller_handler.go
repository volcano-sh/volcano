/*
Copyright 2019 The Volcano Authors.

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

package podgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

type resourceRequest struct {
	Name      string
	Namespace string
}

type metadataForMergePatch struct {
	Metadata annotationForMergePatch `json:"metadata"`
}

type annotationForMergePatch struct {
	Annotations map[string]string `json:"annotations"`
}

func (pg *pgcontroller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	req := resourceRequest{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}

	pg.podQueue.Add(req)
}

func (pg *pgcontroller) addReplicaSet(obj interface{}) {
	rs, ok := obj.(*appsv1.ReplicaSet)
	if !ok {
		klog.Errorf("Failed to convert %v to appsv1.ReplicaSet", obj)
		return
	}

	if *rs.Spec.Replicas == 0 {
		pgName := batchv1alpha1.PodgroupNamePrefix + string(rs.UID)
		klog.V(4).Infof("Delete podgroup %s for replicaset %s/%s spec.replicas == 0",
			pgName, rs.Namespace, rs.Name)
		err := pg.vcClient.SchedulingV1beta1().PodGroups(rs.Namespace).Delete(context.TODO(), pgName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete PodGroup <%s/%s>: %v", rs.Namespace, pgName, err)
		}
	}

	// In the rolling upgrade scenario, the addReplicasSet(replicas=0) event may be received before
	// the updateReplicaSet(replicas=1) event. In this event, need to create PodGroup for the pod.
	if *rs.Spec.Replicas > 0 {
		selector := metav1.LabelSelector{MatchLabels: rs.Spec.Selector.MatchLabels}
		podList, err := pg.kubeClient.CoreV1().Pods(rs.Namespace).List(context.TODO(),
			metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(&selector)})
		if err != nil {
			klog.Errorf("Failed to list pods for ReplicaSet %s: %v", klog.KObj(rs), err)
			return
		}
		if podList != nil && len(podList.Items) > 0 {
			pod := podList.Items[0]
			klog.V(4).Infof("Try to create podgroup for pod %s", klog.KObj(&pod))
			if !slices.Contains(pg.schedulerNames, pod.Spec.SchedulerName) {
				klog.V(4).Infof("Pod %s field SchedulerName is not matched", klog.KObj(&pod))
				return
			}
			err := pg.createNormalPodPGIfNotExist(&pod)
			if err != nil {
				klog.Errorf("Failed to create PodGroup for pod %s: %v", klog.KObj(&pod), err)
			}
		}
	}
}

func (pg *pgcontroller) updateReplicaSet(oldObj, newObj interface{}) {
	pg.addReplicaSet(newObj)
}

func (pg *pgcontroller) addLeaderWorkerSet(obj interface{}) {
	lws, ok := obj.(*lwsv1.LeaderWorkerSet)
	if !ok {
		klog.Errorf("Failed to convert %T to LeaderWorkerSet", obj)
		return
	}

	req := resourceRequest{
		Name:      lws.Name,
		Namespace: lws.Namespace,
	}

	pg.lwsQueue.Add(req)
}

func (pg *pgcontroller) updateLeaderWorkerSet(oldObj, newObj interface{}) {
	newLws, ok := newObj.(*lwsv1.LeaderWorkerSet)
	if !ok {
		klog.Errorf("Failed to convert %T to LeaderWorkerSet", newObj)
		return
	}

	req := resourceRequest{
		Name:      newLws.Name,
		Namespace: newLws.Namespace,
	}

	pg.lwsQueue.Add(req)
}

func (pg *pgcontroller) updatePodAnnotations(pod *v1.Pod, pgName string) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] == "" {
		patch := metadataForMergePatch{
			Metadata: annotationForMergePatch{
				Annotations: map[string]string{
					scheduling.KubeGroupNameAnnotationKey: pgName,
				},
			},
		}

		patchBytes, err := json.Marshal(&patch)
		if err != nil {
			klog.Errorf("Failed to json.Marshal pod annotation: %v", err)
			return err
		}

		if _, err := pg.kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			klog.Errorf("Failed to update pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
			return err
		}
	} else {
		if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] != pgName {
			klog.Errorf("normal pod %s/%s annotations %s value is not %s, but %s", pod.Namespace, pod.Name,
				scheduling.KubeGroupNameAnnotationKey, pgName, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
		}
	}
	return nil
}

func (pg *pgcontroller) getAnnotationsFromUpperRes(kind string, name string, namespace string) map[string]string {
	switch kind {
	case "ReplicaSet":
		rs, err := pg.kubeClient.AppsV1().ReplicaSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", kind, namespace, name, err)
			return map[string]string{}
		}
		return rs.Annotations
	case "DaemonSet":
		ds, err := pg.kubeClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", kind, namespace, name, err)
			return map[string]string{}
		}
		return ds.Annotations
	case "StatefulSet":
		ss, err := pg.kubeClient.AppsV1().StatefulSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", kind, namespace, name, err)
			return map[string]string{}
		}
		return ss.Annotations
	case "Job":
		job, err := pg.kubeClient.BatchV1().Jobs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", kind, namespace, name, err)
			return map[string]string{}
		}
		return job.Annotations
	default:
		return map[string]string{}
	}
}

// Inherit annotations from upper resources.
func (pg *pgcontroller) inheritUpperAnnotations(pod *v1.Pod, obj *scheduling.PodGroup) {
	if pg.inheritOwnerAnnotations {
		for _, reference := range pod.OwnerReferences {
			if reference.Kind != "" && reference.Name != "" {
				var upperAnnotations = pg.getAnnotationsFromUpperRes(reference.Kind, reference.Name, pod.Namespace)
				for k, v := range upperAnnotations {
					if strings.HasPrefix(k, scheduling.AnnotationPrefix) {
						obj.Annotations[k] = v
					}
				}
			}
		}
	}
}

func (pg *pgcontroller) createNormalPodPGIfNotExist(pod *v1.Pod) error {
	podProvider := NewPodProvider(pod)
	pgName := podProvider.GetName()

	if _, err := pg.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		obj := &scheduling.PodGroup{
			ObjectMeta: podProvider.GetObjectMeta(),
			Spec:       podProvider.GetSpec(),
			Status: scheduling.PodGroupStatus{
				Phase: scheduling.PodGroupPending,
			},
		}

		pg.inheritUpperAnnotations(pod, obj)
		// Individual annotations on pods would overwrite annotations inherited from upper resources.
		if queueName, ok := pod.Annotations[scheduling.QueueNameAnnotationKey]; ok {
			obj.Spec.Queue = queueName
		}

		if value, ok := pod.Annotations[scheduling.PodPreemptable]; ok {
			obj.Annotations[scheduling.PodPreemptable] = value
		}
		if value, ok := pod.Annotations[scheduling.CooldownTime]; ok {
			obj.Annotations[scheduling.CooldownTime] = value
		}
		if value, ok := pod.Annotations[scheduling.RevocableZone]; ok {
			obj.Annotations[scheduling.RevocableZone] = value
		}
		if value, ok := pod.Labels[scheduling.PodPreemptable]; ok {
			obj.Labels[scheduling.PodPreemptable] = value
		}
		if value, ok := pod.Labels[scheduling.CooldownTime]; ok {
			obj.Labels[scheduling.CooldownTime] = value
		}

		if value, found := pod.Annotations[scheduling.JDBMinAvailable]; found {
			obj.Annotations[scheduling.JDBMinAvailable] = value
		} else if value, found := pod.Annotations[scheduling.JDBMaxUnavailable]; found {
			obj.Annotations[scheduling.JDBMaxUnavailable] = value
		}

		if _, err := pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Create(context.TODO(), obj, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				klog.Errorf("Failed to create normal PodGroup for Pod <%s/%s>: %v",
					pod.Namespace, pod.Name, err)
				return err
			} else {
				klog.V(4).Infof("PodGroup <%s/%s> already exists for Pod <%s/%s>",
					pod.Namespace, pgName, pod.Namespace, pod.Name)
			}
		} else {
			klog.V(4).Infof("PodGroup <%s/%s> created for Pod <%s/%s>",
				pod.Namespace, pgName, pod.Namespace, pod.Name)
		}
	}

	return pg.updatePodAnnotations(pod, pgName)
}

func (pg *pgcontroller) syncLeaderWorkerSetPodGroups(lws *lwsv1.LeaderWorkerSet) error {
	targetReplicas := lws.Status.Replicas
	// .Status.Replicas reflects the current number of groups in the LeaderWorkerSet, especially if the LeaderWorkerSet is
	// undergoing a rolling update and MaxSurge is configured, the replicas of the LeaderWorkerSet will increase,
	// requiring the creation of additional PodGroups.

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			lwsv1.SetNameLabelKey: lws.Name,
		},
	}
	pgSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		klog.Errorf("failed to create label selector for leaderworkerset %s/%s: %v", lws.Namespace, lws.Name, err)
		return err
	}

	existedPgs, err := pg.pgLister.PodGroups(lws.Namespace).List(pgSelector)
	if err != nil {
		klog.Errorf("failed to list podgroups for leaderworkerset %s/%s: %v", lws.Namespace, lws.Name, err)
		return err
	}

	// Delete extra PodGroups, two scenarios:
	// 1. Scale down
	// 2. Rolling update completed, the additional maxSurge PodGroups need to be deleted
	for index := targetReplicas; index < int32(len(existedPgs)); index++ {
		pgName := fmt.Sprintf(LeaderWorkerSetPodGroupNameFmt, lws.Name, index)
		err := pg.vcClient.SchedulingV1beta1().PodGroups(lws.Namespace).Delete(context.TODO(), pgName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("failed to delete podgroup <%s/%s>: %v", lws.Namespace, pgName, err)
			return err
		}

		klog.V(4).Infof("deleted podgroup <%s/%s>", lws.Namespace, pgName)
	}

	for index := 0; index < int(targetReplicas); index++ {
		err := pg.createOrUpdateLeaderWorkerSetPodGroup(lws, index)
		if err != nil {
			return fmt.Errorf("failed to create/update podgroup for leaderworkerset %s/%s at group index %d: %v", lws.Namespace, lws.Name, index, err)
		}
	}

	return nil
}

func (pg *pgcontroller) createOrUpdateLeaderWorkerSetPodGroup(lws *lwsv1.LeaderWorkerSet, index int) error {
	lwsProvider := NewLeaderWorkerSetProvider(lws, index)
	pgName := lwsProvider.GetName()

	lwsPg, err := pg.pgLister.PodGroups(lws.Namespace).Get(pgName)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("failed to get podgroup %s in local cache for LeaderWorkerSet %s/%s: %v",
				pgName, lws.Namespace, lws.Name, err)
			return err
		}

		// create new podgroup
		lwsPg = &scheduling.PodGroup{
			ObjectMeta: lwsProvider.GetObjectMeta(),
			Spec:       lwsProvider.GetSpec(),
		}

		if _, err = pg.vcClient.SchedulingV1beta1().PodGroups(lws.Namespace).Create(context.TODO(), lwsPg, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}

		return nil
	}

	// update podgroup
	pgShouldUpdate := false

	// inherit annotations change
	if !reflect.DeepEqual(lwsPg.Annotations, lws.Annotations) {
		lwsPg.Annotations = lws.Annotations
		pgShouldUpdate = true
	}

	// inherit labels change
	updatedLabels := make(map[string]string)
	for k, v := range lwsPg.Labels {
		updatedLabels[k] = v
	}
	// If there are same keys, take the value in lws as the standard
	for k, v := range lws.Labels {
		updatedLabels[k] = v
	}
	if !reflect.DeepEqual(lwsPg.Labels, updatedLabels) {
		lwsPg.Labels = updatedLabels
		pgShouldUpdate = true
	}

	// check whether podgroup min resources changed
	newMinResources := lwsProvider.GetMinResources()
	if !quotav1.Equals(*lwsPg.Spec.MinResources, *newMinResources) {
		lwsPg.Spec.MinResources = newMinResources
		pgShouldUpdate = true
	}

	if !pgShouldUpdate {
		return nil
	}

	if _, err = pg.vcClient.SchedulingV1beta1().PodGroups(lws.Namespace).Update(context.TODO(), lwsPg, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}
