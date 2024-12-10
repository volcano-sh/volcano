# Network Topology Aware Scheduling

Author: William Wang, Peng Gu, Kevin Wang, Klaus Ma, Xuzheng Chang

# Motivation

In the LLM training scenario, the model parallels have extremely high requirements for the network throughput to exchange data, making the networking to be a bottleneck. There are diverse network in the datacenter, e.g. IB, RoCE, Nvswitch and in each type of network, there might be multiple levels of switch having different throughput and latency. Use requires that the workload can be scheduled to the best performance domain with highest throughput and lowest latency to accelerate data exchanging for training.

## Use Case 1

1. A training Job: 8 GPU per Pod \* \~3k (5k) Pods  
   1. MPI, PyTorch …  
2. 1 BF3, 8 CX7, 8 H100

![network topology usecase1](images/network-topology-aware/network-topology-usecase1.png)

## 

3. Schedule the job, prefer scheduling all pods to one tier1 topology zone, if not enough nodes, try to schedule all the pods to one tier2 topology zone.  
4. Gang scheduling is also needed in this case, to make sure all pods are able to proceed with their work. 

## Use Case 2

1. LLM training job:  16 NPU per pod, 3000 pod per job

![network topology usecase2](images/network-topology-aware/network-topology-usecase2.png)

2. There are 3 network control planes, which are VPC, Roce and HCCS.  
3. The pods in Job are expected to be grouped and the pods belonging to the same group have higher demand for the network bandwidth. The tensor parallels are performed on pods in the same group.  
![network topology usecase3](images/network-topology-aware/network-topology-usecase3.png)

4. The pods belonging to the same group are required to be scheduled to HCCS topology zone.  
5. Prefer to schedule the pod group to one HCCS topology zone, if not enough nodes, try to schedule to the RoCE topology zone and then the VPC topology zone.  
6. Gang scheduling is required in this case, to make sure all pods are able to proceed with their work.

# Scope:

In Scope:

* support clos network topology define and management  
* support network topology aware scheduling for Volcano job  
* support spine-leaf network

# Function Detail

### network topology management

Option 1: Describe network topology by labels, there's a proposal in upstream  
Cons: 
* complicated to construct tree for the topology information  

Option 2: Describe network topology by CRD, NetworkTopology in Volcano → plugin/scheduling

- label \-\> NetworkTopology   
- Rest API (NV, HW) \-\> NetworkTopology

Pros: 
  * easier for debugging:  
  * engineers don’t need to construct the tree of topology info by themselves.   
  * the scheduler uses the same as engineers can see.

* Components besides scheduler are able to use the CR as well

#### network topology definition

`HyperNode` is a performance domain which consists of a group of nodes or sub-performance domains.The network bandwidth and latency is the same in one HyperNode. This CRD is used to describe the network topology in Kubernetes cluster.

`Tier` is a way to distinguish different performance domains. The bandwidth and latency are the same in one tier. The smaller the value of the tier, the higher the bandwidth. For example, compute-network and storage network can be in different tiers, or in the compute-network, there are several levels of spine, leaf switches, each level can be identified as a tier.

```go
type HyperNode struct {
	metav1.TypeMeta `json:”,inline”`
	metav1.ObjectMeta `json:”metadata, omitempty”`

	Spec HyperNodeSpec `json:”spec”`
	Status HyperNodeStatus `json:”status”`
}

type HyperNodeSpec struct {
	tier string	`json:"tier,omitempty"`
	members []MemberSpec	`json:"members,omitempty"`
}

type MemberSpec struct {
	name string	`json:"name,omitempty"`
	type string	`json:"type,omitempty"`
}

type HyperNodeStatus struct {
	……
}

```

Here are two examples:

Example 1:  
Here is a spine-leaf tree, there are three levels of switches, connecting the 8 nodes.
![network topology example1](images/network-topology-aware/network-topology-example1.png)

```go
version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s0
spec:
   tier: 1
   members:
    name: node0
    name: node1

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s1
spec:
  HyperNode
   tier: 1
   members:
    name: node2
    name: node3

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s2
spec:
  HyperNode
   tier: 1
   members:
    name: node4
    name: node5

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s3
spec:
  HyperNode
   tier: 1
   members:
    name: node6
    name: node7

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s4
spec:
   tier: 2
   members:
    name: s0
    name: s1
version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s5
spec:
   tier: 2
   members:
    name: s2
    name: s3

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: s6
spec:
   tier: 3
   members:
    name: s4
    name: s5

```

Example 2:  
There are two roce networks in one Kubernetes cluster which are roce-network0 and roce-network1, and there are two nvlink-networks in each roce network. Each nvlink-network has 4 hosts. The CR is like this.
![network topology example2](images/network-topology-aware/network-topology-example2.png)

```go
version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: roce-network0
spec:
tier: 2
	members:
		- name: nvlink-network0
		  type: HyperNode
		- name: nvlink-network1
		  type: HyperNode

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: roce-network1
spec:
	tier: 2
	members:
		- name: nvlink-network2
		  type: HyperNode
		- name: nvlink-network3
		  type: HyperNode

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: nvlink_network-0
spec:
   tier: 1
   members:
    name: node1
    type: Node
    name:node2
    type: Node

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: nvlink_network-1
spec:
   tier: 1
   members:
    name: node3
    type: Node
    name:node4
    type: Node

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: nvlink_network-2
spec:
   tier: 1
   members:
    name: node5
    type: Node
    name:node6
    type: Node

version: topology.volcano.sh/v1alpha1
kind: HyperNode
name: nvlink_network-3
spec:
   tier: 1
   members:
    name: node7
    type: Node
    name: node8
    type: Node
```

#### network topology generation and updation

* **Network topology discovery/detection tool**: a tool to generate network topology CR by analyzing labels, system file or API of HW vendor. The community will offer a tool to generate CR by label.  
![nework topology generation](images/network-topology-aware/nework-topology-generation.png)    

### Job management

HyperJob is an API for managing a group of replicated volcano jobs, which aligns with HyperNode/SuperPod for optimal LLM training.

```go
type HyperJob struct {  
	metav1.TypeMeta `json:",inline"\`

	// +optional  
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"\`

	// Specification of the desired behavior of the volcano Hyperjob, including the minAvailable  
	// +optional  
	Spec HyperJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"\`

	// Current status of the volcano HyperJob  
	// +optional  
	Status HyperJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"\`  
}

type HyperJobSpec struct {  
	// The minimal available Job to run for this HyperJob  
	// Defaults to the summary of jobs' replicas  
	// +optional  
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"\`  
	  
	Jobs []JobSepc `json:"jobs,omitempty" protobuf:"bytes,4,opt,name=jobs"\`  
	  
...  
}
```

##### Volcano Job sample:

```go
apiVersion: batch.volcano.sh/v1alpha1  
kind: Job  
metadata:  
  name: pytorch-job  
spec:  
  minAvailable: 1  
  schedulerName: volcano  
  // describes the network topology scheduling requirement of jobs  
  // example: hard, with highest-tier-allowed: 1, means for each job-foo, scheduler needs to find a tier1 to run all the job-foo pods.   
  networkTopologies:  
  - mode: hard 
    highestTierAllowed: 1
  plugins:  
    pytorch: ["--master=master","--worker=worker","--port=23456"] # Pytorch plugin register  
  tasks:  
    - replicas: 1  
      name: master  
      policies:  
        - event: TaskCompleted  
          action: CompleteJob  
      template:  
        spec:  
          containers:  
            - image: gcr.io/kubeflow-ci/pytorch-dist-sendrecv-test:1.0  
              imagePullPolicy: IfNotPresent  
              name: master  
          restartPolicy: OnFailure  
    - replicas: 2  
      name: worker  
      template:  
        spec:  
          containers:  
            - image: gcr.io/kubeflow-ci/pytorch-dist-sendrecv-test:1.0  
              imagePullPolicy: IfNotPresent  
              name: worker  
              workingDir: /home  
          restartPolicy: OnFailure
```

##### HyperJob sample:

```go
apiVersion: [batch.volcano.sh/v1aplha1](http://batch.volcano.sh/v1aplha1)  
kind: HyperJob  
metadata:  
    name: multi-volcano-job  
spec:  
    networkTopologies: 
    - mode: hard** // we don’t really need to explicitly indicate the soft requirements, we can make it as a default algorithm behavior. And we need to describe the default behavior in the field comment  
      highestTierAllowed: 2**  
    replicatedJobs:  
    - replicas: 40  
      name: volcano-job  
      template:  
           // describes the network topology scheduling requirement of jobs  
           // example: hard, with highest-tier-allowed: 1, means for each job-foo, scheduler needs to find a tier1 to run all the job-foo pods.   
        networkTopologies:
        - mode: hard
          highestTierAllowed: 1
         schedulerName: volcano  
         tasks:  
         - replicas: 32  
           name: “worker”  
           template:  
              spec:  
                  containers:  
                  - name: worker  
                    image: alpine  
                    command: [“/bin/sh”, “-c”, “sleep 3600”]  
                    imagePullPolicy: IfNotPresent  
                    resources:  
                       requests:  
                          gpu: 8  
                       limits:  
                          gpu: 8
```

## Implementation

### Overview

Phase 1 without hyperJob supported  
![network topology implementation 01](images/network-topology-aware/network-topology-implementation-01.png)

Phase 2 with hyperJob supported  
![network topology implementation 02](images/network-topology-aware/network-topology-implementation-02.png)  
A naive way to build nodes by tier as an input of volcano scheduler, scheduler traverses sequentially from low to high tier, the selected nodeGroups of a hyperJob should be in one same tier because we start from the lowest tier, and will stop to search the upper tier when current tier can meet hyperJob/Job’s resources.  
eg:  
```go
tier0: [][] *node{ {node0,node1}, {node2,node3},{node4,node5}, {node6,node7} }  
tier1: [][] *node{ {node0,node1,node2,node3}, {node4,node5,node6,node7} }  
tier2: [][] *node{ {node0,node1,node2,node3, node4,node5,node6,node7} }
```

### API

HyperNode:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +genclient:nonNamespaced
// +kubebuilder:resource:path=hypernodes,shortName=hn,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="NodeCount",type=integer,JSONPath=`.status.nodeCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// HyperNode represents a collection of nodes sharing similar network topology or performance characteristics.
type HyperNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired configuration of the HyperNode.
	// +optional
	Spec HyperNodeSpec `json:"spec"`

	// Status provides the current state of the HyperNode.
	// +optional
	Status HyperNodeStatus `json:"status,omitempty"`
}

// HyperNodeSpec defines the desired state of a HyperNode.
type HyperNodeSpec struct {
	// Tier categorizes the performance level of the HyperNode.
	// +required
	Tier string `json:"tier,omitempty"`

	// Members defines a list of node groups or individual nodes included in the HyperNode.
	// +optional
	Members []MemberSpec `json:"members,omitempty"`
}

// MemberSpec represents a specific node or a hyperNodes in the hyperNode.
type MemberSpec struct {
	// Type specifies the member type (e.g., "Node", "HyperNode").
	// +kubebuilder:validation:Enum=Node;HyperNode
	// +required
	Type string `json:"type,omitempty"`

	// Selector defines the selection rules for this member.
	// +optional
	Selector MemberSelector `json:"selector,omitempty"`
}

// MemberSelectorType specifies the selection method.
type MemberSelectorType string

const (
	// ExactMatchMemberSelectorType matches nodes exactly by name.
	ExactMatchMemberSelectorType MemberSelectorType = "Exact"

	// RegexMatchMemberSelectorType matches nodes based on a regular expression.
	RegexMatchMemberSelectorType MemberSelectorType = "Regex"
)

// +kubebuilder:validation:XValidation:rule="self.type == 'Exact' ? has(self.exactMatch) : true",message="ExactMatch must be specified when Type is 'Exact'"
// +kubebuilder:validation:XValidation:rule="self.type == 'Regex' ? has(self.regexMatch) : true",message="RegexMatch must be specified when Type is 'Regex'"

// MemberSelector defines the criteria for selecting nodes.
//
// Example for Exact match:
//
//	members:
//	- selector:
//	    type: Exact
//	    exactMatch:
//	      name: "node1"
//
// Example for Regex match:
//
//	members:
//	- selector:
//	    type: Regex
//	    regexMatch:
//	      pattern: "^node-[0-9]+$"
type MemberSelector struct {
	// Type specifies the selection method (Exact or Regex).
	// +kubebuilder:validation:Enum=Exact;Regex
	// // +required
	Type MemberSelectorType `json:"type"`

	// ExactMatch defines the exact match criteria (required when Type is "Exact").
	// +optional
	ExactMatch *ExactMatch `json:"exactMatch,omitempty"`

	// RegexMatch defines the regex match criteria (required when Type is "Regex").
	// +optional
	RegexMatch *RegexMatch `json:"regexMatch,omitempty"`
}

// ExactMatch represents the criteria for exact name matching.
type ExactMatch struct {
	// Name specifies the exact name of the node to match.
	// +optional
	Name string `json:"name"`
}

// RegexMatch represents the criteria for regex-based matching.
type RegexMatch struct {
	// Pattern defines the regex pattern to match node names.
	// +optional
	Pattern string `json:"pattern"`
}

// HyperNodeStatus represents the observed state of a HyperNode.
type HyperNodeStatus struct {
	// Conditions provide details about the current state of the HyperNode.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// NodeCount is the total number of nodes currently in the HyperNode.
	// +kubebuilder:validation:Minimum=0
	NodeCount int64 `json:"nodeCount,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// HyperNodeList contains a list of HyperNode resources.
type HyperNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of HyperNodes.
	Items []HyperNode `json:"items"`
}
```

auto generate code for job, hyperJob, hyperNode,  
refer the API repo [volcano-sh/apis: The API (CRD) of Volcano (github.com)](https://github.com/volcano-sh/apis) and add CRDs, generate codes and submit to the repo.

### Scheduler

**configuration:**

* allow users to configure the network topology policy in job  
* allow users to enable/disable the network-topology-aware plugin in scheduler configmap


**action:** allocate  
Allocate resources for queue-\> hyperJob \-\> Job \-\> Task，

**plugin:** NetworkTopology

* AddJobGroupReadyFn: check whether hyperJob minavailable is met.  
* AddNodeGroupOrderFn: score for hyperNodes.(hard limit, binpack or spread strategy)  
* AddNodeOrderFn: score for nodes.(soft limit, closest tiers have higher score)

### Webhook

Add admission for job in file pkg/webhooks/admission/jobs/validate/admit\_job.go, this can be done by CRD definition.

* job.spec.network-topologies.mode must in \[“soft”,”hard”\]  
* job.spec.network-topologies**.**highest-tier-allowed must be set when job.spec.network-topologies.mode==hard

### Controller

Phase1:  
Support unified interface to register hyperNode controllers by vendor.  
Phase2:  
Support hyperJob, create jobs when hyperJob is created.

### Effort and plan

Phase1:  
Volcano job Webhook & API: 0.25 man-month    
Controller: 0.5 man-month   
Scheduler: 0.75 man-month

Phase2:  
HyperJob API: 0.25 man-month  
Controller: 0.5 man-month  
Scheduler: 0.25 man-month

## Feature Interaction

## Performance

Build nodeGroups list from hyperNode CRD may increase the time complexity when there are too many nodes and switches

## GUI/CLI

topology-generator : a new tool/controller to generate topology CR by labels, a new tool to display the hierarchical switches and nodes intuitively.

# References

meeting records:  
[https://docs.google.com/document/d/10r\_8iAayFMAnZXl5ImanKCpoNMlwz8pZ25nS2rQ7uyc/edit?tab=t.0](https://docs.google.com/document/d/10r_8iAayFMAnZXl5ImanKCpoNMlwz8pZ25nS2rQ7uyc/edit?tab=t.0) 
