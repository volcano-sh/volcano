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
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/util"
)

const (
	controllerRevisionHashLabelKey = "controller-revision-hash"
)

type requestKind string

const (
	podKind         requestKind = "Pod"
	replicaSetKind  requestKind = "ReplicaSet"
	statefulSetKind requestKind = "StatefulSet"
)

type pgRequest struct {
	kind      requestKind
	namespace string
	name      string
	uid       types.UID // Only Pod keys retain a UID; workload keys reconcile the current object.
}

type workloadState struct {
	kind      requestKind
	namespace string
	name      string
	uid       types.UID
	selector  *metav1.LabelSelector
	revision  string
	replicas  *int32
}

type podAnnotationPatch struct {
	Metadata struct {
		UID         types.UID         `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
}

func (pg *pgcontroller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	if owner := metav1.GetControllerOf(pod); owner != nil {
		switch requestKind(owner.Kind) {
		case replicaSetKind:
			if pg.rsInformer != nil {
				pg.queue.Add(pgRequest{kind: replicaSetKind, namespace: pod.Namespace, name: owner.Name})
				return
			}
		case statefulSetKind:
			if pg.stsInformer != nil {
				pg.queue.Add(pgRequest{kind: statefulSetKind, namespace: pod.Namespace, name: owner.Name})
				return
			}
		}
	}
	pg.queue.Add(pgRequest{kind: podKind, namespace: pod.Namespace, name: pod.Name, uid: pod.UID})
}

func (pg *pgcontroller) addReplicaSet(obj interface{}) {
	rs, ok := obj.(*appsv1.ReplicaSet)
	if !ok {
		klog.Errorf("Failed to convert %v to appsv1.ReplicaSet", obj)
		return
	}

	pg.queue.Add(pgRequest{kind: replicaSetKind, namespace: rs.Namespace, name: rs.Name})
}

func (pg *pgcontroller) updateReplicaSet(oldObj, newObj interface{}) {
	pg.addReplicaSet(newObj)
}

func (pg *pgcontroller) addStatefulSet(obj interface{}) {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		klog.Errorf("Failed to convert %v to appsv1.StatefulSet", obj)
		return
	}

	pg.queue.Add(pgRequest{kind: statefulSetKind, namespace: sts.Namespace, name: sts.Name})
}

func (pg *pgcontroller) updateStatefulSet(oldObj, newObj interface{}) {
	pg.addStatefulSet(newObj)
}

func (pg *pgcontroller) deletePodGroup(obj interface{}) {
	deleted, ok := obj.(*scheduling.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		deleted, ok = tombstone.Obj.(*scheduling.PodGroup)
		if !ok {
			return
		}
	}
	owner := metav1.GetControllerOf(deleted)
	if owner == nil {
		return
	}
	switch requestKind(owner.Kind) {
	case replicaSetKind, statefulSetKind:
		pg.queue.Add(pgRequest{kind: requestKind(owner.Kind), namespace: deleted.Namespace, name: owner.Name})
	case podKind:
		pg.queue.Add(pgRequest{kind: podKind, namespace: deleted.Namespace, name: owner.Name, uid: owner.UID})
	}
}

func (pg *pgcontroller) reconcileReplicaSet(rs *appsv1.ReplicaSet) error {
	return pg.reconcileWorkload(workloadState{
		kind: replicaSetKind, namespace: rs.Namespace, name: rs.Name, uid: rs.UID,
		selector: rs.Spec.Selector, replicas: rs.Spec.Replicas,
	})
}

func (pg *pgcontroller) reconcileStatefulSet(sts *appsv1.StatefulSet) error {
	return pg.reconcileWorkload(workloadState{
		kind: statefulSetKind, namespace: sts.Namespace, name: sts.Name, uid: sts.UID,
		selector: sts.Spec.Selector, revision: sts.Status.UpdateRevision, replicas: sts.Spec.Replicas,
	})
}

func (pg *pgcontroller) reconcileWorkload(workload workloadState) error {
	// Kubernetes defaults nil replicas to one. A zero-value fake object follows the same rule.
	if workload.replicas != nil && *workload.replicas == 0 {
		return pg.deleteManagedPodGroup(workload)
	}
	if workload.selector == nil {
		return fmt.Errorf("%s %s/%s has no selector", workload.kind, workload.namespace, workload.name)
	}
	selector, err := metav1.LabelSelectorAsSelector(workload.selector)
	if err != nil {
		return err
	}
	pods, err := pg.podLister.Pods(workload.namespace).List(selector)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		owner := metav1.GetControllerOf(pod)
		if owner == nil || requestKind(owner.Kind) != workload.kind || owner.Name != workload.name || owner.UID != workload.uid {
			continue
		}
		if workload.revision != "" && pod.Labels[controllerRevisionHashLabelKey] != workload.revision {
			continue
		}
		if !slices.Contains(pg.schedulerNames, pod.Spec.SchedulerName) {
			continue
		}
		name := helpers.GeneratePodgroupName(pod)
		if annotation := pod.Annotations[scheduling.KubeGroupNameAnnotationKey]; annotation != "" && annotation != name {
			continue
		}
		return pg.reconcileManagedPodGroup(pod, workload)
	}
	return nil // A later Pod Add enqueues this owner key.
}

func (pg *pgcontroller) deleteManagedPodGroup(workload workloadState) error {
	name := batchv1alpha1.PodgroupNamePrefix + string(workload.uid)
	client := pg.vcClient.SchedulingV1beta1().PodGroups(workload.namespace)
	group, err := client.Get(context.TODO(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownedByWorkload(group, workload) {
		return nil
	}
	return client.Delete(context.TODO(), name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &group.UID}})
}

func ownedByWorkload(group *scheduling.PodGroup, workload workloadState) bool {
	owner := metav1.GetControllerOf(group)
	return owner != nil && requestKind(owner.Kind) == workload.kind && owner.Name == workload.name && owner.UID == workload.uid
}

func (pg *pgcontroller) reconcileManagedPodGroup(pod *v1.Pod, workload workloadState) error {
	name := helpers.GeneratePodgroupName(pod)
	client := pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace)
	group, err := client.Get(context.TODO(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.Create(context.TODO(), pg.buildPodGroupFromPod(pod, name), metav1.CreateOptions{})
		if err != nil {
			return err
		}
		return pg.updatePodAnnotations(pod, name)
	}
	if err != nil {
		return err
	}
	if !ownedByWorkload(group, workload) {
		return nil
	}
	if group.DeletionTimestamp != nil {
		return fmt.Errorf("PodGroup %s/%s is terminating", group.Namespace, group.Name)
	}
	updated := group.DeepCopy()
	if pg.shouldUpdateExistingPodGroup(updated, pod) {
		if _, err := client.Update(context.TODO(), updated, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return pg.updatePodAnnotations(pod, name)
}

func (pg *pgcontroller) updatePodAnnotations(pod *v1.Pod, pgName string) error {
	if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] == "" {
		patch := podAnnotationPatch{}
		patch.Metadata.UID = pod.UID
		patch.Metadata.Annotations = map[string]string{scheduling.KubeGroupNameAnnotationKey: pgName}

		patchBytes, err := json.Marshal(patch)
		if err != nil {
			klog.Errorf("Failed to json.Marshal pod annotation: %v", err)
			return err
		}

		if _, err := pg.kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			klog.Errorf("Failed to update pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
			return err
		}
		klog.V(4).Infof("Bound Pod <%s/%s> to PodGroup <%s/%s>", pod.Namespace, pod.Name, pod.Namespace, pgName)
	} else {
		if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] != pgName {
			klog.Errorf("normal pod %s/%s annotations %s value is not %s, but %s", pod.Namespace, pod.Name,
				scheduling.KubeGroupNameAnnotationKey, pgName, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
		}
	}
	return nil
}

func (pg *pgcontroller) getAnnotationsFromUpperRes(pod *v1.Pod) map[string]string {
	var annotations = make(map[string]string)

	for _, reference := range pod.OwnerReferences {
		if reference.Kind != "" && reference.Name != "" {
			tmp := make(map[string]string)
			switch reference.Kind {
			case "ReplicaSet":
				rs, err := pg.kubeClient.AppsV1().ReplicaSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmp = rs.Annotations
			case "DaemonSet":
				ds, err := pg.kubeClient.AppsV1().DaemonSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmp = ds.Annotations
			case "StatefulSet":
				ss, err := pg.kubeClient.AppsV1().StatefulSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmp = ss.Annotations
			case "Job":
				job, err := pg.kubeClient.BatchV1().Jobs(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmp = job.Annotations
			}

			for k, v := range tmp {
				if _, ok := annotations[k]; !ok {
					annotations[k] = v
				}
			}
		}
	}

	return annotations
}

func (pg *pgcontroller) getMinMemberFromUpperRes(upperAnnotations map[string]string, namespance, name string) int32 {
	minMember := int32(1)

	if minMemberAnno, ok := upperAnnotations[scheduling.VolcanoGroupMinMemberAnnotationKey]; ok {
		minMemberFromAnno, err := strconv.ParseInt(minMemberAnno, 10, 32)
		if err != nil {
			klog.Errorf("Failed to convert minMemberAnnotation of Pod owners <%s/%s> into number: %v, minMember remains as 1",
				namespance, name, err)
			return minMember
		}
		if minMemberFromAnno < 0 {
			klog.Errorf("minMemberAnnotation %d is not positive, minMember remains as 1", minMemberFromAnno)
			return minMember
		}
		minMember = int32(minMemberFromAnno)
	}

	return minMember
}

// Inherit annotations from upper resources.
func (pg *pgcontroller) inheritUpperAnnotations(upperAnnotations map[string]string, obj *scheduling.PodGroup) {
	if pg.inheritOwnerAnnotations {
		for k, v := range upperAnnotations {
			if strings.HasPrefix(k, scheduling.AnnotationPrefix) {
				obj.Annotations[k] = v
			}
		}
	}
}

func (pg *pgcontroller) createNormalPodPGIfNotExist(pod *v1.Pod) error {
	pgName := helpers.GeneratePodgroupName(pod)

	if _, err := pg.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		podGroup := pg.buildPodGroupFromPod(pod, pgName)
		if _, err := pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Create(context.TODO(), podGroup, metav1.CreateOptions{}); err != nil {
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

// When statefulSet is updated, its associated pod template may change.
// In such cases, we need to update the corresponding PodGroup simultaneously.
func (pg *pgcontroller) createOrUpdateNormalPodPG(pod *v1.Pod) error {
	pgName := helpers.GeneratePodgroupName(pod)

	if podGroup, err := pg.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		newPodGroup := pg.buildPodGroupFromPod(pod, pgName)
		if _, err := pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Create(context.TODO(), newPodGroup, metav1.CreateOptions{}); err != nil {
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
	} else {
		podGroupToUpdate := podGroup.DeepCopy()
		needUpdate := pg.shouldUpdateExistingPodGroup(podGroupToUpdate, pod)
		if needUpdate {
			_, err = pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Update(context.TODO(), podGroupToUpdate, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("Failed to update PodGroup <%s/%s>: %v", pod.Namespace, pgName, err)
				return err
			}
			klog.V(4).Infof("PodGroup <%s/%s> updated for Pod <%s/%s>", pod.Namespace, pgName, pod.Namespace, pod.Name)
		}
	}

	return pg.updatePodAnnotations(pod, pgName)
}

func (pg *pgcontroller) buildPodGroupFromPod(pod *v1.Pod, pgName string) *scheduling.PodGroup {
	var minMember = int32(1)
	var ownerAnnotations = make(map[string]string)
	if pg.inheritOwnerAnnotations {
		ownerAnnotations = pg.getAnnotationsFromUpperRes(pod)
		minMember = pg.getMinMemberFromUpperRes(ownerAnnotations, pod.Namespace, pod.Name)
	}
	minResources := util.CalTaskRequests(pod, minMember)
	obj := &scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       pod.Namespace,
			Name:            pgName,
			OwnerReferences: newPGOwnerReferences(pod),
			Annotations:     map[string]string{},
			Labels:          map[string]string{},
		},
		Spec: scheduling.PodGroupSpec{
			MinMember:         minMember,
			PriorityClassName: pod.Spec.PriorityClassName,
			MinResources:      &minResources,
		},
		Status: scheduling.PodGroupStatus{
			Phase: scheduling.PodGroupPending,
		},
	}

	pg.inheritUpperAnnotations(ownerAnnotations, obj)
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

	// Parse and set NetworkTopology from Pod annotations
	if networkTopology := parseNetworkTopologyFromPod(pod); networkTopology != nil {
		obj.Spec.NetworkTopology = networkTopology

		highestTier := 1 // default value
		if networkTopology.HighestTierAllowed != nil {
			highestTier = *networkTopology.HighestTierAllowed
		}
		klog.V(4).Infof("Set NetworkTopology for PodGroup %s/%s: mode:%s, highestTier:%d",
			obj.Namespace, obj.Name, networkTopology.Mode, highestTier)
	}

	return obj
}

// parseNetworkTopologyFromPod extracts NetworkTopology configuration from Pod annotations
func parseNetworkTopologyFromPod(pod *v1.Pod) *scheduling.NetworkTopologySpec {
	annotations := pod.Annotations
	if annotations == nil {
		return nil
	}

	// Check if any NetworkTopology annotations are present
	modeStr, modeExists := annotations[topologyv1alpha1.NetworkTopologyModeAnnotationKey]
	tierStr, tierExists := annotations[topologyv1alpha1.NetworkTopologyHighestTierAnnotationKey]

	if !modeExists && !tierExists {
		return nil
	}

	nt := &scheduling.NetworkTopologySpec{
		Mode: scheduling.HardNetworkTopologyMode,
	}

	// Parse mode
	if modeExists {
		mode := scheduling.NetworkTopologyMode(strings.ToLower(modeStr))
		if mode == scheduling.HardNetworkTopologyMode || mode == scheduling.SoftNetworkTopologyMode {
			nt.Mode = mode
		} else {
			klog.Warningf("Invalid network topology mode %q in pod %s/%s, using default 'hard' mode", modeStr, pod.Namespace, pod.Name)
		}
	}

	// Parse highest tier allowed
	if tierExists {
		if tier, err := strconv.Atoi(tierStr); err == nil {
			nt.HighestTierAllowed = &tier
		} else {
			klog.Warningf("Invalid network topology highest tier %s in pod %s/%s: %v", tierStr, pod.Namespace, pod.Name, err)
		}
	}

	return nt
}

func (pg *pgcontroller) shouldUpdateExistingPodGroup(podGroup *scheduling.PodGroup, pod *v1.Pod) bool {
	isUpdated := false

	newPodGroup := pg.buildPodGroupFromPod(pod, podGroup.Name)
	if !reflect.DeepEqual(newPodGroup.Spec, podGroup.Spec) {
		podGroup.Spec = newPodGroup.Spec
		isUpdated = true
	}

	if !reflect.DeepEqual(newPodGroup.Labels, podGroup.Labels) {
		podGroup.Labels = newPodGroup.Labels
		isUpdated = true
	}

	if !reflect.DeepEqual(newPodGroup.Annotations, podGroup.Annotations) {
		podGroup.Annotations = newPodGroup.Annotations
		isUpdated = true
	}

	return isUpdated
}

func newPGOwnerReferences(pod *v1.Pod) []metav1.OwnerReference {
	if len(pod.OwnerReferences) != 0 {
		for _, ownerReference := range pod.OwnerReferences {
			if ownerReference.Controller != nil && *ownerReference.Controller {
				return pod.OwnerReferences
			}
		}
	}

	gvk := schema.GroupVersionKind{
		Group:   v1.SchemeGroupVersion.Group,
		Version: v1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}
	ref := metav1.NewControllerRef(pod, gvk)
	return []metav1.OwnerReference{*ref}
}
