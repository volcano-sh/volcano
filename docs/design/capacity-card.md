# Capacity Card Scheduling Plugin

## Introduction

The Capacity Card plugin is a Volcano scheduler plugin designed to provide fine-grained management and scheduling capabilities for GPU/NPU/DCU and other accelerator card resources in heterogeneous computing clusters. It extends Volcano's capacity scheduling capabilities to support various types of accelerator cards, including whole cards, MPS (Multi-Process Service) shared cards, MIG (Multi-Instance GPU) cards, and other shared card technologies.

As AI and HPC workloads increasingly rely on diverse GPU hardware (A100, H100, H200, V100, etc.), users need precise control over card resource allocation at the queue level, while supporting flexible multi-card selection strategies for jobs.

## Motivation

Current schedulers face several limitations when handling heterogeneous GPU clusters:

1. **Lack of fine-grained card type quota management**: Standard GPU resource requests cannot differentiate between different card types. For example, card models like `NVIDIA-H200` and `NVIDIA-A800` all use the resource name `nvidia.com/gpu`. Current queue quotas only support card resource names and cannot implement card type configuration.

2. **Lack of support for single-instance multi-card requirements**: Due to scarce GPU resources, to maximize utilization of existing GPU resources, online and offline instances (Pods) need to support single-instance multi-card model deployment. For example, when deploying a single instance, it needs to support simultaneous use of GPU models like `NVIDIA GPU 4090, 4090D, 5090`, deploying whichever GPU type has available resources.

3. **CPU/memory coupled with GPU resources**: For GPU servers, GPU is the bottleneck, so there's no need to limit CPU/memory. CPU/memory quotas should only apply to non-GPU servers.

## User Stories

### Story 1: Heterogeneous GPU Cluster Management

As a cluster administrator, I want to manage a cluster with multiple GPU types (A100, H100, V100) and allocate specific card quotas to different teams through queues.

For example:
- Team A (queue-a): 10 A100 cards, 5 H100 cards
- Team B (queue-b): 20 V100 cards, 3 A100 cards

### Story 2: Job Flexibility with Multi-Card Selection

As a data scientist, I want to submit a training job that can run on either H100 or A100 GPUs, with preference for H100.

### Story 3: CPU/Memory Decoupled from GPU Resources

As a resource administrator, I want to ensure GPU tasks only consume GPU quotas, not CPU/memory quotas, thereby reducing configuration complexity.

## Core Goals

1. Implement fine-grained card type quota management

2. Support single-instance multi-card requirements

3. Decouple CPU/memory from GPU resource quotas

## Design Details

### Architecture Overview

How the Capacity Card plugin works:
1. Discover card resources from node labels and status
2. Parse card quotas from queue annotations
3. Validate job card requests against queue card quotas
4. Track card resource allocation across jobs and tasks
5. Enforce allocation limits during scheduling

### Key Concepts

#### Card Resources vs. K8S Resources

- **K8S Resource Name**: The actual resource name in node status and Pod requests (e.g., `nvidia.com/gpu`, `nvidia.com/gpu.shared`, `nvidia.com/mig-1g.18gb`)
- **Card Type Name**: User-friendly, canonical name for card types (e.g., `NVIDIA-A100`, `NVIDIA-H800/mps-80*1/2`, `NVIDIA-H200/mig-1g.18gb-mixed`)

|K8S Resource Name|Card Type Name|Note|
|---|---|---|
|`nvidia.com/gpu`|`NVIDIA-A100`|Whole card|
|`nvidia.com/gpu.shared`|`NVIDIA-H800/mps-80*1/2`|MPS sub-card|
|`nvidia.com/mig-1g.18gb`|`NVIDIA-H200/mig-1g.18gb-mixed`|MIG sub-card|

The plugin maintains mappings between card names and K8s resource names for scheduling decisions.

#### Multi-Card Requests

Tasks can specify multiple acceptable card types, separated by `|`, with priority decreasing from left to right:
```
NVIDIA-A100|NVIDIA-H100
```

### API Design

#### Queue Annotations for Card Quotas

Queues use the annotation `volcano.sh/card.quota` to specify card resource quotas:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-a
  annotations:
    volcano.sh/card.quota: |
      {
        "NVIDIA-A100": 10,
        "NVIDIA-H100": 5,
        "NVIDIA-A100/mps-80g*1/8": 16
      }
spec:
  capability:
    cpu: "100"
    memory: "200Gi"
```

**Card Type Quotas**: Set quotas for each card type via JSON object in the `volcano.sh/card.quota` annotation. Card types without quotas default to 0, and must have a quota to use that card type.

Using annotations rather than configuring in `capability`, `deserved`, or `guarantee` avoids compatibility issues and only takes effect for the Capacity Card plugin.

Additionally, there's no need to set K8S resource names like `nvidia.com/gpu` in `capability`, `deserved`, or `guarantee`. The Capacity Card plugin automatically maps card type names to K8S resource names, simplifying management complexity.

**CPU/Memory Quotas**: CPU/memory quotas are set in `capability`. If not set, the default quota is also 0. No need to configure `guarantee` as lower-level queues don't reserve resources; upper-level business systems control `capability` during allocation to implement resource reservation. `deserved` also doesn't need configuration as resource reclamation is not currently supported.

**Other Resources**: Resources other than CPU/memory/card types are not limited and don't need configuration.

#### Job Annotations for Card Requests

`volcano.sh/card.name`: Located in `Pod` annotations, specifies the card model name used by specific instances in the task. The scheduler uses the card model from this annotation combined with queue quotas for quota management.

```yaml
metadata:
  annotations:
    volcano.sh/card.name: "NVIDIA-A100"
```

`volcano.sh/card.request` (optional): Located in `PodGroup` annotations, specifies the total requested card model resource quantity for the current task. Used for resource checking to prevent tasks from entering `Inqueue` state, avoiding the scheduler creating too many `Pending` `Pods`.

If `Pods` have already been created, `volcano.sh/card.request` will not take effect, deferring to the actual resources requested by `Pods`, such as `Deployment` type workloads, which directly generate `Pods` after creation without queuing.

```yaml
metadata:
  annotations:
    volcano.sh/card.request: |
      {
        "NVIDIA-A100": 8
      }
```

##### Job Resource Card Requests

K8S resource types must still be configured in `resources`. The Capacity Card plugin cannot inject K8S resource type requests into `Pods`. K8S resource types need to correspond to the card model requested by the task.

```yaml
resources:
  limits:
    nvidia.com/gpu: 1
  requests:
    nvidia.com/gpu: 1
```

##### Node Selection

Node selection is implemented through affinity. The Capacity Card plugin does not perform node filtering.

```yaml
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoreDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: nvidia.com/gpu.product
            operation: In
            values:
            - NVIDIA-A100
```

##### Volcano Job Example

`volcano.sh/card.request` and `volcano.sh/card.name` are new annotations, other fields remain unchanged.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: training-job
  annotations:
    volcano.sh/card.request: |
      {
        "NVIDIA-A100": 8
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
            volcano.sh/card.name: "NVIDIA-A100"
        spec:
          affinity:
            nodeAffinity:
              requiredDuringSchedulingIgnoreDuringExecution:
                nodeSelectorTerms:
                - matchExpressions:
                  - key: nvidia.com/gpu.product
                    operation: In
                    values:
                    - NVIDIA-A100
          containers:
            - name: trainer
              image: training:latest
              resources:
                limits:
                  nvidia.com/gpu: 1
                requests:
                  nvidia.com/gpu: 1
```

##### Deployment Example

`volcano.sh/card.name` is a new annotation, other fields remain unchanged.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inference-deployment
spec:
  replicas: 2
  selector:
    matchLabels:
      app: inference
  template:
    metadata:
      labels:
        app: inference
      annotations:
        volcano.sh/card.name: "NVIDIA-A100"
        scheduling.volcano.sh/queue-name: "cr-queue1"
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoreDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: nvidia.com/gpu.product
                operation: In
                values:
                - NVIDIA-A100
      containers:
        - name: inference
          image: inference:latest
          resources:
            limits:
              nvidia.com/gpu: 1
            requests:
              nvidia.com/gpu: 1
      schedulerName: volcano
```

##### Single-Instance Multi-Card Request Example

To use single-instance multi-card deployment, simply set the `volcano.sh/card.request` and `volcano.sh/card.name` annotations to multi-card configuration, such as `NVIDIA-A100|NVIDIA-H100`, to use either `NVIDIA-A100` or `NVIDIA-H100`.

Note that multiple card types must correspond to the same K8S resource type, and must match the K8S resource type configured in the task's `resources`. Combining card resources with different K8S resource types is not supported. If K8S resource types differ, the K8S resource type in `resources` must match the finally selected card type, but `resources` are determined when the task is created and cannot be dynamically modified during scheduling. For example:

- Whole GPU and Shared card combination: `NVIDIA-A100|NVIDIA-H800/mps-80*1/2`, with K8S resources `nvidia.com/gpu` and `nvidia.com/gpu.shared` respectively
- Different GPU Shared card combination: `NVIDIA-H200/mig-1g.18gb-mixed|NVIDIA-H200/mig-2g.36gb-mixed`, with K8S resources `nvidia.com/mig-1g.18gb` and `nvidia.com/mig-2g.36gb` respectively
- Different card type combination: `NVIDIA-A100|Ascend-910B`, with K8S resources `nvidia.com/gpu` and `huawei.com/ascend-910` respectively

Volcano Job Example

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: training-job
  annotations:
    volcano.sh/card.request: |
      {
        "NVIDIA-A100|NVIDIA-H100": 8
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
            volcano.sh/card.name: "NVIDIA-A100|NVIDIA-H100"
        spec:
          affinity:
            nodeAffinity:
              requiredDuringSchedulingIgnoreDuringExecution:
                nodeSelectorTerms:
                - matchExpressions:
                  - key: nvidia.com/gpu.product
                    operation: In
                    values:
                    - NVIDIA-A100
                    - NVIDIA-H100
          containers:
            - name: trainer
              image: training:latest
              resources:
                limits:
                  nvidia.com/gpu: 1
                requests:
                  nvidia.com/gpu: 1
```

Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inference-deployment
spec:
  replicas: 2
  selector:
    matchLabels:
      app: inference
  template:
    metadata:
      labels:
        app: inference
      annotations:
        volcano.sh/card.name: "NVIDIA-A100|NVIDIA-H100"
        scheduling.volcano.sh/queue-name: "cr-queue1"
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoreDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: nvidia.com/gpu.product
                operation: In
                values:
                - NVIDIA-A100
                - NVIDIA-H100
      containers:
        - name: inference
          image: inference:latest
          resources:
            limits:
              nvidia.com/gpu: 1
            requests:
              nvidia.com/gpu: 1
      schedulerName: volcano
```

### Plugin Configuration

The plugin supports configuration through scheduler configuration:

```yaml
actions: "enqueue, allocate, backfill"
tiers:
  - plugins:
      - name: capacity-card
        arguments:
          cardUnlimitedCpuMemory: true  # Optional: if true, card resources don't need CPU/memory quotas
          nodeOrderWeight: 1.0 # Optional, node ordering weight coefficient
```

**Configuration Options**:
- `cardUnlimitedCpuMemory` (bool, default: false): If set to true, tasks requesting card resources won't be checked against the queue's CPU/memory quota limits. Useful when card resources are the primary constraint.
- `nodeOrderWeight` (float64, default: 1.0): Node ordering weight coefficient, used to adjust the importance of card type priority in scheduling decisions. Must be positive. Larger values increase the impact of card type priority.

### Node Card Discovery

The plugin automatically discovers card resources from node labels and status. The system identifies card types by matching node labels with regex patterns in the format: `<prefix>/<type>.product` (e.g., `nvidia.com/gpu.product`).

#### Node Label and Resource Examples

```yaml
apiVersion: v1
kind: Node
metadata:
  labels:
    # Basic card information (required)
    nvidia.com/gpu.product: "NVIDIA-A100"     # Card product name
    nvidia.com/gpu.count: "8"                 # Physical card count
    nvidia.com/gpu.memory: "81920"            # Memory per card (MB)
    
    # MPS-specific labels (optional)
    nvidia.com/gpu.replicas: "8"              # MPS shared replica count
    
    # MIG-specific labels (optional)
    nvidia.com/mig-1g.5gb.count: "7"          # MIG profile instance count
status:
  allocatable:
    nvidia.com/gpu: "8"                       # Whole card resource count
    nvidia.com/gpu.shared: "64"               # MPS shared resource count (replicas * count)
    nvidia.com/mig-1g.5gb: "7"                # MIG partition resource count
```

#### Card Type Name and Quantity Generation Rules

The plugin automatically generates card type names based on node labels and allocatable resources, used for internal scheduler resource management and quota checking.

##### 1. MPS Shared Cards (Multi-Process Service)

**Card Name Generation**:
- **Format**: `<card-product>/mps-<memory>g*1/<replicas>`
- **Components**:
  - `<card-product>`: Retrieved from `<prefix>/<type>.product` label (e.g., `NVIDIA-A100`)
  - `<memory>`: Retrieved from `<prefix>/<type>.memory` label, converted from MB to GB (rounded)
  - `<replicas>`: Retrieved from `<prefix>/<type>.replicas` label

**Card Quantity Retrieval**:
- Read MPS resource count from `status.allocatable`
- **Resource name**: Fixed as `nvidia.com/gpu.shared` (plugin internal constant `MPSResourceName`)

**Complete Example**:
```yaml
metadata:
  labels:
    nvidia.com/gpu.product: "NVIDIA-A100"
    nvidia.com/gpu.count: "8"
    nvidia.com/gpu.memory: "81920"        # 81920 MB ≈ 80 GB
    nvidia.com/gpu.replicas: "8"
status:
  allocatable:
    nvidia.com/gpu.shared: "64"           # 8 cards × 8 replicas = 64
```
- **Generated card type name**: `NVIDIA-A100/mps-80g*1/8`
- **Card quantity**: `64` (represents a total of 64 MPS instances)

##### 2. MIG Partition Cards (Multi-Instance GPU)

**Card Name Generation**:
- **Format**: `<card-product>/mig-<profile>-mixed`
- **Components**:
  - `<card-product>`: Retrieved from `<prefix>/<type>.product` label (e.g., `NVIDIA-A100`)
  - `<profile>`: Extracted from resource name, the part after removing `nvidia.com/mig-` prefix

**Card Quantity Retrieval**:
- Read corresponding MIG resource count from `status.allocatable`
- **Matching rule**: All resources with names starting with `nvidia.com/mig-`

**Complete Example**:
```yaml
metadata:
  labels:
    nvidia.com/gpu.product: "NVIDIA-A100"
    nvidia.com/gpu.count: "8"
    nvidia.com/gpu.memory: "81920"
    nvidia.com/mig-1g.5gb.count: "7"
    nvidia.com/mig-2g.10gb.count: "4"
status:
  allocatable:
    nvidia.com/mig-1g.5gb: "7"
    nvidia.com/mig-2g.10gb: "4"
```
- **Generated card type names**:
  - `NVIDIA-A100/mig-1g.5gb-mixed` (quantity: 7)
  - `NVIDIA-A100/mig-2g.10gb-mixed` (quantity: 4)

##### 3. Whole Cards (Whole GPU)

**Card Name Generation**:
- Directly uses the value of `<prefix>/<type>.product` label as the card name
- **Matching rule**: Finds labels matching `^((.+?)/(\w+))\.product$`
  - Example: `nvidia.com/gpu.product: "NVIDIA-A100"` → Card name is `NVIDIA-A100`

**Card Quantity Retrieval**:
- Read corresponding resource count from `status.allocatable`
- **Matching rule**: Resources with names starting with the extracted resource prefix (`<prefix>`)
  - Example: `nvidia.com/gpu: "8"` → Card quantity is 8

**Complete Example**:
```yaml
metadata:
  labels:
    nvidia.com/gpu.product: "NVIDIA-A100"
    nvidia.com/gpu.count: "8"
    nvidia.com/gpu.memory: "81920"
status:
  allocatable:
    nvidia.com/gpu: "8"
```
- **Generated card type name**: `NVIDIA-A100`
- **Card quantity**: `8`

#### Card Type Discovery Flow

1. **Label Parsing**: Iterate through node labels, find labels matching the `<prefix>/<type>.product` pattern
2. **Extract Basic Information**: Extract `product`, `count`, `memory`, etc. from labels
3. **Resource Scanning**: Iterate through all resources in `status.allocatable`
4. **Type Determination and Mapping**:
   - If resource name is `nvidia.com/gpu.shared`, generate MPS card type
   - If resource name starts with `nvidia.com/mig-`, generate corresponding MIG card type
   - If resource name starts with the extracted prefix, generate whole card type
5. **Establish Mapping**: Maintain `Card Type Name → Kubernetes Resource Name` mapping for scheduling decisions

### Main Workflows

#### Plugin Initialization (OnSessionOpen)

1. **Build Total Resources**:
   - List all nodes from informer
   - Extract card information from node labels
   - Parse card resources from node status
   - Build mapping: card name → K8s resource name
   - Calculate cluster total resources (CPU, memory, cards)

2. **Build Queue Attributes**:
   - Parse card quotas from queue annotations (`volcano.sh/card.quota`)
   - Calculate queue capability, guarantee, and deserved resources
   - Track allocated, inqueue, and elastic resources for each queue

3. **Register Scheduling Functions**:
   - `JobEnqueueableFn`: Pre-check job card requests against queue quotas
   - `AllocatableFn`: Validate task card allocation against queue quotas
   - `NodeOrderFn`: Score and sort nodes based on card type priority
   - `AllocateFunc` / `DeallocateFunc`: Update queue resource tracking

#### Job Enqueueable Check

When submitting a job:

1. Parse job's card requests from annotations (`volcano.sh/card.request`)
2. Calculate total resources to be used: `allocated + inqueue + job.minResources - elastic`
3. Check CPU/memory quotas (unless `cardUnlimitedCpuMemory` is enabled and task uses card resources)
4. Check card resource quotas:
   - For each requested card type
   - If multi-card request (contains `|`), check each alternative
   - Verify: `totalToBeUsed[cardType] <= queueCapability[cardType]`
5. If all checks pass, mark job as Inqueue and reserve resources

#### Task Allocatable Check

When scheduling a task:

1. Parse task's card request from annotations (`volcano.sh/card.name`)
2. Extract card resources from Pod's resource requests
3. Calculate total resources to be allocated: `allocated + task.request`
4. Check CPU/memory quotas (unless `cardUnlimitedCpuMemory` is enabled and task uses card resources)
5. Check card resource quotas:
   - Support multi-card selection (e.g., `A100|H100`)
   - For multi-card, check each option, succeed if any passes
   - Verify: `totalToBeAllocated[cardType] <= queueCapability[cardType]`
6. If check fails, emit Kubernetes event to Pod with reason

#### Node Ordering

When a task specifies multi-card type selection (e.g., `A100|H100|V100`), the plugin scores nodes to implement card type priority scheduling:

1. Parse card name from task annotation (`volcano.sh/card.name`)
2. Check if it contains `|` separator, if not return score 0 (no priority preference)
3. Split card type list by `|` separator, left side has highest priority
4. Iterate through card type list, check if node has that card type resource
5. Calculate node score:
   - Use exponential decay formula: `baseScore = 100 × (0.5 ^ index)`
     - 0th card type (highest priority): 100 points
     - 1st card type: 50 points
     - 2nd card type: 25 points
     - 3rd card type: 12.5 points
   - Apply weight coefficient: `finalScore = baseScore × nodeOrderWeight`
   - Return score immediately after finding first matching card type
6. Final score combines with scores from other plugins to determine node selection

**Scoring Characteristics**:
- Only multi-card selection tasks participate in node ordering
- Each card type has a unique non-zero score
- Adjust overall influence of card type priority through `nodeOrderWeight` parameter

#### Resource Tracking

The plugin maintains real-time resource tracking:

- **On allocation**: Add task resources to `queue.allocated`
- **On deallocation**: Subtract task resources from `queue.allocated`
- **Queue share calculation**: `share = max(allocated[resource] / deserved[resource])`

### Implementation Details

#### Card Resource Quantification

Card resources are stored as scalar resources in milli-units (multiplied by 1000), consistent with Volcano's internal resource representation: 2 cards → 2000 in scalar resources

#### Multi-Card Request Processing

The plugin supports two modes for multi-card request checking:

##### Task Mode (Task-level Check)

For task allocation, the plugin checks if **any single card type** can satisfy the request, since each task must use only one card type:

For a multi-card request like `A100|H100|V100`:

1. Split by `|` separator
2. For each card type in the list:
   - Clone `toBeUsedResource`
   - Add requested quantity to each individual card name
   - Check `toBeUsedResource[cardType] <= queueCapability[cardType]`
   - If any card type passes, return success
3. If all fail, return error with multi-card name

##### Job Mode (Job-level Check)

For job enqueue, the plugin checks if **the sum of all card type quotas** can satisfy the request, since different tasks in the same job can use different card types:

For a multi-card request like `A100|H100`:

1. Split by `|` separator
2. Calculate sum of available quotas for all card types:
   - `totalAvailableQuota = queueCapability[A100] + queueCapability[H100]`
3. Calculate sum of resources to be used for all card types:
   - `totalToBeUsed = toBeUsedResource[A100] + toBeUsedResource[H100] + requestedQuantity`
4. Check `totalToBeUsed <= totalAvailableQuota`
5. If yes, return success; otherwise, return error

**Example:**
- Queue has: A100=5, H100=3 (total=8)
- Allocated: A100=2, H100=1 (total=3)
- New job requests: `A100|H100` 4 cards
- Job mode check: (2+1+4) = 7 <= 8 ✓ Success
- This allows different tasks in the job to potentially use 3 additional A100s and 1 additional H100

This enhancement allows flexible job submission where different tasks can use different card types, maximizing resource utilization.

#### Event Recording

The plugin emits Kubernetes events for the following situations:
- `GetTaskRequestResourceFailed`: Failed to parse task resource request
- `EmptyQueueCapability`: Queue has no configured capability
- `InsufficientCPUQuota`: Insufficient CPU quota in queue
- `InsufficientMemoryQuota`: Insufficient memory quota in queue
- `InsufficientScalarQuota`: Insufficient card/scalar quota in queue

### Metrics and Observability

The plugin exports Prometheus metrics to track queue resources:
- `volcano_queue_card_deserved`: Deserved card resources per queue
- `volcano_queue_card_allocated`: Currently allocated card resources per queue
- `volcano_queue_card_request`: Requested card resources per queue
- `volcano_queue_card_capacity`: Card capacity per queue

## Example Scenarios

### Example 1: Basic Card Quota

**Cluster Setup**:
- 2 nodes, each with 4 A100 cards (total: 8 A100s)

**Queue Configuration**:
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: team-a
  annotations:
    volcano.sh/card.quota: '{"NVIDIA-A100": 5}'
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
    volcano.sh/card.request: '{"NVIDIA-A100": 4}'
spec:
  queue: team-a
  minAvailable: 4
  tasks:
    - replicas: 4
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100"
        spec:
          containers:
            - name: worker
              resources:
                limits:
                  nvidia.com/gpu: 1
                requests:
                  nvidia.com/gpu: 1
```

**Result**: Job successfully enqueues (4 ≤ 5) and schedules tasks.

### Example 2: Multi-Card Selection

**Job Submission**:
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: flexible-training
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100|NVIDIA-H100": 4}'
spec:
  queue: team-a
  minAvailable: 1
  tasks:
    - replicas: 4
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100|NVIDIA-H100"
        spec:
          containers:
            - name: worker
              resources:
                limits:
                  nvidia.com/gpu: 1
                requests:
                  nvidia.com/gpu: 1
```

**Result**: Scheduler will try to allocate A100 first; if quota is exhausted, will try H100.

### Example 3: MPS Shared GPU

**Node Labels**:
```yaml
nvidia.com/gpu.product: "NVIDIA-A100"
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
    volcano.sh/card.quota: '{"NVIDIA-A100/mps-80g*1/8": 32}'
```

**Job Submission**:
```yaml
metadata:
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100/mps-80g*1/8": 16}'
spec:
  tasks:
    - replicas: 16
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100/mps-80g*1/8"
        spec:
          containers:
            - resources:
                limits:
                  nvidia.com/gpu.shared: 1
```

**Result**: 16 inference Pods share 4 A100 GPUs via MPS.

### Example 4: Node Ordering Configuration

**Scheduler Configuration** (adjust weight to increase card type priority importance):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: capacity-card
        arguments:
          cardUnlimitedCpuMemory: false
          nodeOrderWeight: 2.0    # Increase card type priority influence
```

**Job Submission** (multi-card selection with priority):
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: multi-card-training
  annotations:
    volcano.sh/card.request: '{"NVIDIA-A100|NVIDIA-H100|NVIDIA-T4": 4}'
spec:
  queue: team-a
  minAvailable: 1
  tasks:
    - replicas: 4
      template:
        metadata:
          annotations:
            volcano.sh/card.name: "NVIDIA-A100|NVIDIA-H100|NVIDIA-T4"
        spec:
          affinity:
            nodeAffinity:
              requiredDuringSchedulingIgnoreDuringExecution:
                nodeSelectorTerms:
                - matchExpressions:
                  - key: nvidia.com/gpu.product
                    operation: In
                    values:
                    - NVIDIA-A100
                    - NVIDIA-H100
                    - NVIDIA-T4
          containers:
            - name: trainer
              image: training:latest
              resources:
                limits:
                  nvidia.com/gpu: 1
                requests:
                  nvidia.com/gpu: 1
```

**Node Scoring** (assuming weight of 2.0):
- Nodes with NVIDIA-A100: `100 × 0.5^0 × 2.0 = 200` (highest priority)
- Nodes with NVIDIA-H100: `100 × 0.5^1 × 2.0 = 100` (medium priority)
- Nodes with NVIDIA-T4: `100 × 0.5^2 × 2.0 = 50` (lower priority)

**Result**: Scheduler prefers A100 nodes when resources are available, then H100, finally T4.

## Considerations

1. **Plugin Compatibility**: The Capacity Card plugin is designed to work with other Volcano plugins (gang, priority, etc.). It should not be enabled simultaneously with the standard `capacity` or `proportion` plugins to avoid conflicts.

2. **Card Discovery Requirements**: Node labels must be properly configured (typically by GPU operators like NVIDIA GPU Operator) for card discovery to work correctly.

3. **Annotation-Based Design**: Choosing annotations over native Kubernetes ResourceList allows:
   - Compatibility with scheduler logic
   - More flexible naming conventions
   - Support for multi-card selection syntax
   - Easier evolution without API changes

4. **Multi-Card Scheduling**: The plugin supports flexible multi-card scheduling:
   - **Job Mode** (enqueue phase): Checks sum of multi-card quotas, allowing different tasks in the same job to use different card types
     - Example: Job requests `"A100|H100": 6`, queue has `A100: 4, H100: 4`, enqueue check passes (6 ≤ 4+4)
     - Allows 3 tasks to use A100, 3 tasks to use H100, maximizing resource utilization
   - **Task Mode** (allocation phase): Ensures each individual task uses only one card type
   - This provides optimal resource utilization while maintaining task-level constraints

5. **Node Ordering Weight**: The `nodeOrderWeight` parameter affects the overall scale of node scoring:
   - `> 2.0`: Card type priority dominates scheduling decisions
   - `1.0-2.0`: Card type priority important but balanced (default 1.0)
   - `0.5-1.0`: Other factors (like utilization) more important
   - `< 0.5`: Card type priority has minimal impact

6. **Performance Considerations**: The plugin caches node card information to minimize overhead during scheduling cycles.

## Future Work

- Support preemption and reclamation for card resources
- Support hierarchical queue card quota management

## References

- [Volcano Capacity Scheduling Design](./capacity-scheduling.md)
- [NVIDIA MPS Documentation](https://docs.nvidia.com/deploy/mps/index.html)
- [NVIDIA MIG User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/)
- [Volcano Scheduler Framework](https://volcano.sh/en/docs/)

