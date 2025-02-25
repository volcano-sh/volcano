/*
Copyright 2025 The Volcano Authors.

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/utils/ptr"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
	"volcano.sh/apis/pkg/apis/helpers"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/controllers/util"
)

// Provider is an interface for providing necessary information of PodGroup.
type Provider interface {
	// GetOwnerReferences returns the owner references for the PodGroup.
	// The owner reference indicates which resource owns and manages this PodGroup.
	GetOwnerReferences() []metav1.OwnerReference

	// GetName returns the name for the PodGroup.
	GetName() string

	// GetMinResources returns the minimum resources required by the PodGroup.
	GetMinResources() *v1.ResourceList

	// GetSpec returns the spec of the PodGroup.
	GetSpec() scheduling.PodGroupSpec

	// GetObjectMeta returns the metadata of the PodGroup.
	GetObjectMeta() metav1.ObjectMeta
}

func newControllerRef(obj metav1.Object, gvk schema.GroupVersionKind) []metav1.OwnerReference {
	ref := metav1.NewControllerRef(obj, gvk)
	return []metav1.OwnerReference{*ref}
}

type PodProvider struct {
	pod *v1.Pod
}

func NewPodProvider(pod *v1.Pod) Provider {
	return &PodProvider{pod: pod}
}

func (p *PodProvider) GetOwnerReferences() []metav1.OwnerReference {
	if len(p.pod.OwnerReferences) != 0 {
		for _, ownerReference := range p.pod.OwnerReferences {
			if ownerReference.Controller != nil && *ownerReference.Controller {
				return p.pod.OwnerReferences
			}
		}
	}

	return newControllerRef(p.pod, schema.GroupVersionKind{
		Group:   v1.SchemeGroupVersion.Group,
		Version: v1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	})
}

func (p *PodProvider) GetName() string {
	return helpers.GeneratePodgroupName(p.pod)
}

func (p *PodProvider) GetMinResources() *v1.ResourceList {
	return util.GetPodQuotaUsage(p.pod)
}

func (p *PodProvider) GetSpec() scheduling.PodGroupSpec {
	return scheduling.PodGroupSpec{
		MinMember:         1,
		PriorityClassName: p.pod.Spec.PriorityClassName,
		MinResources:      p.GetMinResources(),
	}
}

func (p *PodProvider) GetObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       p.pod.Namespace,
		Name:            p.GetName(),
		OwnerReferences: p.GetOwnerReferences(),
		Annotations:     map[string]string{},
		Labels:          map[string]string{},
	}
}

const (
	LeaderWorkerSetPodGroupNameFmt = "%s-%d"
	OwnerApiVersionField           = "metadata.ownerReferences.apiVersion"
	OwnerKindField                 = "metadata.ownerReferences"
	LeaderWorkerSetKind            = "LeaderWorkerSet"
	LeaderWorkerSetApiVersion      = "leaderworkerset.x-k8s.io/v1"
)

type LeaderWorkerSetProvider struct {
	lws   *lwsv1.LeaderWorkerSet
	index int
}

func NewLeaderWorkerSetProvider(lws *lwsv1.LeaderWorkerSet, index int) Provider {
	return &LeaderWorkerSetProvider{
		lws:   lws,
		index: index,
	}
}

func (l *LeaderWorkerSetProvider) GetOwnerReferences() []metav1.OwnerReference {
	return newControllerRef(l.lws, schema.GroupVersionKind{
		Group:   lwsv1.SchemeGroupVersion.Group,
		Version: lwsv1.SchemeGroupVersion.Version,
		Kind:    LeaderWorkerSetKind,
	})
}

func (l *LeaderWorkerSetProvider) GetName() string {
	return fmt.Sprintf(LeaderWorkerSetPodGroupNameFmt, l.lws.Name, l.index)
}

func (l *LeaderWorkerSetProvider) GetMinResources() *v1.ResourceList {
	res := v1.ResourceList{}

	// calculate leader min resources
	leaderTemplate := l.lws.Spec.LeaderWorkerTemplate.LeaderTemplate
	// leaderTemplate can be nil, if leaderTemplate is nil, use workerTemplate instead
	if leaderTemplate == nil {
		leaderTemplate = &l.lws.Spec.LeaderWorkerTemplate.WorkerTemplate
	}
	res = quotav1.Add(res, *util.GetPodQuotaUsage(&v1.Pod{Spec: leaderTemplate.Spec}))

	// calculate workers min resources
	for i := int32(0); i < ptr.Deref(l.lws.Spec.LeaderWorkerTemplate.Size, 1)-1; i++ {
		res = quotav1.Add(res, *util.GetPodQuotaUsage(&v1.Pod{Spec: l.lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec}))
	}

	return &res
}

func (l *LeaderWorkerSetProvider) GetSpec() scheduling.PodGroupSpec {
	// Default startupPolicy is LeaderCreated, Leader and Workers Pods are scheduled together, so MinAvailable is set to size
	spec := scheduling.PodGroupSpec{
		MinMember:    *l.lws.Spec.LeaderWorkerTemplate.Size,
		MinResources: l.GetMinResources(),
	}

	// If the StartUpPolicy of lws is LeaderReady, set MinMember to 1 to allow the Leader Pod to be scheduled.
	// However, minResources should still be set to the minResources of the PodGroup (1 Leader + (size-1) Workers).
	// If the cluster resources are insufficient, scheduling the Leader Pod alone would be meaningless,
	// as at least one Worker will definitely not be able to be scheduled.
	if l.lws.Spec.StartupPolicy == lwsv1.LeaderReadyStartupPolicy {
		spec.MinMember = 1
	}

	if queueName, ok := l.lws.Annotations[scheduling.QueueNameAnnotationKey]; ok {
		spec.Queue = queueName
	}

	// TODO: implement task granularity network topology aware scheduling

	return spec
}

func (l *LeaderWorkerSetProvider) GetObjectMeta() metav1.ObjectMeta {
	meta := metav1.ObjectMeta{
		Namespace:       l.lws.Namespace,
		Name:            l.GetName(),
		OwnerReferences: l.GetOwnerReferences(),
		Annotations:     l.lws.Annotations,
		Labels:          l.lws.Labels,
	}
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels[lwsv1.SetNameLabelKey] = l.lws.Name
	return meta
}
