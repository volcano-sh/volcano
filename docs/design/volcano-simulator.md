# Volcano Simulator

## Background
Volcano Simulator provides a simulation platform for Volcano scheduler, with the goal of creating a realistic simulation 
environment for evaluating the performance of Volcano scheduler. 
Through this platform, users can build simulated clusters containing thousands or even tens of thousands of nodes
in a short period of time and run various tests on them, such as performance stress testing and throughput testing.

### Applicable scenario
Volcano Simulator mainly has the following scenarios:
- Users want to create large-scale clusters with minimal resources.
- Users can submit configuration file in yaml format to define the properties of the simulated cluster.
- Users can submit test tasks declaratively and return test result reports on demand. 

### Goal
Our goal is very clear. At this stage, we will achieve the following capabilities:
- Provides a binary tool `vcsimctl` for cluster creation and test task submission.
- Implement a command `vcsimctl create cluster --config=cluster.yaml`, which can create a simulated cluster with 
  large number of fake nodes, and users can define the resource size of the fake nodes in `cluster.yaml` config.
- Implement a command `vcsimctl run --test=test-case.yaml`, which can run a general throughput testing, 
  and users can define the test case in yaml config.

### Non Goal
As long as we have cluster simulation capabilities and have completed general throughput testing, 
we can theoretically support any complex test cases. This requirement focuses on building the simulation framework, 
not spending excessive effort on constructing numerous test cases.

## Example
### Step 1
Users define the following cluster in `cluster.yaml`, containing node number and node capacity:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: SimulatorCluster
metadata:
  name: kind-simulator  # cluster name
spec:
  nodes:
  - name: big-node    # Node name prefix
    count: 1000       # Node number
    capacity:         # Node capacity
      cpu: "128"      # CPU
      memory: 256Gi   # Memory
      pods: "110"     # Max Pod number
    allocatable:      # Node allocatable
      cpu: "128"
      memory: 256Gi
      pods: "110"
```
  
### Step 2
Users run create cluster command:

```shell
vcsimctl create cluster --config=cluster.yaml
```

Then, users will see a real control plane cluster which is implemented by Kind and 1000 fake nodes which are simulated by Kwok.

```shell
$ kubectl get node
kind-simulator-control-plane   Ready,SchedulingDisabled   master   6d5h   v1.19.16
fake-big-node-zq7wp            Ready                      worker   6d5h   v1.18.1
fake-big-node-zrtfh            Ready                      worker   6d5h   v1.18.1
fake-big-node-zsjmb            Ready                      worker   6d5h   v1.18.1
fake-big-node-zsv7j            Ready                      worker   6d5h   v1.18.1
fake-big-node-zswjx            Ready                      worker   6d5h   v1.18.1
fake-big-node-ztnb5            Ready                      worker   6d5h   v1.18.1
fake-big-node-zxls4            Ready                      worker   6d5h   v1.18.1
  ...
```

In control plane cluster, we will install the Volcano scheduler.

In each node, we will ensure the actual resource capacity aligns with the declared configuration specifications:

```shell
$kubectl describe node fake-big-node-zq7wp
...
Capacity:
  cpu:     128
  memory:  256Gi
  pods:    110
Allocatable:
  cpu:     128
  memory:  256Gi
  pods:    110
...
```

### Step 3

Users define the following test case in test-case.yaml to measure the scheduler's processing capacity
under high load, specifically the number of Pods it can schedule per unit time.


```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: SimulatorJob
metadata:
  name: scheduling-throughput  # test case name
spec:
  namespaces: 1
  replicasPerNamespace: 2
  timing:
    qps: 100
    duration: 60
  objects:
    - name: test-pod
      objectPath: "deployment.yaml"
```

### Step 4
Users run test command:

```shell
vcsimctl run --test=test-case.yaml
```

Users shall see the test result report:

```shell
Test Finished
Test: /home/simu/test-case.yaml
Status: Success
--------------------------------------------------------------------------------
Measurement: SchedulingThroughput, Report: {
  "perc50": 279.2,
  "perc90": 282.2,
  "perc99": 282.2,
  "max": 282.2
}
Test report file path: /home/simu/.vcsimctl/kind-simulator/report/SchedulingThroughput__2025-07-15T22:25:21+08:00.json
```

## Design Details

### API Declaration

#### SimulatorCluster

```Go
// SimulatorCluster defines cluster-level simulation parameters
type SimulatorCluster struct {
    metav1.TypeMeta   `json:",inline"`  // API version and kind
    metav1.ObjectMeta `json:"metadata"` // Standard object metadata

    Spec SimulatorClusterSpec `json:"spec"` // Desired cluster configuration
}

// SimulatorClusterSpec contains cluster configuration details
type SimulatorClusterSpec struct {
    Nodes []NodeSpec `json:"nodes"` // List of node configurations
}

// NodeSpec defines properties for simulated nodes
type NodeSpec struct {
    Name        string              `json:"name"`        // Node name prefix
    Count       int                 `json:"count"`       // Number of nodes to simulate
    Capacity    corev1.ResourceList `json:"capacity"`    // Total node resources
    Allocatable corev1.ResourceList `json:"allocatable"` // Allocatable resources
}
```

#### SimulatorJob

```Go
// SimulatorJob defines the volcano scheduler performance test job
type SimulatorJob struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              SimulatorJobSpec `json:"spec,omitempty"`
}

// SimulatorJobSpec contains test configuration parameters
type SimulatorJobSpec struct {
    Namespaces           int64        `json:"namespaces"`           // Number of namespaces to simulate
    ReplicasPerNamespace int64        `json:"replicasPerNamespace"` // Pod replicas per namespace
    Timing               TimingConfig `json:"timing"`               // Request rate control settings
    Objects              []ObjectRef  `json:"objects"`              // List of K8s objects to simulate
}

// TimingConfig controls request rate during simulation
type TimingConfig struct {
    QPS      float64 `json:"qps"`      // Queries per second (request rate)
    Duration float64 `json:"duration"` // Test duration in seconds
}

// ObjectRef references a Kubernetes manifest file
type ObjectRef struct {
    Name       string `json:"name"`       // Unique identifier for the object
    ObjectPath string `json:"objectPath"` // Path to YAML manifest file
}
```

### Core  Implementation

#### Simulate Scheduler
We use Kind to install a real k8s control plane cluster and exactly deploy the Volcano scheduler in the cluster.
This makes our simulation environment closer to the Volcano scheduler under real conditions, 
and thus obtain more accurate performance test reports.

#### Simulate Node
We can use KWOK (Kubernetes WithOut Kubelet), which enables effortless mocking of thousands of
highly realistic fake nodes with minimal resource consumption and near-instantaneous creation time. 

It achieves this by simulating node existence and core lifecycle behaviors (like ready/not-ready states, labels, taints, conditions)
directly within the Kubernetes API server, bypassing the need for actual kubelets, pods, or underlying compute resources. 
Simply define the desired node characteristics and scale using KWOK's configuration, 
and it instantly populates the cluster with these virtual nodes, allowing rapid testing of scheduling, 
autoscaling, and cluster operations at massive scale without the overhead of real infrastructure.

#### Throughput Test Objective
This test measures scheduler throughput by tracking:
1. Pods Scheduled per Second (Pods/sec)
   - The number of Pods scheduled each second.
   - Goal: Higher values indicate stronger scheduler performance.
2. Cumulative Pods Scheduled (Total)
   - The total count of pods scheduled during the test.
   - Goal: Verifies test integrity and full workload completion.

#### Throughput Calculation
1. Check for events: verify there is at least one event of pod scheduling. If none exist, return an error immediately.
2. Determine the time window:
   - Use the first event's timestamp as startTime.
   - Use the last event's timestamp as endTime.
3. Calculate duration: Convert the time difference between endTime and startTime to seconds.
4. Compute throughput: Divide the total event count by the duration to get the rate in pods per second.

## Project Plan
- [ ] 2025-07-13: Build the main framework of the vcsimctl binary command
- [ ] 2025-07-18: Implement cluster config API and how vcsimctl parse cluster config
- [ ] 2025-07-23: Implement vcsimctl create control plane cluster by Kind
- [ ] 2025-07-28: Implement vcsimctl create fake node by Kwok
- [ ] 2025-08-02: Implement test case API and how vcsimctl parse test case config
- [ ] 2025-08-07: Supplement the metrics of the scheduling process
- [ ] 2025-08-10: Implement preparation work before vcsimctl run test case
- [ ] 2025-08-15: Implement vcsimctl run throughput testing case
- [ ] 2025-08-20: Implement vcsimctl generate test result report
- [ ] 2025-08-25: Contribute user guide document
