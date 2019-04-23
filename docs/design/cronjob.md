# CronJob

[@TommyLike](https://github.com/tommyLike/); April 23, 2019

## Motivation

`CronJob` is designed to help end user to schedule their batch job at some specific time, such as if you want to
schedule a batch job at a low activity period, or just need the job to be executed periodically, it's especially
useful for generating data for test or research. 

## Function Specification

The kubernetes [CronJob](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/) would be a best example when considering the functionality or behaviour of our own batch `CronJob`,
therefore, our API and controller would be much similar to the original `CronJob` in kubernetes except the field that we focus.

### API

```go
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CronJob struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of a CronJob
	Spec CronJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of CronJob
	// +optional
	Status CronJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type CronJobSpec struct {
	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule" protobuf:"bytes,1,opt,name=schedule"`
	// Template is a template based on which the Job should run.

	Template JobSpec `json:"template" protobuf:"bytes,2,opt,name=spec"`
	// Optional deadline in seconds for starting the job if it misses scheduled
	// time for any reason.  Missed jobs executions will be counted as failed ones.
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty" protobuf:"varint,3,opt,name=startingDeadlineSeconds"`

	// Specifies how to treat concurrent executions of a Job.
	// Valid values are:
	// - "Allow" (default): allows CronJobs to run concurrently;
	// - "Forbid": forbids concurrent runs, skipping next run if previous run hasn't finished yet;
	// - "Replace": cancels currently running job and replaces it with a new one
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty" protobuf:"bytes,4,opt,name=concurrencyPolicy,casttype=ConcurrencyPolicy"`

	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions.  Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty" protobuf:"varint,5,opt,name=suspend"`

	// The number of successful finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 3.
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty" protobuf:"varint,6,opt,name=successfulJobsHistoryLimit"`

	// The number of failed finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 1.
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty" protobuf:"varint,7,opt,name=failedJobsHistoryLimit"`
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

// CronJobStatus represents the current state of a cron job.
type CronJobStatus struct {
	// A list of pointers to currently running jobs.
	// +optional
	Active []v1.ObjectReference `json:"active,omitempty" protobuf:"bytes,1,rep,name=active"`

	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty" protobuf:"bytes,4,opt,name=lastScheduleTime"`
}

```

### CronJob Controller

The `CronJobController` will run periodically, watching all `CronJob` resource as well as their batch job resources.
It's responsible for creating/deleting batch job resource according to `CronJobSpec` and current state, also updating
the state of `CronJob`.

### Admission Controller

The admission controller will ensure the correctness of `JobSpec`, especially the `Schedule` expression.

### Feature Interaction

#### cli

Although end usr can manipulate `CronJob` via command `kubectl apply`, `kubectl patch`, `kubectl list` etc, considering
some of them are not convenient, we will add some `vkctl` subcommands to achieve this.

__suspend&resume__:

It's quite common that user need to suspend or resume their Cron jobs, therefore the new command would be:
```$xslt
vkctl cronjob suspend/resume --name <name-of-cronjob> --namespace <namespace>
```

__list__:

`List` command will show all of the Cron Jobs in clusters within namespace.
```$xslt
vkctl cronjob list --namespace <namespace>
```
__jobs__:

`jobs` command will show all related jobs regarding one specific Cron job:

```$xslt
vkctl cronjob jobs --name <name-of-cronjob> --namespace <namespace>
```
the output of this command would be:
```$xslt
NAMESPACE    NAME                STATUS      STARTED_AT       AGE
test        testcronjob-4jn22    COMPLETED   2019-2-14 18:00  5m
test        testcronjob-4jn22    COMPLETED   2019-2-14 19:00  6m
test        testcronjob-4jn22    RUNNING     2019-2-14 20:00  1m
```


