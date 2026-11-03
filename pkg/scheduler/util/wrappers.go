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

package util

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// PodWrapper wraps a Pod so that a test can set one field at a time instead of
// passing a long positional argument list to BuildPod. Every setter returns the
// wrapper, so a pod is built as a chain and passed to the test with Obj:
//
//	pod := MakePod("ns", "p").
//		Phase(v1.PodPending).
//		Resource(api.BuildResourceList("1", "1G")).
//		PodGroup("pg").
//		Obj()
type PodWrapper struct {
	v1.Pod
}

// MakePod returns a PodWrapper with the same defaults as BuildPod: a UID of
// "<namespace>-<name>" (the scheduler keys tasks by UID), one container to hold
// requests, and the pod group annotation key present but empty.
func MakePod(namespace, name string) *PodWrapper {
	return &PodWrapper{
		Pod: v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       types.UID(namespace + "-" + name),
				Name:      name,
				Namespace: namespace,
				Annotations: map[string]string{
					schedulingv1beta1.KubeGroupNameAnnotationKey: "",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{}},
			},
		},
	}
}

// Obj returns the wrapped pod.
func (p *PodWrapper) Obj() *v1.Pod {
	return &p.Pod
}

// Node sets the node the pod is already assigned to.
func (p *PodWrapper) Node(nodeName string) *PodWrapper {
	p.Spec.NodeName = nodeName
	return p
}

// Phase sets the pod phase.
func (p *PodWrapper) Phase(phase v1.PodPhase) *PodWrapper {
	p.Status.Phase = phase
	return p
}

// Resource sets the requests of the pod's first container.
func (p *PodWrapper) Resource(req v1.ResourceList) *PodWrapper {
	if len(p.Spec.Containers) == 0 {
		p.Spec.Containers = []v1.Container{{}}
	}
	p.Spec.Containers[0].Resources.Requests = req
	return p
}

// PodGroup sets the pod group the pod belongs to.
func (p *PodWrapper) PodGroup(name string) *PodWrapper {
	return p.Annotation(schedulingv1beta1.KubeGroupNameAnnotationKey, name)
}

// Labels replaces the pod labels.
func (p *PodWrapper) Labels(labels map[string]string) *PodWrapper {
	p.ObjectMeta.Labels = labels
	return p
}

// Label adds one label, creating the map if needed.
func (p *PodWrapper) Label(key, value string) *PodWrapper {
	if p.ObjectMeta.Labels == nil {
		p.ObjectMeta.Labels = map[string]string{}
	}
	p.ObjectMeta.Labels[key] = value
	return p
}

// Annotations replaces the pod annotations. A nil map is replaced by an empty
// one so that a later Annotation call cannot write to a nil map.
func (p *PodWrapper) Annotations(annotations map[string]string) *PodWrapper {
	if annotations == nil {
		annotations = map[string]string{}
	}
	p.ObjectMeta.Annotations = annotations
	return p
}

// Annotation adds one annotation, creating the map if needed.
func (p *PodWrapper) Annotation(key, value string) *PodWrapper {
	if p.ObjectMeta.Annotations == nil {
		p.ObjectMeta.Annotations = map[string]string{}
	}
	p.ObjectMeta.Annotations[key] = value
	return p
}

// NodeSelector sets the pod's node selector.
func (p *PodWrapper) NodeSelector(selector map[string]string) *PodWrapper {
	p.Spec.NodeSelector = selector
	return p
}

// Priority sets the pod priority.
func (p *PodWrapper) Priority(priority *int32) *PodWrapper {
	p.Spec.Priority = priority
	return p
}

// Affinity sets the pod affinity and anti-affinity rules.
func (p *PodWrapper) Affinity(affinity *v1.Affinity) *PodWrapper {
	p.Spec.Affinity = affinity
	return p
}

// Tolerations sets the pod tolerations.
func (p *PodWrapper) Tolerations(tolerations []v1.Toleration) *PodWrapper {
	p.Spec.Tolerations = tolerations
	return p
}

// TopologySpreadConstraints sets the pod topology spread constraints.
func (p *PodWrapper) TopologySpreadConstraints(constraints []v1.TopologySpreadConstraint) *PodWrapper {
	p.Spec.TopologySpreadConstraints = constraints
	return p
}

// PreemptionPolicy sets the pod preemption policy.
func (p *PodWrapper) PreemptionPolicy(policy v1.PreemptionPolicy) *PodWrapper {
	p.Spec.PreemptionPolicy = &policy
	return p
}

// NominatedNodeName sets the node the scheduler has nominated for the pod.
func (p *PodWrapper) NominatedNodeName(nodeName string) *PodWrapper {
	p.Status.NominatedNodeName = nodeName
	return p
}

// RestartPolicy sets the pod restart policy.
func (p *PodWrapper) RestartPolicy(policy v1.RestartPolicy) *PodWrapper {
	p.Spec.RestartPolicy = policy
	return p
}

// SchedulerName sets the scheduler that should handle the pod.
func (p *PodWrapper) SchedulerName(name string) *PodWrapper {
	p.Spec.SchedulerName = name
	return p
}

// PodGroupWrapper wraps a PodGroup, following the same one-setter-per-field
// pattern as PodWrapper.
type PodGroupWrapper struct {
	schedulingv1beta1.PodGroup
}

// MakePodGroup returns a PodGroupWrapper for the given namespace and name.
func MakePodGroup(namespace, name string) *PodGroupWrapper {
	return &PodGroupWrapper{
		PodGroup: schedulingv1beta1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

// Obj returns the wrapped pod group.
func (p *PodGroupWrapper) Obj() *schedulingv1beta1.PodGroup {
	return &p.PodGroup
}

// Queue sets the queue the pod group belongs to.
func (p *PodGroupWrapper) Queue(queue string) *PodGroupWrapper {
	p.Spec.Queue = queue
	return p
}

// MinMember sets the minimum number of tasks that must be scheduled.
func (p *PodGroupWrapper) MinMember(minMember int32) *PodGroupWrapper {
	p.Spec.MinMember = minMember
	return p
}

// MinTaskMember sets the minimum number of members per task role.
func (p *PodGroupWrapper) MinTaskMember(minTaskMember map[string]int32) *PodGroupWrapper {
	p.Spec.MinTaskMember = minTaskMember
	return p
}

// MinResources sets the minimum resources the pod group needs.
func (p *PodGroupWrapper) MinResources(minResources v1.ResourceList) *PodGroupWrapper {
	p.Spec.MinResources = &minResources
	return p
}

// Phase sets the pod group phase.
func (p *PodGroupWrapper) Phase(phase schedulingv1beta1.PodGroupPhase) *PodGroupWrapper {
	p.Status.Phase = phase
	return p
}

// PriorityClassName sets the priority class of the pod group.
func (p *PodGroupWrapper) PriorityClassName(name string) *PodGroupWrapper {
	p.Spec.PriorityClassName = name
	return p
}

// Annotations replaces the pod group annotations.
func (p *PodGroupWrapper) Annotations(annotations map[string]string) *PodGroupWrapper {
	p.ObjectMeta.Annotations = annotations
	return p
}

// Annotation adds one annotation, creating the map if needed.
func (p *PodGroupWrapper) Annotation(key, value string) *PodGroupWrapper {
	if p.ObjectMeta.Annotations == nil {
		p.ObjectMeta.Annotations = map[string]string{}
	}
	p.ObjectMeta.Annotations[key] = value
	return p
}

// SubGroupPolicy sets the pod group's sub group policies.
func (p *PodGroupWrapper) SubGroupPolicy(policies []schedulingv1beta1.SubGroupPolicySpec) *PodGroupWrapper {
	p.Spec.SubGroupPolicy = policies
	return p
}
