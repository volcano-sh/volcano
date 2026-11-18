In the current design, Volcano adopts a global scheduling policy defined in a centralized configuration file. 
As a result, all queues and jobs within the cluster share the same set of scheduling actions and plugins, 
regardless of their specific characteristics. 
However, with the increasing adoption of diverse workloads such as 
training and inference tasks even-within the same tenant—there is a growing demand for differentiated scheduling strategies. 
To address this, it is essential to introduce support for scheduling policies at the queue-levels.

# 1、Changes to the CRD

## 1.1、Extend the queue CRD

Add the `SchedulerPolicy` in the `QueueSpec`, allowing the queue to define its own scheduling policy configuration.

```go
type QueueSpec struct {
    Weight     int32
    Capability v1.ResourceList
    State QueueState
    Reclaimable *bool
    ExtendClusters []Cluster
    Guarantee Guarantee `json:"guarantee,omitempty" protobuf:"bytes,4,opt,name=guarantee"`
    Affinity *Affinity `json:"affinity,omitempty" protobuf:"bytes,6,opt,name=affinity"`
    Type string `json:"type,omitempty" protobuf:"bytes,7,opt,name=type"`
    Parent string `json:"parent,omitempty" protobuf:"bytes,8,opt,name=parent"`
    Deserved v1.ResourceList `json:"deserved,omitempty" protobuf:"bytes,9,opt,name=deserved"`
    Priority int32 `json:"priority,omitempty" protobuf:"bytes,10,opt,name=priority"`
	
    // Add SchedulerPolicy to point to the scheduling policy defined in the configuration file
    SchedulerPolicy string
}

```

## 1.2、Usage examples of scheduling policies at the queue and job levels

An example for users is as follows:

In the `volcano-scheduler-configmap`, define both the global scheduling policy and queue-level scheduling policies.

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
      - name: priority
      - name: gang
        enablePreemptable: false
      - name: conformance
    - plugins:
      - name: overcommit
      - name: drf
        enablePreemptable: false
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
  SchedulerPolicy-example-1: |
    actions: "enqueue, allocate, backfill"
    tiers:
      - plugins:
          - name: predicates
          - name: binpack
            arguments:
              binpack.weight: 1
              binpack.cpu: 1
              binpack.memory: 1
  SchedulerPolicy-example-2: |
    actions: "enqueue, allocate, backfill"
    tiers:
      - plugins:
          - name: predicates
          - name: priority
          - name: nodeorder
```

An example configuration of a queue-level scheduling policy is shown below:

```yaml
#create test queue and then run deployment.
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: test
spec:
  weight: 1
  reclaimable: false
  capability:
    cpu: 2
  SchedulerPolicy: SchedulerPolicy-example-1
```

## 1.3、Add the PluginRegistry and SchedulerPolicy

Currently, the `session` struct contains many extension points, such as `xxxFn`. 
These functions currently represent global configurations in the session and will also represent plugin-specific configurations in the queue-level and job-level in the future. 

Therefore, we can extract them into a separate struct.

```go
type PluginRegistry struct {
    Tiers          []conf.Tier
    plugins         []Plugin      

    jobOrderFns         map[string]api.CompareFn
    queueOrderFns       map[string]api.CompareFn
    victimQueueOrderFns map[string]api.VictimCompareFn
    taskOrderFns        map[string]api.CompareFn
    clusterOrderFns     map[string]api.CompareFn
    predicateFns        map[string]api.PredicateFn
    prePredicateFns     map[string]api.PrePredicateFn
    bestNodeFns         map[string]api.BestNodeFn
    nodeOrderFns        map[string]api.NodeOrderFn
    batchNodeOrderFns   map[string]api.BatchNodeOrderFn
    nodeMapFns          map[string]api.NodeMapFn
    nodeReduceFns       map[string]api.NodeReduceFn
    preemptableFns      map[string]api.EvictableFn
    reclaimableFns      map[string]api.EvictableFn
    overusedFns         map[string]api.ValidateFn

    preemptiveFns     map[string]api.ValidateWithCandidateFn
    allocatableFns    map[string]api.AllocatableFn
    jobReadyFns       map[string]api.ValidateFn
    jobPipelinedFns   map[string]api.VoteFn
    jobValidFns       map[string]api.ValidateExFn
    jobEnqueueableFns map[string]api.VoteFn
    jobEnqueuedFns    map[string]api.JobEnqueuedFn
    targetJobFns      map[string]api.TargetJobFn
    reservedNodesFns  map[string]api.ReservedNodesFn
    victimTasksFns    map[string][]api.VictimTasksFn
    jobStarvingFns    map[string]api.ValidateFn
}
```

So, the `Session` struct can be shortened.

```go
type Session struct {
    ...
    PluginRegistry
}
```

Directly embed the PluginRegistry in the session to avoid changes to the `(ssn *Session) AddxxxFn`.

Add a new `SchedulerPolicy` to represent the scheduling policy at the queue or job level.

```go
type SchedulerPolicy struct {
    actions []framework.Action
    PluginRegistry
}
```

## 1.4、Add a new map in the session.

Add a new map in the `session` to store `SchedulerPolicy` associated with `SchedulerPolicyName`.

```go
type Session struct {
    ...
    PluginRegistry
    schedulerPolicies   map[string]*SchedulerPolicy
}
```

Likewise, the `SchedulerCache` should also include this new field.

## 1.5、Add Actions in the session.
At present, global actions are configured within the `Scheduler` instead of the `Session`. 
This leads to a limitation where `action.Execute(ssn)` cannot determine which actions should 
be executed for jobs that follow the global scheduling policy. 
To resolve this, the `Session` should be extended to include a set of `actions` representing the `global action configuration`.

```go
type Session struct {
    ...
    PluginRegistry
    schedulerPolicies   map[string]*SchedulerPolicy
    Actions []framework.Action
}
```

```go
func (pc *Scheduler) runOnce() {
    pc.mutex.Lock()
    actions := pc.actions
    plugins := pc.plugins
    configurations := pc.configurations
    pc.mutex.Unlock()

    // Load ConfigMap to check which action is enabled.
    conf.EnabledActionMap = make(map[string]bool)
    for _, action := range actions {
        conf.EnabledActionMap[action.Name()] = true
    }

    ssn := framework.OpenSession(pc.cache, plugins, configurations)
    ssn.Actions = actions   // Move global actions into session
    defer func() {
        framework.CloseSession(ssn)
        metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
    }()

    for _, action := range actions {
        actionStartTime := time.Now()
        action.Execute(ssn)
        metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
    }
}
```

# 2、Changes to the scheduler
## 2.1、Registration of SchedulerPolicy

Currently, in runOnce, `action.Execute(ssn)` executes the action retrieved from the global configuration file.

```go
// scheduler.go
func (pc *Scheduler) runOnce() {
    klog.V(4).Infof("Start scheduling ...")
    scheduleStartTime := time.Now()
    defer klog.V(4).Infof("End scheduling ...")

    pc.mutex.Lock()
    actions := pc.actions
    plugins := pc.plugins
    configurations := pc.configurations
    pc.mutex.Unlock()

    // Load ConfigMap to check which action is enabled.
    conf.EnabledActionMap = make(map[string]bool)
    for _, action := range actions {
        conf.EnabledActionMap[action.Name()] = true
    }
	
    ssn := framework.OpenSession(pc.cache, plugins, configurations)
    defer func() {
        framework.CloseSession(ssn)
        metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
    }()

    for _, action := range actions {
        actionStartTime := time.Now()
        action.Execute(ssn)
        metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
    }
}
```
By modifying `action.Execute(ssn)`, make the action a union of the global action and the queue action to ensure that all queue actions and Job actions are executed.

During the `OpenSession` phase, 
the system constructs a `SchedulerPolicy` object for each policy 
defined in the ConfigMap specified by the schedulerPolicy configuration, 
and stores the corresponding actions and extensions associated with each policy.

Since some users develop their own plugins based on the plugin, we cannot modify the plugin interface.
Therefore, we need to use plugin.OnSessionOpen(ssn) to first register the queue-level or job-level plugin functions into the session, and then save them to `SchedulerPolicy` through an intermediate variable.

```go
func OpenSession(cache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration) *Session {
    ssn := openSession(cache)
    ssn.Tiers = tiers
    ssn.Configurations = configurations
    ssn.NodeMap = GenerateNodeMapAndSlice(ssn.Nodes)
    ssn.PodLister = NewPodLister(ssn)
	
    for SchedulerPolicyName, SchedulerPolicy := range loadSchedulerPolicies(confStr) {
        for _, tier := range SchedulerPolicy.Tiers {
            for _, plugin := range tier.Plugins {
                pb, _ := GetPluginBuilder(plugin.Name)
                plugin := pb(plugin.Arguments)
                plugin.OnSessionOpen(ssn)      // The queue-level or job-level plugin functions are registered into this temporary ssn.
            }       
        }
        SchedulerPolicy.xxxFn = ssn.xxxFn
        ssn.xxxFn = nil
        ssn.schedulerPolicies[SchedulerPolicyName] = SchedulerPolicy
    }
	
  // Global plugin registration is still retained.
    for _, tier := range tiers {
        for _, plugin := range tier.Plugins {
            if pb, found := GetPluginBuilder(plugin.Name); !found {
                klog.Errorf("Failed to get plugin %s.", plugin.Name)
            } else {
                plugin := pb(plugin.Arguments)
                ssn.plugins[plugin.Name()] = plugin
                onSessionOpenStart := time.Now()
                plugin.OnSessionOpen(ssn)
                metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionOpen, metrics.Duration(onSessionOpenStart))
            }
        }
    }
	
    return ssn
}
```

## 2.2、action.execute
Within the `Action.Execute`, 
should determine whether the current job has a job-level scheduling policy 
or whether its associated queue has an enabled queue-level scheduling policy. 
If a matching policy is found, the corresponding `SchedulerPolicy` is applied; 
otherwise, the default scheduling policy is used.

(1) enqueue
```go
// volcano.sh/volcano/pkg/scheduler/actions/enqueue/enqueue.go
func (enqueue *Action) Execute(ssn *framework.Session) {
   
    queues := util.NewPriorityQueue(ssn.QueueOrderFn)
    queueSet := sets.NewString()
    jobsMap := map[api.QueueID]*util.PriorityQueue{}

    // When a job specifies a job-level scheduling policy 
    // or the queue it belongs to specifies a queue-level scheduling policy, 
    // verify whether the scheduling policy has enabled the corresponding action.
    for _, job := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(enqueue.Name()) {
            continue
        }   
		
        if !ssn.Actions.Has(enqueue.Name()) {
            continue
        }   
		
        // Existing enqueue logic…
        if job.IsPending() {
            if _, found := jobsMap[job.Queue]; !found {
                jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
            }
            jobsMap[job.Queue].Push(job)
        }
    }

    // Subsequent logic remains unchanged…
}
```

(2) allocate
```go
// volcano.sh/volcano/pkg/scheduler/actions/allocate/allocate.go
func (alloc *Action) pickUpQueuesAndJobs(queues *util.PriorityQueue, jobsMap map[api.QueueID]*util.PriorityQueue) {
    ssn := alloc.session
    for _, job := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(alloc.Name()) {
            continue
        }

        if !ssn.Actions.Has(enqueue.Name()) {
            continue
        }
		
        // Subsequent logic remains unchanged…
        if job.IsPending() {
            if conf.EnabledActionMap["enqueue"] {
                klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: job status is pending.",
                    job.Namespace, job.Name, job.Queue)
                continue
            } else {
                klog.V(4).Infof("Job <%s/%s> Queue <%s> status update from pending to inqueue, reason: no enqueue action is configured.",
                    job.Namespace, job.Name, job.Queue)
                job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
            }
        }

        if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
            klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
            continue
        }

        if _, found := ssn.Queues[job.Queue]; !found {
            klog.Warningf("Skip adding Job <%s/%s> because its queue %s is not found",
                job.Namespace, job.Name, job.Queue)
            continue
        }

        if _, found := jobsMap[job.Queue]; !found {
            jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
            queues.Push(ssn.Queues[job.Queue])
        }

        klog.V(4).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
        jobsMap[job.Queue].Push(job)
    }
}
```
(3) preempt
```go
// preempt.go
func (pmpt *Action) Execute(ssn *framework.Session) {
    pmpt.parseArguments(ssn)

    preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
    preemptorTasks := map[api.JobID]*util.PriorityQueue{}
    var underRequest []*api.JobInfo
    queues := map[api.QueueID]*api.QueueInfo{}

    for _, job := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(pmpt.Name()) {
            continue
        }

        if !ssn.Actions.Has(enqueue.Name()) {
            continue
        }		
		
        // Subsequent logic remains unchanged…
        if job.IsPending() {
            continue
        }
		
}
```
(4) reclaim
```go
// reclaim.go
func (ra *Action) Execute(ssn *framework.Session) {
    klog.V(5).Infof("Enter Reclaim ...")
    defer klog.V(5).Infof("Leaving Reclaim ...")

    queues := util.NewPriorityQueue(ssn.QueueOrderFn)
    queueMap := map[api.QueueID]*api.QueueInfo{}
    preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
    preemptorTasks := map[api.JobID]*util.PriorityQueue{}

    for _, job := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(ra.Name()) {
            continue
        }

        if !ssn.Actions.Has(enqueue.Name()) {
            continue
        }
		
        // Subsequent logic remains unchanged…
        if job.IsPending() {
            continue
        }
		
}
```
(5) backfill
```go
// backfill.go
func (backfill *Action) pickUpPendingTasks(ssn *framework.Session) []*api.TaskInfo {
    queues := util.NewPriorityQueue(ssn.QueueOrderFn)
    jobs := map[api.QueueID]*util.PriorityQueue{}
    tasks := map[api.JobID]*util.PriorityQueue{}
    var pendingTasks []*api.TaskInfo

    for _, job := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(backfill.Name()) {
            continue
        }

        if !ssn.Actions.Has(enqueue.Name()) {
             continue
        }
		
        // Subsequent logic remains unchanged…
        if job.IsPending() {
            continue
        }
    }

    return pendingTasks
}
```
(6) shuffle
```go
// shuffle.go
func (shuffle *Action) Execute(ssn *framework.Session) {
    klog.V(5).Infoln("Enter Shuffle ...")
    defer klog.V(5).Infoln("Leaving Shuffle ...")
	
    tasks := make([]*api.TaskInfo, 0)
    for _, jobInfo := range ssn.Jobs {
        SchedulerPolicyName := getSchedulerPolicy(ssn, job)
        if SchedulerPolicyName != "" && !ssn.schedulerPolicies[SchedulerPolicyName].Actions.Has(shuffle.Name()) {
            continue
        }

        if !ssn.Actions.Has(enqueue.Name()) {
            continue
        }
		
        // Subsequent logic remains unchanged…
        for _, taskInfo := range jobInfo.Tasks {
            if taskInfo.Status == api.Running {
                tasks = append(tasks, taskInfo)
            }
        }
    }
	
}
```

## 2.3、Execution of extension points

Currently, the functions registered during the OpenSession phase are triggered in the action, but these functions are globally configured.

Taking the action enqueue as an example:

```go
// Current action.Execute()
func (enqueue *Action) Execute(ssn *framework.Session) {
	
    queues := util.NewPriorityQueue(ssn.QueueOrderFn)
    queueSet := sets.NewString()
    ...
	
    for {
        if queues.Empty() {
            break
        }

        queue := queues.Pop().(*api.QueueInfo)

        // skip the Queue that has no pending job
        jobs, found := jobsMap[queue.UID]
        if !found || jobs.Empty() {
            continue
        }
        job := jobs.Pop().(*api.JobInfo)

        if job.PodGroup.Spec.MinResources == nil || ssn.JobEnqueueable(job) {
            ssn.JobEnqueued(job)
            job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
            ssn.Jobs[job.UID] = job
        }

        // Added Queue back until no job in Queue.
        queues.Push(queue)
    }
}
```

In `action.Execute()`, there are functions that were registered earlier, and these functions can be categorized into queue-level and job-level.

- **Queue-level**: For example, `NewPriorityQueue(ssn.QueueOrderFn)`, this function is responsible for maintaining the relationship between multiple queues and can use global configurations without the need for modification.

- **Job-level**: For example, `ssn.JobEnqueued(job)`, this function uses the `JobEnqueued` function registered in the session. Each queue or job can have a different `JobEnqueued`, and the job-level functions are influenced by the different plugins of the queue or job.

So, for job-level extension points, we need to determine whether the job or queue has enabled a `SchedulerPolicy` and, based on that, use the `xxxFn` from `SchedulerPolicy`.

```go
func (ssn *Session) JobEnqueued(obj interface{}) {
    executeJobEnqueuedFns := func(tiers []conf.Tier, jobEnqueuedFns map[string]api.JobEnqueuedFn) {
        for _, tier := range tiers {
            for _, plugin := range tier.Plugins {
                if !isEnabled(plugin.EnabledJobEnqueued) {
                    continue
                }
                fn, found := jobEnqueuedFns[plugin.Name]
                if !found {
                    continue
                }
                fn(obj)
            }
        }
    }

    // If the job is configured with a scheduling policy
    SchedulerPolicyName := getSchedulerPolicy(ssn, obj) 
    if SchedulerPolicyName != nil {
        executeJobEnqueuedFns(ssn.schedulerPolicies[SchedulerPolicyName].Tiers, ssn.schedulerPolicies[SchedulerPolicyName].jobEnqueuedFns)
        return
    }

    // global scheduling policy
    executeJobEnqueuedFns(ssn.Tiers, ssn.jobEnqueuedFns)
}
```

![schedulerPolicy.png](images/schedulerPolicy.png)