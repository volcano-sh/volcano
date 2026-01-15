# HyperJob for Multi-cluster Job Splitting

@JesseStutler; Jan 15, 2026

## Motivation

As AI training workloads grow in scale and complexity, organizations increasingly face the challenge of managing large-scale training jobs across multiple heterogeneous clusters. Several key problems have emerged:
- **Scale Limitations**: Training jobs for large LLM and foundation models require hundreds or thousands of GPUs. Single-cluster scheduling becomes a bottleneck when job requirements exceed cluster capacity.
- **Heterogeneous Infrastructure**: Different clusters may have different types of accelerators (A100, H100, Ascend910B, Asecend910C, etc.), even with different hardware configurations, network topologies, and geographic locations. 
Current solutions lack the ability to split and coordinate training jobs across these heterogeneous environments.
- **Operational Complexity**: Managing distributed training across clusters manually is error-prone and requires deep expertise in both the training framework and cluster orchestration.

Volcano's existing `Job` API is designed for single-cluster batch workload scheduling and lacks the primitives needed for multi-cluster job coordination and splitting. There is a need for a higher-level abstraction that can manage job distribution across clusters while maintaining training semantics.

## Goals

- **High-level Abstraction on Top of Volcano Job**: HyperJob is a higher-level abstraction built on top of Volcano Job. It composes multiple Volcano Job templates and extends training capabilities beyond single cluster boundaries, while preserving the full capabilities of existing Volcano Jobs within each cluster.
- **Automatic Job Splitting and Distribution**: Automatically split large-scale training jobs across multiple heterogeneous clusters based on accelerator requirements and cluster availability, without manual intervention.
- **Simplified Multi-cluster Experience**: Enable users to run multi-cluster training with the same ease as single-cluster Volcano Jobs. Users should not need to understand complex multi-cluster concepts like propagation policies or federation mechanisms.
- **Unified Status Aggregation**: Aggregate and track the status of all sub-jobs across clusters in a single HyperJob resource, providing a unified view of distributed training progress.
- **Enhanced Fault Tolerance and Extensibility**: Support fast failure recovery across clusters and provide extensible plugin mechanisms to accommodate various training frameworks and custom requirements.

## Scope

### In Scope

- **API Definition**: Define HyperJob CRD for declaring multi-cluster training workloads as a composition of Volcano Job templates.
- **Job Splitting and Distribution**: Implement automatic job splitting strategies (static and auto modes) that intelligently distribute training tasks across heterogeneous clusters based on accelerator types and availability.
- **Cluster Selection**: Provide cluster affinity and preference mechanisms to guide job placement decisions while maintaining scheduling flexibility.
- **Status Aggregation**: Track and aggregate the lifecycle status of all sub-jobs across clusters, presenting a unified view through the HyperJob status.
- **Multi-cluster Coordination**: Manage the creation, monitoring, and lifecycle of Volcano Jobs distributed across multiple clusters.

### Out of Scope
- **Network Infrastructure**: Cross-cluster network connectivity, service mesh configuration, and inter-cluster communication setup are not handled by HyperJob. Users should ensure network reachability between clusters beforehand.
- **Cluster Federation**: Cluster discovery, registration, and federation management are assumed to be provided by external multi-cluster systems.
- **Training Framework Logic**: Framework-specific coordination such as distributed training configuration generation and communication patterns are delegated to HyperJob plugins or handled externally.
- **Data Management**: Data synchronization, distributed storage setup, checkpointing strategies, and model artifacts management across clusters are outside the scope of HyperJob.


## Use Cases

### Case 1: Large-scale Training Job Splitting

A research team wants to train a large language model that requires 256 GPUs, but their largest cluster only has 128 GPUs. Using HyperJob, they can split the training job into two sub-jobs, each with 128 GPUs, and run them across two clusters.

```yaml
apiVersion: training.volcano.sh/v1alpha1
kind: HyperJob
metadata:
  name: llm-training
spec:
  minAvailable: 2
  maxDomains: 2
  replicatedJobs:
  - name: trainer
    replicas: 2
    templateSpec:
      tasks:
      - name: worker
        replicas: 128
        template:
          spec:
            containers:
            - name: trainer
              image: training-image:v1
              resources:
                requests:
                  nvidia.com/gpu: 1
```

### Case 2: Heterogeneous Clusters

An organization has multiple clusters with different generations of accelerators (e.g., Ascend NPU 910B and 910C). They need to run a training job across these heterogeneous clusters.

```yaml
apiVersion: training.volcano.sh/v1alpha1
kind: HyperJob
metadata:
  name: ascend-heterogeneous-training
spec:
  minAvailable: 2
  replicatedJobs:
  - name: trainer-910b
    replicas: 1
    clusterNames: ["cluster-ascend-910b-1", "cluster-ascend-910b-2"]
    templateSpec:
      tasks:
      - name: worker
        replicas: 64
        template:
          spec:
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                    - key: hardware-type
                      operator: In
                      values:
                      - Ascend910B
            containers:
            - name: trainer
              image: training-image:v1
              resources:
                requests:
                  ascend910c: 1
                limits:
                  ascend910c: 1
  - name: trainer-910c
    replicas: 1
    clusterNames: ["cluster-ascend-910c-1"]
    templateSpec:
      tasks:
      - name: worker
        replicas: 64
        template:
          spec:
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                    - key: hardware-type
                      operator: In
                      values:
                      - Ascend910C
            containers:
            - name: trainer
              image: training-image:v1
              resources:
                requests:
                  ascend910c: 1
                limits:
                  ascend910c: 1
```

## API Design

The HyperJob API introduces a new CRD in the `training.volcano.sh` API group. 

### HyperJob

```go
type HyperJob struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    
    Spec   HyperJobSpec   `json:"spec,omitempty"`
    Status HyperJobStatus `json:"status,omitempty"`
}
```

`HyperJob` is the top-level resource that users create to declare a multi-cluster training job. 

### HyperJobSpec

```go
type HyperJobSpec struct {
    // ReplicatedJobs defines a group of volcano jobs managed by the hyperjob.
    // Each ReplicatedJob can be replicated multiple times and scheduled to different clusters.
    // +listType=map
    // +listMapKey=name
    ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty"`
    
    // MinAvailable specifies the minimum number of volcano jobs that must be running
    // for the HyperJob to be considered healthy. This enables partial failure tolerance.
    // If not specified, all replicated jobs must be running.
    // +optional
    MinAvailable *int32 `json:"minAvailable,omitempty"`
    
    // MaxDomains specifies the maximum number of clusters (domains) across which
    // the HyperJob can be split. This is used by the auto-splitting algorithm
    // to determine the optimal distribution strategy.
    // +optional
    MaxDomains *int32 `json:"maxDomains,omitempty"`
    
    // Plugins specifies framework-specific plugins to be enabled for this HyperJob.
    // These plugins can perform cross-cluster coordination, configuration generation,
    // and other framework-specific tasks. The key is the plugin name, and the value
    // is a list of arguments passed to the plugin.
    // +optional
    Plugins map[string][]string `json:"plugins,omitempty"`
}
```

**Field Descriptions:**

- **ReplicatedJobs**: A group of Volcano Jobs managed by the HyperJob. Each ReplicatedJob defines a Volcano Job template that can be replicated multiple times and distributed across different clusters.

- **MinAvailable**: The minimal number of available Volcano Jobs required to run the HyperJob. This provides fault tolerance by allowing the HyperJob to remain operational even if some Volcano Jobs fail.
For example, if the total number of replicated jobs is 4 but only 3 need to be running for successful training, set this to 3, then the HyperJob will still be considered as Running.
  > **Note**: This field is reserved and is currently not used by the controller. Currently the HyperJob doesn't have an explicit phase but will expand if HyperJob needs the ability to quickly recover from failures, just like vcjob. 

- **MaxDomains**: The maximum number of domains (clusters) to split the HyperJob across. This is used in multi-cluster job splitting scenarios.
  > **Note**: This field is reserved for future automatic splitting features and is currently not used by the controller.

- **Plugins**: Specifies the plugins to be enabled for the HyperJob. The key is the plugin name, and the value is the list of arguments for the plugin. Similar to Volcano Job plugins, this allows framework-specific extensions.
  > **Note**: This field is reserved. Currently the controller does not implement any HyperJob-specific plugins, but this field is provided for future extensibility.

### ReplicatedJob

```go
type ReplicatedJob struct {
    // Name is a unique identifier for this replicated job within the HyperJob.
    Name string `json:"name"`
    
    // TemplateSpec defines the Volcano Job specification that will be used
    // to create actual jobs. This is similar to PodTemplateSpec in Deployments.
    TemplateSpec v1alpha1.JobSpec `json:"templateSpec"`
    
    // SplitPolicy determines how this replicated job should be split across clusters.
    // If not specified, the job will not be split and will be scheduled as-is.
    // +optional
    SplitPolicy *SplitPolicy `json:"splitPolicy,omitempty"`
    
    // Replicas specifies how many copies of this job template should be created.
    // Each replica can potentially be scheduled to a different cluster based on
    // resource availability and cluster preferences.
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas,omitempty"`
    
    // ClusterNames provides a list of preferred cluster names for scheduling this job.
    // The scheduler will attempt to place replicas on these clusters first, but may
    // choose other clusters if resources are unavailable. Empty list means no preference.
    // +optional
    ClusterNames []string `json:"clusterNames,omitempty"`
}
```

**Field Descriptions:**

- **Name**: A unique identifier for this replicated job within the HyperJob. Used for status tracking and identifying which Volcano Jobs belong to which replicated job.

- **TemplateSpec**: The Volcano Job specification that will be replicated. This is the template used to create actual Volcano Jobs in target clusters.

- **SplitPolicy**: Specifies the splitting strategy for multi-cluster job splitting, including the number and types of accelerators that need to be split.
  > **Note**: This field is reserved for future automatic splitting features and is currently not used by the controller.

- **Replicas**: The number of Volcano Jobs to be created from this template. Each replica will be scheduled to different clusters based on resource availability and cluster preferences.

- **ClusterNames**: The list of cluster names to which the replicated jobs prefer to be scheduled. The controller will attempt to place replicas on these clusters first.

### SplitPolicy

```go
type SplitPolicy struct {
    // Mode determines the splitting strategy.
    // - static: User explicitly defines how many accelerators each sub-job should have.
    //           The controller creates exactly enough sub-jobs to satisfy the total requirement.
    // - auto: Controller automatically determines the optimal splitting strategy based on
    //         cluster resource availability and MaxDomains constraint.
    // +kubebuilder:validation:Enum=static;auto
    Mode SplitMode `json:"mode,omitempty"`
    
    // Accelerators specifies the total number of accelerators needed for this job.
    // In static mode, each sub-job will receive an equal share (or as equal as possible).
    // In auto mode, this is the total requirement that the controller will satisfy
    // by optimally distributing across available clusters.
    // +optional
    Accelerators *int `json:"accelerators,omitempty"`
    
    // AcceleratorType specifies the resource name for the accelerator to split on.
    // Common values: "nvidia.com/gpu", "amd.com/gpu", "habana.ai/gaudi"
    // This must match the resource name used in the task template's resource requests.
    // +optional
    AcceleratorType *string `json:"acceleratorType,omitempty"`
}

type SplitMode string

const (
    // SplitModeStatic indicates that jobs should be split into equal parts
    // based on the specified accelerator count and number of replicas.
    SplitModeStatic SplitMode = "static"
    
    // SplitModeAuto indicates that the controller should automatically
    // determine the optimal split based on cluster availability.
    SplitModeAuto SplitMode = "auto"
)
```

**Field Descriptions:**

- **Mode**: The mode of the split policy. Supports "static" and "auto" modes.
  > **Note**: Both static and auto modes are reserved for future automatic splitting features and are currently not implemented by the controller. 
  Currently controller follows static segmentation, and auto means that external services can be scaled to connect to external services in the future, 
  and the external services determine the partitioning policy

- **Accelerators**: The number of accelerators to split across clusters.
  > **Note**: This field is reserved for future automatic splitting features and is currently not used by the controller.

- **AcceleratorType**: The type of the accelerator, such as "nvidia.com/gpu", "amd.com/gpu", "huawei.com/ascend910", etc.
  > **Note**: This field is reserved for future automatic splitting features and is currently not used by the controller.

### HyperJobStatus

```go
type HyperJobStatus struct {
    // Conditions represent the latest observations of the HyperJob's state.
    // Only set when all child Volcano Jobs have reached terminal states.
    // Supported condition types:
    // - "Completed": All child jobs completed successfully
    // - "Failed": At least one child job failed, aborted, or terminated
    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    
    // ReplicatedJobsStatus tracks the status of each replicated job.
    // +listType=map
    // +listMapKey=name
    ReplicatedJobsStatus []ReplicatedJobStatus `json:"replicatedJobsStatus,omitempty"`
    
    // SplitCount represents the total number of Volcano Jobs this HyperJob is split into by the controller.
    // +optional
    SplitCount *int32 `json:"splitCount,omitempty"`
    
    // ObservedGeneration is the generation observed by the controller.
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// Condition types for HyperJob
const (
    // HyperJobConditionCompleted means all child Volcano Jobs have completed successfully
    HyperJobConditionCompleted = "Completed"
    // HyperJobConditionFailed means all child Volcano Jobs have finished but at least one failed
    HyperJobConditionFailed = "Failed"
)
```

### ReplicatedJobStatus

```go
type ReplicatedJobStatus struct {
    // Name of the replicated job.
    Name string `json:"name"`
    
    // JobStates stores the state of each Volcano Job created by the replicated job.
    JobStates map[string]v1alpha1.JobState `json:"jobStates,omitempty"`
    
    // Pending is the total number of pods under the replicated job in pending state.
    Pending int32 `json:"pending,omitempty"`
    
    // Running is the total number of pods under the replicated job in running state.
    Running int32 `json:"running,omitempty"`
    
    // Succeeded is the total number of pods under the replicated job in succeeded state.
    Succeeded int32 `json:"succeeded,omitempty"`
    
    // Failed is the total number of pods under the replicated job in failed state.
    Failed int32 `json:"failed,omitempty"`
    
    // Terminating is the total number of pods under the replicated job in terminating state.
    Terminating int32 `json:"terminating,omitempty"`
    
    // Unknown is the total number of pods under the replicated job in unknown state.
    Unknown int32 `json:"unknown,omitempty"`
}
```

**Status Tracking:**

The HyperJob status provides multiple levels of observability:

- **Conditions**: Represents the terminal state of the HyperJob. Following the Kubernetes Job pattern, conditions are only set when all child Volcano Jobs have reached a terminal state. Two condition types are supported:
  - `Completed` (type: "Completed"): Set to True when all child Volcano Jobs have completed successfully
  - `Failed` (type: "Failed"): Set to True when all child Volcano Jobs have finished but at least one has failed, aborted, or terminated
  
  During execution, when child jobs are still running or pending, no condition is set. This allows users to distinguish between in-progress and terminal states.

- **ReplicatedJobsStatus**: Status for each replicated job, including:
  - Individual Volcano Job states mapped by job name
  - Aggregated pod counts (Pending, Running, Succeeded, Failed, Terminating, Unknown) across all jobs created from the replicated job template

- **SplitCount**: Total number of Volcano Jobs created from the HyperJob

- **ObservedGeneration**: The spec generation that has been reconciled by the controller

### Comparison with Volcano Job

HyperJob is built on top of Volcano Job, not as a replacement. It extends Volcano's capabilities to multi-cluster scenarios while preserving all the features of Volcano Job within each cluster.

| Aspect | Volcano Job | HyperJob |
|--------|-------------|----------|
| **Scope** | Single cluster | Multiple clusters |
| **Abstraction Level** | Cluster-level primitive (manages Pods) | Meta-level primitive (manages Volcano Jobs) |
| **Primary Use Case** | Batch workload scheduling | Large-scale training across heterogeneous clusters |
| **Job Composition** | Single job with multiple tasks | Composition of multiple Volcano Jobs |
| **Status Tracking** | Tracks pods within a single job | Aggregates status from multiple Volcano Jobs across clusters |

HyperJob is designed for scenarios where training requirements exceed single cluster capacity or need to leverage heterogeneous accelerator resources across different clusters.

## Implementation

The HyperJob controller is responsible for splitting HyperJob into multiple Volcano Jobs and aggregate each Volcano Job's status across multiple clusters.

For detailed controller implementation design is documented in the volcano-global repository:
- [HyperJob Controller Design](https://github.com/volcano-sh/volcano-global/blob/main/docs/proposals/hyperjob-controller.md)

## Future Work

### Automatic Job Splitting
Implement the SplitPolicy automatically split jobs across clusters based on accelerator requirements:
- Static mode: Split jobs into equal parts based on specified accelerator count
- Auto mode: Integrate with external services to dynamically determine optimal split based on cluster resource availability

### Enhanced Features
- **Advanced Fault Tolerance**: Implement automatic job migration and recovery strategies when sub-jobs fail across clusters
- **HyperJob Plugins**: Develop HyperJob-specific plugins for framework-specific multi-cluster coordination

