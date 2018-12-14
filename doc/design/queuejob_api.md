# QueueJob API

@jinzhejz, 12/22/2017

## Overview
[Resource sharing architecture for batch and serving workloads in Kubernetes](https://docs.google.com/document/d/1-H2hnZap7gQivcSU-9j4ZrJ8wE_WwcfOkTeAGjzUyLA/edit#) proposed
`QueueJob` feature to run batch job with services workload in Kubernetes. Considering the complexity, the whole batch job proposal was separated into two phase: `Queue` and `QueueJob`. This document presents the API definition of `QueueJob` and feature interaction with `Queue`.

## API
```go
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              QueueJobSpec   `json:"spec"`
	Status            QueueJobStatus `json:"status,omitempty"`
}

type QueueJobSpec struct {
	// Priority of the QueueJob, higher priority QueueJob gets resources first
	Priority     int          `json:"priority"`
	// ResourceUnit * ResourceNo = total resource of QueueJob
	ResourceUnit ResourceList `json:"resourceunit"`
	ResourceNo   int          `json:"resourceno"`
	// The Queue which the QueueJob belongs to
	Queue        string       `json:"queue"`
}

type QueueJobStatus struct {
	// The resources allocated to the QueueJob
	Allocated ResourceList `json:"allocated"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []QueueJob `json:"items"`
}
```

## Function details
### Workflow
![workflow](../images/queuejob.jpg)

It is basically the same as the workflow in Queue API document (`QuotaManager` is not included in above workflow). The difference is just including `QueueJob` in `Queue`.

A `Queue` can include 0 or more `QueueJob`. 

* If a Queue includes 0 QueueJob, its resource request is same as before. Such as `q03` in above.
* If a Queue includes 1 or more QueueJob, the resource request of the queue equals the sum of all QueueJob resource request. Such as `q01` and `q02` in above.

For Queue `q01` and `q02`, Kube-batch will assign resources to their QueueJob directly.
For Queue `q03`, Kube-batch will just assign resources to the Queue.

## Future work
* Now QueueJob is associated not with the real batch job, users who want to submit a batch job need to create their own QueueJob and watch the QueueJob, then submit their batch job after kube-batch assign resources to QueueJob.