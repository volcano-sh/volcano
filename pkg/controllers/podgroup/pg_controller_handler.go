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

type podRequest struct {
	podName      string
	podNamespace string
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

	req := podRequest{
		podName:      pod.Name,
		podNamespace: pod.Namespace,
	}

	pg.queue.Add(req)
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
			klog.V(4).Infof("Try to create or update podgroup for pod %s", klog.KObj(&pod))
			if !slices.Contains(pg.schedulerNames, pod.Spec.SchedulerName) {
				klog.V(4).Infof("Pod %s field SchedulerName is not matched", klog.KObj(&pod))
				return
			}
			// If the pod is already associated with a podgroup, skip creating a new one.
			if pgName := pod.Annotations[scheduling.KubeGroupNameAnnotationKey]; pgName != "" {
				klog.V(4).Infof("Pod %s is already associated with a podgroup %s", klog.KObj(&pod), pgName)
				return
			}
			// In the upgrade scenario, need to synchronize and update the podgroup-related information.
			// For example, if you update `scheduling.volcano.sh/group-min-member: "2"`, the podgroup's `minMember` needs to be updated to 2.
			err := pg.createOrUpdateNormalPodPG(&pod)
			if err != nil {
				klog.Errorf("Failed to create or update PodGroup for pod %s: %v", klog.KObj(&pod), err)
			}
		}
	}
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

	if *sts.Spec.Replicas == 0 {
		pgName := batchv1alpha1.PodgroupNamePrefix + string(sts.UID)
		err := pg.vcClient.SchedulingV1beta1().PodGroups(sts.Namespace).Delete(context.TODO(), pgName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete PodGroup <%s/%s>: %v", sts.Namespace, pgName, err)
		}
	}

	// In the rolling upgrade scenario, the addStatefulSet(replicas=0) event may be received before
	// the updateStatefulSet(replicas=1) event, and after the addPod event for the new created pod.
	// In this event, need to create PodGroup for the pod.
	if *sts.Spec.Replicas > 0 {
		matchLabels := make(map[string]string, len(sts.Spec.Selector.MatchLabels)+1)
		for k, v := range sts.Spec.Selector.MatchLabels {
			matchLabels[k] = v
		}
		matchLabels[controllerRevisionHashLabelKey] = sts.Status.UpdateRevision
		selector := metav1.LabelSelector{MatchLabels: matchLabels}
		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			klog.Errorf("Failed to convert label selector for StatefulSet <%s/%s>: %v", sts.Namespace, sts.Name, err)
			return
		}
		pods, err := pg.podInformer.Lister().List(labelSelector)
		if err != nil {
			klog.Errorf("Failed to list pods for StatefulSet <%s/%s>: %v", sts.Namespace, sts.Name, err)
			return
		}
		if len(pods) > 0 {
			pod := pods[0]
			klog.V(4).Infof("Try to create or update podgroup for pod %s/%s when statefulset add or update", pod.Namespace, pod.Name)
			if !slices.Contains(pg.schedulerNames, pod.Spec.SchedulerName) {
				klog.V(4).Infof("Pod %s field SchedulerName is not matched", klog.KObj(pod))
				return
			}

			// If the pod is already associated with a podgroup, skip creating a new one. This scenario is applicable to LeaderWorkerSet,
			// which will create podgroups by itself, and Volcano does not need to create a podgroup for statefulset.
			if pgName := pod.Annotations[scheduling.KubeGroupNameAnnotationKey]; pgName != "" {
				klog.V(4).Infof("Pod %s is already associated with a podgroup %s", klog.KObj(pod), pgName)
				return
			}

			err := pg.createOrUpdateNormalPodPG(pod)
			if err != nil {
				klog.Errorf("Failed to create or update PodGroup for pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
			}
		}
	}
}

func (pg *pgcontroller) updateStatefulSet(oldObj, newObj interface{}) {
	pg.addStatefulSet(newObj)
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
		klog.V(4).Infof("Bound Pod <%s/%s> to PodGroup <%s/%s>", pod.Namespace, pod.Name, pod.Namespace, pgName)
	} else {
		if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] != pgName {
			klog.Errorf("normal pod %s/%s annotations %s value is not %s, but %s", pod.Namespace, pod.Name,
				scheduling.KubeGroupNameAnnotationKey, pgName, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
		}
	}
	return nil
}

func (pg *pgcontroller) getAnnotationsLabelsFromUpperRes(pod *v1.Pod) (annotations, labels map[string]string) {
	annotations = make(map[string]string)
	labels = make(map[string]string)

	for _, reference := range pod.OwnerReferences {
		if reference.Kind != "" && reference.Name != "" {
			tmpAnnotations := make(map[string]string)
			tmpLabels := make(map[string]string)
			switch reference.Kind {
			case "ReplicaSet":
				rs, err := pg.kubeClient.AppsV1().ReplicaSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmpAnnotations = rs.Annotations
				tmpLabels = rs.Labels
			case "DaemonSet":
				ds, err := pg.kubeClient.AppsV1().DaemonSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmpAnnotations = ds.Annotations
				tmpLabels = ds.Labels
			case "StatefulSet":
				ss, err := pg.kubeClient.AppsV1().StatefulSets(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmpAnnotations = ss.Annotations
				tmpLabels = ss.Labels
			case "Job":
				job, err := pg.kubeClient.BatchV1().Jobs(pod.Namespace).Get(context.TODO(), reference.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get upper %s for Pod <%s/%s>: %v", reference.Kind, pod.Namespace, reference.Name, err)
					continue
				}
				tmpAnnotations = job.Annotations
				tmpLabels = job.Labels
			}

			for k, v := range tmpAnnotations {
				if _, ok := annotations[k]; !ok {
					annotations[k] = v
				}
			}
			for k, v := range tmpLabels {
				if _, ok := labels[k]; !ok {
					labels[k] = v
				}
			}
		}
	}

	return
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

func matchPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// Inherit annotations from upper resources.
func (pg *pgcontroller) inheritUpperAnnotations(upperAnnotations map[string]string, obj *scheduling.PodGroup) {
	if pg.InheritOwnerAnnotations {
		for k, v := range upperAnnotations {
			if matchPrefix(k, pg.InheritOwnerAnnotationPrefixes) {
				obj.Annotations[k] = v
			}
		}
	}
}

func (pg *pgcontroller) inheritOriginPodAnnotations(podAnnotations map[string]string, obj *scheduling.PodGroup) {
	if pg.InheritPodAnnotations {
		for k, v := range podAnnotations {
			if matchPrefix(k, pg.InheritPodAnnotationPrefixes) {
				obj.Annotations[k] = v
			}
		}
	}
}

func (pg *pgcontroller) inheritUpperPodLabels(labels map[string]string, obj *scheduling.PodGroup) {
	if pg.InheritOwnerLabels {
		for k, v := range labels {
			if matchPrefix(k, pg.InheritOwnerLabelPrefixes) {
				obj.Labels[k] = v
			}
		}
	}
}

func (pg *pgcontroller) inheritOriginPodLabels(labels map[string]string, obj *scheduling.PodGroup) {
	if pg.InheritPodLabels {
		for k, v := range labels {
			if matchPrefix(k, pg.InheritPodLabelPrefixes) {
				obj.Labels[k] = v
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
	var ownerLabels = make(map[string]string)
	if pg.InheritOwnerAnnotations || pg.InheritOwnerLabels {
		ownerAnnotations, ownerLabels = pg.getAnnotationsLabelsFromUpperRes(pod)
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
	pg.inheritOriginPodAnnotations(pod.Annotations, obj)

	pg.inheritUpperPodLabels(ownerLabels, obj)
	pg.inheritOriginPodLabels(pod.Labels, obj)

	// Individual annotations on pods would overwrite annotations inherited from upper resources.
	if queueName, ok := pod.Annotations[scheduling.QueueNameAnnotationKey]; ok {
		obj.Spec.Queue = queueName
	}

	// NOTE: scheduling.PodPreemptable, scheduling.CooldownTime, scheduling.RevocableZone
	// scheduling.JDBMinAvailable, scheduling.JDBMaxUnavailable are with volcano.sh/ prefix,
	// and would be inherited from pod annotations/labels by default.

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
