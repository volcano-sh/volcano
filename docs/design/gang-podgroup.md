# Gang PodGroups

[@umiaplha](http://github.com/umialpha); Feb 20, 2020

## Motivation
Pods are grouped by PodGroup, but What if we need to group PodGroups?
For example, job1 has 3 tasks with MinMember = 1, job2 has 3 tasks with MinMember = 1, but job1 must co-run with job2.
Currently, we cannot handler this case.
Thus, we indroduce a new feature named `Gang PodGroups`

## Function Detail
A type of string "SubGroup" is introduced into the PodGroupSpec:
```go
type PodGroupSpec struct {
	// SubGroups defines the groups of PodGroup.
	// When PodGroups grouped together, they are scheduled as a whole.
	SubGroup string
}
`SubGroup` is empty by default. If it is, itself builds a JobGroup including only itself.
**Restrict: We only allow PodGroups within the `same namespace` and `same queue` to make up a JobGroup.

```
Also, a new type `JobGroupInfo` is brought in.
```go
// JobGroupInfo has all the jobs within one group
type JobGroupInfo struct {
	UID       JobGroupID
	Namespace string
	Queue     QueueID
    Jobs      map[JobID]*JobInfo
    // TopJob is the job with the highest order
    TopJob    *JobInfo
    // orderFn specify how to compare the job order
	orderFn   LessFn
}

```
### Allocate Action
Currenly the allocation flow is as follows,
``` go
// pseudocode
namespace := Namepaces.Pop()
queue := namespace.Pop()
job := queue.Pop()
for !job.PendingTasks.Empty() {
    task := job.PendingTasks.Pop()
    // try to allocate the task 
    tryAllocate(task)
    if job.JobReady() {
        // push back the the job for the remaning pending tasks
        pushBackJob(job)
        break
    }
}
if job.JobReady() {
    // commit our allocation statement
    statement.Commit()
}else {
    // discard our allocation statement
    statement.Discard()
}
pushBackNamespace(namespace)
```

After change.
``` go
// pseudocode
namespace := Namepaces.Pop()
queue := namespace.Pop()
// we allocate JobGroup as a unit
jobGroup := queue.Pop()
for _, job := jobGroup.Jobs{    
    job := queue.Pop()
    for !job.PendingTasks.Empty() {
        task := job.PendingTasks.Pop()
        // push back the the job for the remaning pending tasks
        tryAllocate(task)
        if job.JobReady() {
            break
    }
    // we break if any job is not ready
    if !job.JobReady() {
        break
    }
}

if jobGroup.Ready() {
    // commit our allocation statement
    statement.Commit()
    )
}else {
    // discard our allocation statement
    statement.Discard()
}

// if jobGroup is ready and there are still remaining tasks un-allocated
// we push back the jobGroup
if jobGroup.Ready() && !taskAllAllocated {
    pushBackJobGroup(jobGroup)
}
pushBackNamespace(namespace)
```

#### JobGroupOrder
JobGroupOrder is determined by its `TopJob`. `TopJob` is determined by the `orderFn`.


#### TODO
This feature is implemented on the Scheduler Level, not on the Controller Level.
Currently, Controller treats a Volcano-Job as One PodGroup.
There are two approaches to enable this feature on the Controller Level.

1.  Still treat one Volcano-Job as One PodGroup. Add a `SubGroup` into the Volcano Job Spec.
    If users want to group jobs, they must submit Two different Volcano-Jobs with the same `SubGroup`.
    Pros: Simple, small modification.
    Cons: Not friendly, not straightforward to users.

2.  Treat each task within Volcano as a PodGroup, all of the tasks belongs to a same `SubGroup`.
    Pros: Friendly and straightforward to users.
    Cons: Big modification.


