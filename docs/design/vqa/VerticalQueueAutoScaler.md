# VerticalQueueAutoScaler

[@zbbkeepgoing](https://github.com/zbbkeepgoing); Aug 31, 2022

## Motivation

Queue has the ability of resource isolation and resource capacity management in the whole Volcano scheduling pipeline, but the resource capacity control of this queue can not be update automatically in Volcano at present. When the Queue is insufficient, we must adjust the resource capacity manually (although the Weight-Based of queue can slove the problem to a certain extent), but in the following scenarios, the capacity control of Queues is more important.

-  There are tidal effects in many businesses. For example, Queue in Team A is usually used in large quantities from 8:00 to 18:00, while the Queue in Team B is usually used in large quantities from 18:00 to 8:00. Although this scenario can be sloved by Weight-Based of queue to a certain extend, however, if weight can be adjusted based on the time point, it will be more reasonable for the resource allocation of Team A and Team B.

- When using capacity and guarantee to manage Queue instand of wight

  - It means that we cannot meet the resource capacity requirements of Queues at different times like Weight-Based.

  - When task of Queue A meet the capacity in a period of time, but Queue B is not even full of guarantee at this time, in order to improve the utilization rate of the overall cluster, it is necessary to use some indicators to complete the expansion and contraction of the Queue.


## Function Specification

### API

```go
type VerticalQueueAutoscalerType string
type Condition string

const (
	VerticalQueueAutoscalerTidalType VerticalQueueAutoscalerType = "tidal"
	VerticalQueueAutoscalerTidalType VerticalQueueAutoscalerType = "metrics"
	AndCondition                     Condition                   = "and"
	OrCondition                      Condition                   = "or"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=verticalqueueautoscalers,shortName=vqa
// +kubebuilder:subresource:status

// VerticalQueueAutoscaler defines the volcano vertical queue autoscaler.
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=`.status.state.phase`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="QUEUE",type=string,priority=1,JSONPath=`.spec.queue`
type VerticalQueueAutoscaler struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the volcano job, including the minAvailable
	// +optional
	Spec VQASpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of the volcano Job
	// +optional
	Status VQAStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// VQASpec describes how the VerticalQueueAutoscaler will look like and actually behavior.
type VQASpec struct {

	//Specifies the queue that will be used in the scheduler, "default" queue is used this leaves empty.
	Queue string `json:"queue,omitempty" protobuf:"bytes,0,opt,name=queue"`

	// Type is the behavior type of VQA.
	//+kubebuilder:validation:Enum=tidal;metrics
	Type VerticalQueueAutoscalerType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// TidalSpec define vqa configuration in tidal autoscaler type
	// +optional
	TidalSpec []TidalSpec `json:"tidalSpec,omitempty" protobuf:"bytes,2,opt,name=tidalSpec"`

	// TidalSpec define vqa configuration in tidal autoscaler type
	// +optional
	MetricSpec []MetricSpec `json:"tidalSpec,omitempty" protobuf:"bytes,2,opt,name=tidalSpec"`
}

type TidalSpec struct {
	// Schedule The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron
	Schedule   string              `json:"schedule,omitempty"`
	Weight     int32               `json:"weight,omitempty"`
	Capability corev1.ResourceList `json:"capability,omitempty"`
	Guarantee  corev1.ResourceList `json:"guarantee,omitempty"`
}

type MetricSpec struct {
	ScaleBehavior ScaleBehavior `json:"scaleBehavior,omitempty"`
	ResourceLimit ResourceLimit `json:"resourceLimit,omitempty"`
	MetricRules   MetricRules   `json:"metricRules,omitempty"`
}

type ScaleBehavior struct {
	StepType      string        `json:"stepType,omitempty"`
	StepRatio     float64       `json:"stepRatio,omitempty"`
	StepResources StepResources `json:"stepResources,omitempty"`
}

type StepResources struct {
	Weight     int32               `json:"weight,omitempty"`
	Capability corev1.ResourceList `json:"capability,omitempty"`
	Guarantee  corev1.ResourceList `json:"guarantee,omitempty"`
}

type ScaleLimit struct {
	WeightLimit    WeightLimit   `json:"weightLimit,omitempty"`
	CapacityLimit  ResourceLimit `json:"capacityLimit,omitempty"`
	GuaranteeLimit ResourceLimit `json:"guaratneeLimit,omitempty"`
}

type WeightLimit struct {
	Min int32 `json:"min,omitempty"`
	Max int32 `json:"max,omitempty"`
}

type ResourceLimit struct {
	Max corev1.ResourceList `json:"max,omitempty"`
	Min corev1.ResourceList `json:"min,omitempty"`
}

type MetricRules struct {
	ScaleUp   ScaleRule `json:"scaleUp,omitempty"`
	ScaleDown ScaleRule `json:"scaleDown,omitempty"`
}

type ScaleRule struct {
	// StabilizationWindowSeconds is the number of seconds for which past recommendations should be
	// considered while scaling up or scaling down.
	// StabilizationWindowSeconds must be greater than or equal to zero and less than or equal to 3600 (one hour).
	// If not set, use the default values:
	// - For scale up: 0 (i.e. no stabilization is done).
	// - For scale down: 300 (i.e. the stabilization window is 300 seconds long).
	// +optional
	StabilizationWindowSeconds *int32         `json:"stabilizationWindowSeconds,omitempty"`
	Condition                  Condition      `json:"condition,omitempty"`
	MetricSources              []MetricSource `json:"metricSources,omitempty"`
}

type MetricSource struct {
	// PeriodSeconds specifies the window of time for which the policy should hold true.
	// PeriodSeconds must be greater than zero and less than or equal to 1800 (30 min).
	PeriodSeconds int32               `json:"periodSeconds"`
	Metric        v2.MetricIdentifier `json:"metric,omitempty"`
	Target        v2.MetricTarget     `json:"target,omitempty"`
}
```

### Yaml Sample

```yaml
apiVersion: scheduling.volcano.sh/v1alpha1
kind: VerticalQueueAutoscaler
metadata:
  name: vqa-example
spec:
  queue: queue-example
  type: tidal/metrics
  tidalSpec:
  - schedule: 0 0 * * *
    weight: 2
    capacity:
      cpu: 2
      memory: 4Gi
    guarantee:
       cpu: 2
       memroy: 4Gi
  metricsSpec:
    scaleBehavior:
      stepType: equivalent/equiratio
      stepRatio: 2
      stepResources: 
        weight: 2
        resources:
          cpu: 4
          memory: 16Gi
    scaleLimit:
      weightLimit:
        max: 10
        min: 1
      capabilityLimit:
        max:
          cpu: 24
          memory: 96Gi
        min:
          //...
      guaranteeLimit:
        max:
          cpu: 8
          memory: 32Gi
        min:
          //...
    metricRules:
      scaleUp:
        stabilizationWindowSeconds: 300
        condition: and/or
        metricSources:
        - periodSeconds: 60
          metric:
            name: queue_capacity_utilization
            selector:
              matchLabels:
                queue: "queue-example"
          target:
            type: AverageValue/Utilization/Value
            averageValue: 30
            averageUtilization: 50
            value: 100
      scaleDown:
        //...
```

### VQAController

The `VQAController` will manage the lifecycle of VirtualQueueAutoscaler.

#### Tidal

1. Watch Create,Update,Delete event for vqa.

2. Find whether there is tidal cron need to be scheduled now.

3. Find the next scheduled time from now, and put it into delay queue.


#### Metrics

1. Calculate the resource capacity that needs to be scaleup or scaledown with metric rules and scale behavior.

2. Update weight、capacity and guarantee of queue.