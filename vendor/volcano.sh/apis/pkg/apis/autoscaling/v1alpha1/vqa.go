/*
Copyright 2018 The Volcano Authors.

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

package v1alpha1

import (
	v2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VerticalQueueAutoscalerType string
type StepType string
type Condition string

const (
	ResourceVQARatio                   corev1.ResourceName         = "ratio"
	VerticalQueueAutoscalerTidalType   VerticalQueueAutoscalerType = "tidal"
	VerticalQueueAutoscalerMetricsType VerticalQueueAutoscalerType = "metrics"
	EquivalentStepType                 StepType                    = "equivalent"
	EquiratioStepType                  StepType                    = "equiratio"
	AndCondition                       Condition                   = "and"
	OrCondition                        Condition                   = "or"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=verticalqueueautoscalers,shortName=vqa;vqas,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Last Scale",type=date,JSONPath=`.status.lastScaleTime`
// +kubebuilder:printcolumn:name="Last Successful",type=date,JSONPath=`.status.lastScaleSuccessTime`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="QUEUE",type=string,priority=1,JSONPath=`.spec.queue`

// VerticalQueueAutoscaler defines the volcano vertical queue autoscaler.
type VerticalQueueAutoscaler struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the volcano verticalqueueautoscaler, including tidal and metrics
	// +optional
	Spec VQASpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of the volcano verticalqueueautoscaler
	// +optional
	Status VQAStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// VQASpec describes how the VerticalQueueAutoscaler will look like and actually behavior.
type VQASpec struct {

	//Specifies the queue that will be used in the scheduler, "default" queue is used this leaves empty.
	Queue string `json:"queue,omitempty" protobuf:"bytes,1,opt,name=queue"`

	// Type is the behavior type of VQA.
	//+kubebuilder:validation:Enum=tidal;metrics
	Type VerticalQueueAutoscalerType `json:"type,omitempty" protobuf:"bytes,2,opt,name=type"`

	// TidalSpec define tidal autoscaling configuration
	// +optional
	TidalSpec TidalSpec `json:"tidalSpec,omitempty" protobuf:"bytes,3,opt,name=tidalSpec"`

	// TidalSpec define metrics autoscaling configuration
	// +optional
	MetricSpec []MetricSpec `json:"metricSpec,omitempty" protobuf:"bytes,4,opt,name=metricSpec"`
}

type VQAStatus struct {
	// LastScaleTime is the last time the VerticalQueueAutoscaler scaled the capacity of queue,
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty" protobuf:"bytes,1,opt,name=lastScaleTime"`

	// LastScaleSuccessTime is the last time the VerticalQueueAutoscaler scaled the capacity of queue Successful,
	// +optional
	LastScaleSuccessTime *metav1.Time `json:"lastScaleSuccessTime,omitempty" protobuf:"bytes,2,opt,name=lastScaleSuccessTime"`
}

type TidalSpec struct {
	// StartingDeadlineSeconds Optional deadline in seconds for starting the job if it misses scheduled
	// time for any reason.  Missed jobs executions will be counted as failed ones.
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty" protobuf:"varint,1,opt,name=startingDeadlineSeconds"`

	// Tidal define arrays of tidal configuration
	Tidal []Tidal `json:"tidal,omitempty" protobuf:"bytes,2,opt,name=tidal"`
}

type Tidal struct {
	// Schedule The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron
	Schedule string `json:"schedule,omitempty" protobuf:"bytes,1,opt,name=schedule"`
	// Weight  queue.spec.weight
	Weight int32 `json:"weight,omitempty" protobuf:"bytes,2,opt,name=weight"`
	// Capability  queue.spec.capability
	Capability corev1.ResourceList `json:"capability,omitempty" protobuf:"bytes,3,opt,name=capability"`
	// Guarantee  queue.spec.guarantee.resource
	Guarantee corev1.ResourceList `json:"guarantee,omitempty" protobuf:"bytes,4,opt,name=guarantee"`
}

type MetricSpec struct {
	// ScaleBehavior define scale behavior of metrics type
	ScaleBehavior ScaleBehavior `json:"scaleBehavior,omitempty" protobuf:"bytes,1,opt,name=scaleBehavior"`
	// ResourceLimit resources limit of queue
	ResourceLimit ResourceLimit `json:"resourceLimit,omitempty" protobuf:"bytes,2,opt,name=resourceLimit"`
	MetricRules   MetricRules   `json:"metricRules,omitempty" protobuf:"bytes,3,opt,name=metricRules"`
}

type ScaleBehavior struct {
	// StepType scale step type
	//+kubebuilder:validation:Enum=Delete;Orphan
	StepType StepType `json:"stepType,omitempty" protobuf:"bytes,1,opt,name=stepType"`
	// StepRatio ratio of scale step
	StepRatio resource.Quantity `json:"stepRatio,omitempty" protobuf:"bytes,2,opt,name=stepRatio"`
	// StepResources resources of scale step
	StepResources StepResources `json:"stepResources,omitempty" protobuf:"bytes,3,opt,name=stepResources"`
}

type StepResources struct {
	// Weight  queue.spec.weight, it's a increment
	Weight int32 `json:"weight,omitempty" protobuf:"bytes,1,opt,name=weight"`
	// Capability  queue.spec.capability, it's a increment
	Capability corev1.ResourceList `json:"capability,omitempty" protobuf:"bytes,2,opt,name=capability"`
	// Guarantee  queue.spec.guarantee.resource, it's a increment
	Guarantee corev1.ResourceList `json:"guarantee,omitempty" protobuf:"bytes,3,opt,name=guarantee"`
}

type ScaleLimit struct {
	// WeightLimit  limit of queue.spec.weight
	WeightLimit WeightLimit `json:"weightLimit,omitempty" protobuf:"bytes,1,opt,name=weightLimit"`
	// CapacityLimit  limit of queue.spec.capability
	CapacityLimit ResourceLimit `json:"capacityLimit,omitempty" protobuf:"bytes,2,opt,name=capacityLimit"`
	// GuaranteeLimit  limit of queue.spec.guarantee.resource
	GuaranteeLimit ResourceLimit `json:"guaratneeLimit,omitempty" protobuf:"bytes,3,opt,name=guaratneeLimit"`
}

type WeightLimit struct {
	// Min  minimum of queue.spec.weight
	Min int32 `json:"min,omitempty" protobuf:"bytes,1,opt,name=min"`
	// Max  maximum of queue.spec.weight
	Max int32 `json:"max,omitempty" protobuf:"bytes,2,opt,name=max"`
}

type ResourceLimit struct {
	// Min  minimum of queue.spec.capacity or guarantee
	Min corev1.ResourceList `json:"min,omitempty" protobuf:"bytes,1,opt,name=min"`
	// Max  maximum of queue.spec.capacity or guarantee
	Max corev1.ResourceList `json:"max,omitempty" protobuf:"bytes,2,opt,name=max"`
}

type MetricRules struct {
	// ScaleUp  rule of scale up
	ScaleUp ScaleRule `json:"scaleUp,omitempty" protobuf:"bytes,1,opt,name=scaleUp"`
	// ScaleUp  rule of scale down
	ScaleDown ScaleRule `json:"scaleDown,omitempty" protobuf:"bytes,2,opt,name=scaleDown"`
}

type ScaleRule struct {
	// StabilizationWindowSeconds is the number of seconds for which past recommendations should be
	// considered while scaling up or scaling down.
	// StabilizationWindowSeconds must be greater than or equal to zero and less than or equal to 3600 (one hour).
	// If not set, use the default values:
	// - For scale up: 0 (i.e. no stabilization is done).
	// - For scale down: 300 (i.e. the stabilization window is 300 seconds long).
	// +optional
	StabilizationWindowSeconds *int32         `json:"stabilizationWindowSeconds,omitempty" protobuf:"bytes,1,opt,name=stabilizationWindowSeconds"`
	Condition                  Condition      `json:"condition,omitempty" protobuf:"bytes,2,opt,name=condition"`
	MetricSources              []MetricSource `json:"metricSources,omitempty" protobuf:"bytes,3,opt,name=metricSources"`
}

type MetricSource struct {
	// PeriodSeconds specifies the window of time for which the policy should hold true.
	// PeriodSeconds must be greater than zero and less than or equal to 1800 (30 min).
	PeriodSeconds int32 `json:"periodSeconds,omitempty" protobuf:"bytes,1,opt,name=periodSeconds"`
	// Metric identifies the target metric by name and selector
	Metric v2.MetricIdentifier `json:"metric,omitempty" protobuf:"bytes,2,opt,name=metric"`
	// Target specifies the target value for the given metric
	Target v2.MetricTarget `json:"target,omitempty" protobuf:"bytes,3,opt,name=target"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VerticalQueueAutoscalerList is a collection of vertical queue autoscaler.
type VerticalQueueAutoscalerList struct {
	metav1.TypeMeta
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta

	// items is the list of VerticalQueueAutoscaler
	Items []VerticalQueueAutoscaler
}
