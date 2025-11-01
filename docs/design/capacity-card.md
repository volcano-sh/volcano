# Capacity Card Scheduling Plugin

@contributors

## Introduction

The Capacity Card plugin is a Volcano scheduler plugin designed to provide fine-grained GPU/NPU/accelerator card resource management and scheduling capabilities in heterogeneous computing clusters. It extends Volcano's capacity scheduling capabilities to support various types of accelerator cards, including whole cards, MPS (Multi-Process Service) shared cards, MIG (Multi-Instance GPU) cards, and other shared card technologies.

As AI and HPC workloads increasingly rely on diverse GPU hardware (A100, H100, H200, V100, etc.), users need precise control over card resource allocation at the queue level while supporting flexible multi-card selection strategies for jobs.

## Motivation

Current Kubernetes native resource management faces several limitations when dealing with heterogeneous GPU clusters:

1. **Lack of Fine-grained Card Type Management**: Standard Kubernetes resource requests cannot distinguish between different GPU card types (e.g., A100 vs V100) or different GPU sharing profiles (MPS, MIG etc.).

2. **Insufficient Queue-level Card Quota Control**: Organizations need to allocate specific numbers of different card types to different teams/projects, which cannot be easily achieved with native Kubernetes resource quotas.

3. **Inflexible Multi-Card Selection**: Jobs often can run on multiple types of cards with similar capabilities, but Kubernetes lacks a mechanism to express "this job can use card type A OR card type B".

The Capacity Card plugin addresses these challenges by providing:
- Annotation-based card resource specification for queues and jobs
- Support for multiple card types and sharing modes
- Multi-card selection capability (e.g., "use A100 or H100")
- Integration with Volcano's capacity scheduling framework

## In Scope

- Fine-grained card resource quota management at the queue level
- Job-level card resource request validation before enqueueing
- Task-level card name specification and allocation validation
- Support for MPS (Multi-Process Service) shared GPU resources
- Support for MIG (Multi-Instance GPU) shared GPU resources
- Support for whole card and mixed card/shared resource scenarios
- Multi-card selection support (allowing tasks to specify multiple acceptable card types)
- Automatic card resource discovery from node labels
- CPU/Memory unlimited mode for card resources (optional)

## Out of Scope

- Hierarchical queue card quota management
- Preemption and reclaim are not supported for now

## User Stories

### Story 1: Heterogeneous GPU Cluster Management

As a cluster administrator, I want to manage a cluster with multiple GPU types (A100, H100, V100) and allocate specific card quotas to different teams through queues.

For example:
- Team A (queue-a): 10 A100 cards, 5 H100 cards
- Team B (queue-b): 20 V100 cards, 3 A100 cards

### Story 2: Multi-Card Selection for Job Flexibility

As a data scientist, I want to submit a training job that can run on either A100 or H100 GPUs, whichever is available first, without creating separate job submissions.

### Story 3: Mixed Whole and Shared GPU Scheduling

As a platform engineer, I want to provide both whole GPU cards for large training jobs and MPS/MIG partitioned cards for inference services in the same cluster, with separate quota management.

### Story 4: Queue-level Card Quota Enforcement

As a resource manager, I want to ensure that no team can exceed their allocated card quota, even if cluster capacity is available, to enforce SLA agreements.

## Design Detail

### Architecture Overview

The Capacity Card plugin works by:
1. Discovering card resources from node labels and status
2. Parsing card quotas from queue annotations
3. Validating job card requests against queue card quotas
4. Tracking card resource allocation across jobs and tasks
5. Enforcing allocation limits during scheduling

### Key Concepts

#### Card Resource vs. K8s Resource

- **K8s Resource Name**: The actual resource name in node status and pod requests (e.g., `nvidia.com/gpu`, `nvidia.com/gpu.shared`, `nvidia.com/mig-1g.5gb`)
- **Card Name**: A user-friendly, normalized name for the card type (e.g., `NVIDIA-A100-80GB`, `NVIDIA-A100-80GB/mps-80g*1/8`)

The plugin maintains a mapping between card names and K8s resource names for scheduling decisions.

#### Card Types

1. **Whole Card**: Full GPU card resources (e.g., `nvidia.com/gpu`)
2. **MPS Shared Card**: NVIDIA MPS partitioned GPUs (e.g., `nvidia.com/gpu.shared`)
3. **MIG Shared Card**: NVIDIA MIG partitioned GPUs (e.g., `nvidia.com/mig-1g.5gb`)

#### Multi-Card Request

Tasks can specify multiple acceptable card types separated by `|`:
```
NVIDIA-A100-80GB|NVIDIA-H100-80GB
```

During scheduling, the plugin checks if any of the specified card types has sufficient quota in the queue.

### API Design

#### Queue Annotation for Card Quota

Queues use the annotation `volcano.sh/card.quota` to specify card resource quotas:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-a
  annotations:
    volcano.sh/card.quota: |
      {
        "NVIDIA-A100-80GB": 10,
        "NVIDIA-H100-80GB": 5,
        "NVIDIA-A100-80GB/mps-80g*1/8": 16
      }
spec:
  capability:
    cpu: "100"
    memory: "200Gi"
  guarantee:
    resource:
      cpu: "50"
      memory: "100Gi"
```

**Format**: JSON object mapping card names to counts (integers)

#### Job Annotation for Card Request

Jobs use the annotation `volcano.sh/card.request` to specify card resource requests for validation:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: training-job
  annotations:
    volcano.sh/card.request: |
      {
        "NVIDIA-A100-80GB": 8
      }
spec:
  schedulerName: volcano
  queue: queue-a
  minAvailable: 1
  tasks:
    - replicas: 8
      name: worker
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100-80GB"
        spec:
          containers:
            - name: trainer
              image: training:latest
              resources:
                limits:
                  nvidia.com/gpu: 1
```

**Purpose**: Pre-validation before job enqueueing to provide fast feedback

#### Task Annotation for Card Name

Tasks/Pods use the annotation `volcano.sh/card.name` to specify the desired card name:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-pod
  annotations:
    volcano.sh/card.name: "NVIDIA-A100-80GB|NVIDIA-H100-80GB"
spec:
  schedulerName: volcano
  containers:
    - name: trainer
      image: training:latest
      resources:
        limits:
          nvidia.com/gpu: 1
```

**Multi-Card Format**: Use `|` to separate multiple acceptable card types. The scheduler will check quota availability for each type and allocate based on availability.

#### Plugin Configuration

The plugin supports configuration through scheduler config:

```yaml
actions: "enqueue, allocate, backfill"
tiers:
  - plugins:
      - name: capacity-card
        arguments:
          cardUnlimitedCpuMemory: true  # Optional: if true, card resources don't require CPU/Memory quota
```

**Configuration Options**:
- `cardUnlimitedCpuMemory` (bool, default: false): If set to true, tasks requesting card resources are not checked against queue's CPU/Memory quota limits. Useful when card resources are the primary constraint.

### Node Card Discovery

The plugin automatically discovers card resources from node labels:

#### Label Format

```yaml
apiVersion: v1
kind: Node
metadata:
  labels:
    nvidia.com/gpu.product: "NVIDIA-A100-80GB"     # Card product name
    nvidia.com/gpu.count: "8"                       # Number of cards
    nvidia.com/gpu.memory: "81920"                  # Memory per card in MB
    nvidia.com/gpu.replicas: "8"                    # For MPS: number of replicas
    nvidia.com/mig-1g.5gb.count: "7"               # For MIG: count of this profile
status:
  allocatable:
    nvidia.com/gpu: "8"                             # Whole card resource
    nvidia.com/gpu.shared: "64"                     # MPS shared resource
    nvidia.com/mig-1g.5gb: "7"                      # MIG partition resource
```

#### Card Name Generation

- **Whole Card**: Uses the value from `<prefix>/gpu.product` label
  - Example: `NVIDIA-A100-80GB`
  
- **MPS Card**: Generated as `<card-product>/mps-<memory>g*1/<replicas>`
  - Example: `NVIDIA-A100-80GB/mps-80g*1/8`
  
- **MIG Card**: Generated as `<card-product>/mig-<profile>-mixed`
  - Example: `NVIDIA-A100-80GB/mig-1g.5gb-mixed`

### Main Process

#### Plugin Initialization (OnSessionOpen)

1. **Build Total Resource**:
   - List all nodes from the informer
   - Extract card information from node labels
   - Parse card resources from node status
   - Build mapping: card name → K8s resource name
   - Calculate cluster total resources (CPU, Memory, Cards)

2. **Build Queue Attributes**:
   - Parse card quotas from queue annotations (`volcano.sh/card.quota`)
   - Calculate queue capability, guarantee, and deserved resources
   - Track allocated, inqueue, and elastic resources per queue

3. **Register Scheduling Functions**:
   - `JobEnqueueableFn`: Pre-check job card requests against queue quota
   - `AllocatableFn`: Validate task card allocation against queue quota
   - `AllocateFunc` / `DeallocateFunc`: Update queue resource tracking

#### Job Enqueueable Check

When a job is submitted:

1. Parse job's card request from annotation (`volcano.sh/card.request`)
2. Calculate total resources to be used: `allocated + inqueue + job.minResources - elastic`
3. Check CPU/Memory quota (unless `cardUnlimitedCpuMemory` is enabled)
4. Check card resource quota:
   - For each card type requested
   - If multi-card request (contains `|`), check each alternative
   - Verify: `totalToBeUsed[cardType] <= queueCapability[cardType]`
5. If all checks pass, mark job as InQueue and reserve resources

#### Task Allocatable Check

When scheduling a task:

1. Parse task's card request from annotation (`volcano.sh/card.name`)
2. Extract card resource from pod's resource requests
3. Calculate total resources to be allocated: `allocated + task.request`
4. Check CPU/Memory quota (unless `cardUnlimitedCpuMemory` is enabled)
5. Check card resource quota:
   - Support multi-card selection (e.g., `A100|H100`)
   - For multi-card, check each option and succeed if any passes
   - Verify: `totalToBeAllocated[cardType] <= queueCapability[cardType]`
6. If checks fail, emit Kubernetes events to pod with reason

#### Resource Tracking

The plugin maintains real-time resource tracking:

- **On Allocate**: Add task resources to `queue.allocated`
- **On Deallocate**: Subtract task resources from `queue.allocated`
- **Queue Share Calculation**: `share = max(allocated[resource] / deserved[resource])`

### Implementation Details

#### Card Resource Quantification

Card resources are stored as scalar resources in milli-units (multiplied by 1000):
- 2 cards → 2000 in scalar resources
- This aligns with Volcano's internal resource representation

#### Multi-Card Request Processing

For a multi-card request like `A100|H100|V100`:

1. Split by `|` separator
2. For each card type in the list:
   - Clone `toBeUsedResource`
   - Add requested quantity to each individual card name
   - Check if `toBeUsedResource[cardType] <= queueCapability[cardType]`
   - If any card type passes, return success
3. If all fail, return the error with the multi-card name

#### Event Recording

The plugin emits Kubernetes events for:
- `GetTaskRequestResourceFailed`: Failed to parse task resource request
- `EmptyQueueCapability`: Queue has no capability configured
- `InsufficientCPUQuota`: Insufficient CPU quota in queue
- `InsufficientMemoryQuota`: Insufficient memory quota in queue
- `InsufficientScalarQuota`: Insufficient card/scalar quota in queue

### Integration with Capacity Scheduling

The Capacity Card plugin builds upon Volcano's capacity plugin concepts:

- **Capability**: Maximum card resources a queue can use
- **Guarantee**: Reserved card resources not shared with other queues
- **Deserved**: Target allocation for fair sharing and reclaim

However, unlike the standard capacity plugin, card resources are specified via annotations rather than the Queue's ResourceList fields, allowing more flexible card type specification.

### Metrics and Observability

The plugin exports Prometheus metrics for queue resource tracking:
- `volcano_queue_card_deserved`: Deserved card resources per queue
- `volcano_queue_card_allocated`: Currently allocated card resources per queue
- `volcano_queue_card_request`: Requested card resources per queue
- `volcano_queue_card_capacity`: Card capacity per queue

## Example Scenarios

### Example 1: Basic Card Quota

**Cluster Setup**:
- 2 nodes with 4 A100 cards each (total: 8 A100)

**Queue Configuration**:
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: team-a
  annotations:
    volcano.sh/card.quota: '{"NVIDIA-A100-80GB": 5}'
spec:
  capability:
    cpu: "100"
    memory: "500Gi"
```

**Job Submission**:
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: training
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100-80GB": 4}'
spec:
  queue: team-a
  minAvailable: 4
  tasks:
    - replicas: 4
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100-80GB"
        spec:
          containers:
            - name: worker
              resources:
                limits:
                  nvidia.com/gpu: 1
```

**Result**: Job successfully enqueued (4 ≤ 5) and tasks scheduled.

### Example 2: Multi-Card Selection

**Job Submission**:
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: flexible-training
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100-80GB|NVIDIA-H100-80GB": 4}'
spec:
  queue: team-a
  minAvailable: 1
  tasks:
    - replicas: 4
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100-80GB|NVIDIA-H100-80GB"
        spec:
          containers:
            - name: worker
              resources:
                limits:
                  nvidia.com/gpu: 1
```

**Result**: The scheduler will try to allocate A100 first; if quota exhausted, tries H100.

### Example 3: MPS Shared GPU

**Node Labels**:
```yaml
nvidia.com/gpu.product: "NVIDIA-A100-80GB"
nvidia.com/gpu.count: "4"
nvidia.com/gpu.memory: "81920"
nvidia.com/gpu.replicas: "8"
```

**Node Status**:
```yaml
status:
  allocatable:
    nvidia.com/gpu.shared: "32"  # 4 cards × 8 replicas
```

**Queue Configuration**:
```yaml
metadata:
  annotations:
    volcano.sh/card.quota: '{"NVIDIA-A100-80GB/mps-80g*1/8": 32}'
```

**Job Submission**:
```yaml
metadata:
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100-80GB/mps-80g*1/8": 16}'
spec:
  tasks:
    - replicas: 16
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100-80GB/mps-80g*1/8"
        spec:
          containers:
            - resources:
                limits:
                  nvidia.com/gpu.shared: 1
```

**Result**: 16 inference pods share the 4 A100 GPUs via MPS.

## Notes

1. **Plugin Compatibility**: The Capacity Card plugin is designed to work alongside other Volcano plugins (gang, priority, etc.). It should not be enabled simultaneously with the standard `capacity` or `proportion` plugin to avoid conflicts.

2. **Card Discovery Requirements**: Node labels must be properly configured (typically by GPU operators like NVIDIA GPU Operator) for card discovery to work correctly.

3. **Annotation-based Design**: The choice of annotations over native Kubernetes ResourceList allows for:
   - More flexible naming conventions
   - Support for multi-card selection syntax
   - Easier evolution without API changes

4. **Multi-Card Scheduling**: The current implementation checks quota for multi-card requests but enforce that all tasks in a job use the same card type. Future enhancements may add more flexible controls.

5. **Performance Considerations**: The plugin caches node card information to minimize overhead during scheduling cycles.

## Future Work

- Node-level card selection ordering function
- Support different Pods in the same job using different kinds of cards
- Support preemption and reclaim

## References

- [Volcano Capacity Scheduling Design](./capacity-scheduling.md)
- [NVIDIA MPS Documentation](https://docs.nvidia.com/deploy/mps/index.html)
- [NVIDIA MIG User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/)
- [Volcano Scheduler Framework](https://volcano.sh/en/docs/)
