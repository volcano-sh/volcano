/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=jobs,shortName=vcjob;vj
// +kubebuilder:subresource:status

// Job defines the volcano job.
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=`.status.state.phase`
// +kubebuilder:printcolumn:name="minAvailable",type=integer,JSONPath=`.status.minAvailable`
// +kubebuilder:printcolumn:name="RUNNINGS",type=integer,JSONPath=`.status.running`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="QUEUE",type=string,priority=1,JSONPath=`.spec.queue`
type Job struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the volcano job, including the minAvailable
	// +optional
	Spec JobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of the volcano Job
	// +optional
	Status JobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// JobSpec describes how the job execution will look like and when it will actually run.
type JobSpec struct {
	// SchedulerName is the default value of `tasks.template.spec.schedulerName`.
	// +kubebuilder:validation:MaxLength=63
	// +optional
	SchedulerName string `json:"schedulerName,omitempty" protobuf:"bytes,1,opt,name=schedulerName"`

	// The minimal available pods to run for this Job
	// Defaults to the summary of tasks' replicas
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"`

	// The volumes mount on Job
	// +optional
	Volumes []VolumeSpec `json:"volumes,omitempty" protobuf:"bytes,3,opt,name=volumes"`

	// Tasks specifies the task specification of Job
	// +kubebuilder:validation:MinItems=1
	// +optional
	Tasks []TaskSpec `json:"tasks,omitempty" protobuf:"bytes,4,opt,name=tasks"`

	// Specifies the default lifecycle of tasks
	// +optional
	Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,5,opt,name=policies"`

	// Specifies the plugin of job
	// Key is plugin name, value is the arguments of the plugin
	// +optional
	Plugins map[string][]string `json:"plugins,omitempty" protobuf:"bytes,6,opt,name=plugins"`

	// Running Estimate is a user running duration estimate for the job
	// Default to nil
	RunningEstimate *metav1.Duration `json:"runningEstimate,omitempty" protobuf:"bytes,7,opt,name=runningEstimate"`

	// Specifies the queue that will be used in the scheduler, "default" queue is used this leaves empty.
	// +kubebuilder:default:="default"
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	Queue string `json:"queue,omitempty" protobuf:"bytes,8,opt,name=queue"`

	// Specifies the maximum number of retries before marking this Job failed.
	// Defaults to 3.
	// +kubebuilder:default:=3
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxRetry int32 `json:"maxRetry,omitempty" protobuf:"bytes,9,opt,name=maxRetry"`

	// ttlSecondsAfterFinished limits the lifetime of a Job that has finished
	// execution (either Completed or Failed). If this field is set,
	// ttlSecondsAfterFinished after the Job finishes, it is eligible to be
	// automatically deleted. If this field is unset,
	// the Job won't be automatically deleted. If this field is set to zero,
	// the Job becomes eligible to be deleted immediately after it finishes.
	// +kubebuilder:validation:Minimum=0
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty" protobuf:"varint,10,opt,name=ttlSecondsAfterFinished"`

	// If specified, indicates the job's priority.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty" protobuf:"bytes,11,opt,name=priorityClassName"`

	// The minimal success pods to run for this Job
	// +kubebuilder:validation:Minimum=1
	// +optional
	MinSuccess *int32 `json:"minSuccess,omitempty" protobuf:"varint,12,opt,name=minSuccess"`

	// NetworkTopology defines the NetworkTopology config, this field works in conjunction with network topology feature and hyperNode CRD.
	// +optional
	NetworkTopology *NetworkTopologySpec `json:"networkTopology,omitempty" protobuf:"bytes,13,opt,name=networkTopology"`
}

// NetworkTopologyMode represents the networkTopology mode, valid values are "hard" and "soft".
// +kubebuilder:validation:Enum=hard;soft
type NetworkTopologyMode string

const (
	// HardNetworkTopologyMode represents a strict network topology constraint that jobs must adhere to.
	HardNetworkTopologyMode NetworkTopologyMode = "hard"

	// SoftNetworkTopologyMode represents a flexible network topology constraint that allows jobs
	// to cross network boundaries under certain conditions.
	SoftNetworkTopologyMode NetworkTopologyMode = "soft"
)

type NetworkTopologySpec struct {
	// Mode specifies the mode of the network topology constrain.
	// +kubebuilder:default=hard
	// +optional
	Mode NetworkTopologyMode `json:"mode,omitempty" protobuf:"bytes,1,opt,name=mode"`

	// HighestTierAllowed specifies the highest tier that a job allowed to cross when scheduling.
	// +kubebuilder:validation:Minimum=0
	// +optional
	HighestTierAllowed *int `json:"highestTierAllowed,omitempty" protobuf:"bytes,2,opt,name=highestTierAllowed"`

	// HighestTierName specifies the highest tier name that a job allowed to cross when scheduling.
	// HighestTierName and HighestTierAllowed cannot be set simultaneously.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	HighestTierName string `json:"highestTierName,omitempty" protobuf:"bytes,3,opt,name=highestTierName"`
}

// VolumeSpec defines the specification of Volume, e.g. PVC.
type VolumeSpec struct {
	// Path within the container at which the volume should be mounted.  Must
	// not contain ':'.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[^:]+$`
	MountPath string `json:"mountPath" protobuf:"bytes,1,opt,name=mountPath"`

	// defined the PVC name
	// +kubebuilder:validation:MaxLength=253
	// +optional
	VolumeClaimName string `json:"volumeClaimName,omitempty" protobuf:"bytes,2,opt,name=volumeClaimName"`

	// VolumeClaim defines the PVC used by the VolumeMount.
	// +optional
	VolumeClaim *v1.PersistentVolumeClaimSpec `json:"volumeClaim,omitempty" protobuf:"bytes,3,opt,name=volumeClaim"`
}

// JobEvent job event.
type JobEvent string

const (
	// CommandIssued command issued event is generated if a command is raised by user
	CommandIssued JobEvent = "CommandIssued"
	// PluginError  plugin error event is generated if error happens
	PluginError JobEvent = "PluginError"
	// PVCError pvc error event is generated if error happens during IO creation
	PVCError JobEvent = "PVCError"
	// PodGroupError  pod grp error event is generated if error happens during pod grp creation
	PodGroupError JobEvent = "PodGroupError"
	//ExecuteAction action issued event for each action
	ExecuteAction JobEvent = "ExecuteAction"
	//JobStatusError is generated if update job status failed
	JobStatusError JobEvent = "JobStatusError"
	// PodGroupPending  pod grp pending event is generated if pg pending due to some error
	PodGroupPending JobEvent = "PodGroupPending"
)

// LifecyclePolicy specifies the lifecycle and error handling of task and job.
type LifecyclePolicy struct {
	// The action that will be taken to the PodGroup according to Event.
	// One of "Restart", "None".
	// Default to None.
	// +optional
	Action v1alpha1.Action `json:"action,omitempty" protobuf:"bytes,1,opt,name=action"`

	// The Event recorded by scheduler; the controller takes actions
	// according to this Event.
	// +optional
	Event v1alpha1.Event `json:"event,omitempty" protobuf:"bytes,2,opt,name=event"`

	// The Events recorded by scheduler; the controller takes actions
	// according to this Events.
	// +optional
	Events []v1alpha1.Event `json:"events,omitempty" protobuf:"bytes,3,opt,name=events"`

	// The exit code of the pod container, controller will take action
	// according to this code.
	// Note: only one of `Event` or `ExitCode` can be specified.
	// +optional
	ExitCode *int32 `json:"exitCode,omitempty" protobuf:"bytes,4,opt,name=exitCode"`

	// Timeout is the grace period for controller to take actions.
	// Default to nil (take action immediately).
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty" protobuf:"bytes,5,opt,name=timeout"`
}

// +kubebuilder:validation:Enum=none;best-effort;restricted;single-numa-node
type NumaPolicy string

const (
	None           NumaPolicy = "none"
	BestEffort     NumaPolicy = "best-effort"
	Restricted     NumaPolicy = "restricted"
	SingleNumaNode NumaPolicy = "single-numa-node"
)

// TaskSpec specifies the task specification of Job.
type TaskSpec struct {
	// Name specifies the name of tasks
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Replicas specifies the replicas of this TaskSpec in Job
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

	// The minimal available pods to run for this Task
	// Defaults to the task replicas
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinAvailable *int32 `json:"minAvailable,omitempty" protobuf:"bytes,3,opt,name=minAvailable"`

	// Specifies the pod that will be created for this TaskSpec
	// when executing a Job
	// +optional
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,4,opt,name=template"`

	// Specifies the lifecycle of task
	// +optional
	Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,5,opt,name=policies"`

	// Specifies the topology policy of task
	// +optional
	TopologyPolicy NumaPolicy `json:"topologyPolicy,omitempty" protobuf:"bytes,6,opt,name=topologyPolicy"`

	// Specifies the maximum number of retries before marking this Task failed.
	// Defaults to 3.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxRetry int32 `json:"maxRetry,omitempty" protobuf:"bytes,7,opt,name=maxRetry"`

	// Specifies the tasks that this task depends on.
	// +optional
	DependsOn *DependsOn `json:"dependsOn,omitempty" protobuf:"bytes,8,opt,name=dependsOn"`

	// PartitionPolicy defines the partition policy of a task.
	// +optional
	PartitionPolicy *PartitionPolicySpec `json:"partitionPolicy,omitempty" protobuf:"bytes,9,opt,name=partitionPolicy"`
}

type PartitionPolicySpec struct {
	// TotalPartitions indicates how many groups a set of pods within a task is divided into.
	// The product of TotalPartitions and PartitionSize should be equal to Replicas.
	// +kubebuilder:validation:Minimum=1
	TotalPartitions int32 `json:"totalPartitions" protobuf:"bytes,1,opt,name=totalPartitions"`

	// PartitionSize is the number of pods included in each group.
	// +kubebuilder:validation:Minimum=1
	PartitionSize int32 `json:"partitionSize" protobuf:"bytes,2,opt,name=partitionSize"`

	// NetworkTopology defines the NetworkTopology config, this field works in conjunction with network topology feature and hyperNode CRD.
	// +optional
	NetworkTopology *NetworkTopologySpec `json:"networkTopology,omitempty" protobuf:"bytes,3,opt,name=networkTopology"`

	// MinPartitions defines the minimum number of sub-affinity groups required.
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinPartitions int32 `json:"minPartitions,omitempty" protobuf:"bytes,4,opt,name=minPartitions"`
}

// JobPhase defines the phase of the job.
type JobPhase string

const (
	// Pending is the phase that job is pending in the queue, waiting for scheduling decision
	Pending JobPhase = "Pending"
	// Aborting is the phase that job is aborted, waiting for releasing pods
	Aborting JobPhase = "Aborting"
	// Aborted is the phase that job is aborted by user or error handling
	Aborted JobPhase = "Aborted"
	// Running is the phase that minimal available tasks of Job are running
	Running JobPhase = "Running"
	// Restarting is the phase that the Job is restarted, waiting for pod releasing and recreating
	Restarting JobPhase = "Restarting"
	// Completing is the phase that required tasks of job are completed, job starts to clean up
	Completing JobPhase = "Completing"
	// Completed is the phase that all tasks of Job are completed
	Completed JobPhase = "Completed"
	// Terminating is the phase that the Job is terminated, waiting for releasing pods
	Terminating JobPhase = "Terminating"
	// Terminated is the phase that the job is finished unexpected, e.g. events
	Terminated JobPhase = "Terminated"
	// Failed is the phase that the job is restarted failed reached the maximum number of retries.
	Failed JobPhase = "Failed"
)

// JobState contains details for the current state of the job.
type JobState struct {
	// The phase of Job.
	// +kubebuilder:validation:Enum=Pending;Aborting;Aborted;Running;Restarting;Completing;Completed;Terminating;Terminated;Failed
	// +optional
	Phase JobPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

	// Unique, one-word, CamelCase reason for the phase's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`

	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`

	// Last time the condition transit from one phase to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`
}

// TaskState contains details for the current state of the task.
type TaskState struct {
	// The phase of Task.
	// +optional
	Phase map[v1.PodPhase]int32 `json:"phase,omitempty" protobuf:"bytes,11,opt,name=phase"`
}

// JobStatus represents the current status of a Job.
type JobStatus struct {
	// Current state of Job.
	// +optional
	State JobState `json:"state,omitempty" protobuf:"bytes,1,opt,name=state"`

	// The minimal available pods to run for this Job
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"`

	// The status of pods for each task
	// +optional
	TaskStatusCount map[string]TaskState `json:"taskStatusCount,omitempty" protobuf:"bytes,21,opt,name=taskStatusCount"`

	// The number of pending pods.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,3,opt,name=pending"`

	// The number of running pods.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,4,opt,name=running"`

	// The number of pods which reached phase Succeeded.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Succeeded int32 `json:"succeeded,omitempty" protobuf:"bytes,5,opt,name=succeeded"`

	// The number of pods which reached phase Failed.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Failed int32 `json:"failed,omitempty" protobuf:"bytes,6,opt,name=failed"`

	// The number of pods which reached phase Terminating.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Terminating int32 `json:"terminating,omitempty" protobuf:"bytes,7,opt,name=terminating"`

	// The number of pods which reached phase Unknown.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Unknown int32 `json:"unknown,omitempty" protobuf:"bytes,8,opt,name=unknown"`

	//Current version of job
	// +kubebuilder:validation:Minimum=0
	// +optional
	Version int32 `json:"version,omitempty" protobuf:"bytes,9,opt,name=version"`

	// The number of Job retries.
	// +kubebuilder:validation:Minimum=0
	// +optional
	RetryCount int32 `json:"retryCount,omitempty" protobuf:"bytes,10,opt,name=retryCount"`

	// The job running duration is the length of time from job running to complete.
	// +optional
	RunningDuration *metav1.Duration `json:"runningDuration,omitempty" protobuf:"bytes,11,opt,name=runningDuration"`

	// The resources that controlled by this job, e.g. Service, ConfigMap
	// +optional
	ControlledResources map[string]string `json:"controlledResources,omitempty" protobuf:"bytes,12,opt,name=controlledResources"`

	// Which conditions caused the current job state.
	// +optional
	// +patchMergeKey=status
	// +patchStrategy=merge
	Conditions []JobCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"status" protobuf:"bytes,13,rep,name=conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// JobList defines the list of jobs.
type JobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Job `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// JobCondition contains details for the current condition of this job.
type JobCondition struct {
	// Status is the new phase of job after performing the state's action.
	Status JobPhase `json:"status" protobuf:"bytes,1,opt,name=status,casttype=JobPhase"`
	// Last time the condition transitioned from one phase to another.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,2,opt,name=lastTransitionTime"`
}

// Iteration defines the phase of the iteration.
type Iteration string

const (
	// Indicates that when there are multiple tasks,
	// as long as one task becomes the specified state,
	// the task scheduling will be triggered
	IterationAny Iteration = "any"
	// Indicates that when there are multiple tasks,
	// all tasks must become the specified state,
	// the task scheduling will be triggered
	IterationAll Iteration = "all"
)

// DependsOn represents the tasks that this task depends on and their dependencies
type DependsOn struct {
	// Indicates the name of the tasks that this task depends on,
	// which can depend on multiple tasks
	// +optional
	Name []string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// This field specifies that when there are multiple dependent tasks,
	// as long as one task becomes the specified state,
	// the task scheduling is triggered or
	// all tasks must be changed to the specified state to trigger the task scheduling
	// +optional
	Iteration Iteration `json:"iteration,omitempty" protobuf:"bytes,2,opt,name=iteration"`
}

// ConcurrencyPolicy describes how the job will be handled.
// Only one of the following concurrent policies may be specified.
// If none of the following policies is specified, the default one
// is AllowConcurrent.
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows CronJobs to run concurrently.
	AllowConcurrent ConcurrencyPolicy = "Allow"

	// ForbidConcurrent forbids concurrent runs, skipping next run if previous
	// hasn't finished yet.
	ForbidConcurrent ConcurrencyPolicy = "Forbid"

	// ReplaceConcurrent cancels currently running job and replaces it with a new one.
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// JobTemplateSpec describes the data a Job should have when created from a template
type JobTemplateSpec struct {
	// Standard object's metadata of the jobs created from this template.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the job.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec JobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// CronJobSpec defines the desired state of Cronjob
type CronJobSpec struct {
	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule" protobuf:"bytes,1,opt,name=schedule"`

	// The time zone name for the given schedule, see https://en.wikipedia.org/wiki/List_of_tz_database_time_zones.
	// If not specified, this will default to the time zone of the kube-controller-manager process.
	// The set of valid time zone names and the time zone offset is loaded from the system-wide time zone
	// database by the API server during CronJob validation and the controller manager during execution.
	// If no system-wide time zone database can be found a bundled version of the database is used instead.
	// If the time zone name becomes invalid during the lifetime of a CronJob or due to a change in host
	// configuration, the controller will stop creating new new Jobs and will create a system event with the
	// reason UnknownTimeZone.
	// More information can be found in https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#time-zones
	// +optional
	TimeZone *string `json:"timeZone,omitempty" protobuf:"bytes,2,opt,name=timeZone"`

	// Optional deadline in seconds for starting the job if it misses scheduled
	// time for any reason.  Missed jobs executions will be counted as failed ones.
	// +kubebuilder:validation:Minimum=0
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty" protobuf:"varint,3,opt,name=startingDeadlineSeconds"`

	// Specifies how to treat concurrent executions of a Job.
	// Valid values are:
	// - "Allow" (default): allows CronJobs to run concurrently;
	// - "Forbid": forbids concurrent runs, skipping next run if previous run hasn't finished yet;
	// - "Replace": cancels currently running job and replaces it with a new one
	// +kubebuilder:default=Allow
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty" protobuf:"bytes,4,opt,name=concurrencyPolicy,casttype=ConcurrencyPolicy"`

	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions.  Defaults to false.
	// +kubebuilder:default=false
	// +optional
	Suspend *bool `json:"suspend,omitempty" protobuf:"varint,5,opt,name=suspend"`

	// Specifies the job that will be created when executing a CronJob.
	JobTemplate JobTemplateSpec `json:"jobTemplate" protobuf:"bytes,6,opt,name=jobTemplate"`

	// The number of successful finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 3.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty" protobuf:"varint,7,opt,name=successfulJobsHistoryLimit"`

	// The number of failed finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 3.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty" protobuf:"varint,8,opt,name=failedJobsHistoryLimit"`
}

// CronJobStatus represents the current state of a cron job.
type CronJobStatus struct {
	// A list of pointers to currently running jobs.
	// +optional
	// +listType=atomic
	Active []v1.ObjectReference `json:"active,omitempty" protobuf:"bytes,1,rep,name=active"`

	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty" protobuf:"bytes,2,opt,name=lastScheduleTime"`

	// Information when was the last time the job successfully completed.
	// +optional
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty" protobuf:"bytes,3,opt,name=lastSuccessfulTime"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=cronvcjob;cronvj
// +kubebuilder:subresource:status

// CronJob is the Schema for the cronjobs API
type CronJob struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of a cron job, including the schedule.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec CronJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of a cron job.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status CronJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CronJobList contains a list of Cronjob
type CronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []CronJob `json:"items" protobuf:"bytes,2,rep,name=items"`
}
