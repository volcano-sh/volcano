Currently, Volcano’s scheduling strategy relies on global scheduler configurations, 
and the relevant functions in Action are at the Session level rather than the plugin level, 
so significant changes need to be made. This specifically involves two aspects: the definition of CRD and the scheduling logic.

# 1、Changes to the CRD

## 1.1、Extend the queue CRD

Add the `plugins` and `Actions` in the `QueueSpec`, allowing the queue to define its own scheduling policy configuration.
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
	
    // add Actions and Plugins
    Actions []string
    Plugins []PluginConfig
}

type PluginConfig struct {
    // plugin name
    Name string 
    // plugin args
    Args map[string]interface{} 
}
```

An example for users is as follows:

**Option One:**

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: newQueue
spec:
  weight: 10
  actions:
    - enqueue
    - allocate
    - preempt
  plugins:
    - name: priority  
      args:
        weight: 20    
    - name: gang     
      args:
        minAvailable: 5
    - name: nodeorder 
```

**Option Two:**

By configuring a globally unique ConfigMap to store queue and queue-level scheduling plugin configurations.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-queue-policies
  namespace: volcano-system
data:
  # Queue-level policy (Key is the queue name).
  queue1: |
    actions:
      - enqueue    
      - allocate   
      - preempt    
    plugins:
      - name: priority
        args:
          weight: 10
      - name: gang
        args:
          minAvailable: 5
  queue2: |
    actions:
      - enqueue    
      - allocate      
    plugins:
      - name: nodeorder
        args:
          resourceWeights:
            cpu: 1
            memory: 0.5
```

## 1.2、Add the PluginRegistry structure

Currently, the `session` struct contains many extension points, such as `xxxFn`. 
These functions currently represent global configurations in the session and will also represent plugin-specific configurations in the queue in the future. 

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
I’m not sure about the name of this struct yet, but it might be something like PluginRegistry | PluginManager | ExtensionSet | ...

So, the `Session` struct can be shortened.

```go
type Session struct {
    ...
    PluginRegistry
}
```

Directly embed the PluginRegistry in the session to avoid changes to the `(ssn *Session) AddxxxFn`.


## 1.3、Add PluginRegistry to the QueueInfo.

```go
type QueueInfo struct {
    UID  QueueID
    Name string
    Weight int32
    Weights string
    Hierarchy string
    Queue *scheduling.Queue

    // Add PluginRegistry to represent the plugins and functions associated with the queue.
    PluginRegistry
}
```

## 1.4、Add Actions to the QueueInfo

```go
type QueueInfo struct {
    UID  QueueID
    Name string
    Weight int32
    Weights string
    Hierarchy string
    Queue *scheduling.Queue

    // Add PluginRegistry to represent the plugins and functions associated with the queue.
    PluginRegistry
    // Add  actions  for the queue.
    Actions []framework.Action 
}
```

# 2、Changes to the scheduler
## 2.1、queue-level actions
### 2.1.1、Retrieve action.
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
By modifying `action.Execute(ssn)`, make the action a union of the global action and the queue action to ensure that all queue actions are executed.

In the OpenSession phase, record the actions from the Queue CRD into queueInfo.

```go
// session.go
func OpenSession(cache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration) *Session {
    ssn := openSession(cache)
    
    // Initialize the actions for the queue.
    for _, queueInfo := range ssn.Queues {
        enabledActions := []framework.Action
         for _, action := range queueInfo.Queue.Spec.Actions {
            enabledActions.Insert(actionMap[action])
        }
        queueInfo.Actions = enabledActions
    }
    
    // Other initialization logic…
    return ssn
}
```

### 2.1.2、action.execute
In the Execute method of each action, 
iterate through the jobs and directly check whether the action is enabled for the queue the job belongs to. 
If not enabled, skip the job.

(1) enqueue
```go
// volcano.sh/volcano/pkg/scheduler/actions/enqueue/enqueue.go
func (enqueue *Action) Execute(ssn *framework.Session) {
   
    queues := util.NewPriorityQueue(ssn.QueueOrderFn)
    queueSet := sets.NewString()
    jobsMap := map[api.QueueID]*util.PriorityQueue{}

    // Iterate through all jobs, but only process jobs whose associated queue has “enqueue” enabled.
    for _, job := range ssn.Jobs {
        // Check whether the queue has “enqueue” enabled.
        queue := ssn.Queues[job.Queue]
        if !queue.Actions.Has(enqueue.Name()) {
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
        // New: Check whether the queue has “allocate” enabled.
        queue := ssn.Queues[job.Queue]
        if !queue.Actions.Has(alloc.Name()) {
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
        // // New: Check whether the queue has “preempt” enabled.
        queue := ssn.Queues[job.Queue]
        if !queue.Actions.Has(pmpt.Name()) {
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
        // New: Check whether the queue has “reclaim” enabled.
        queue := ssn.Queues[job.Queue]
        if !queue.Actions.Has(ra.Name()) {
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
        // New: Check whether the queue has “backfill” enabled.
        queue := ssn.Queues[job.Queue]
        if !queue.Actions.Has(backfill.Name()) {
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

	// select pods that may be evicted
	tasks := make([]*api.TaskInfo, 0)
	for _, jobInfo := range ssn.Jobs {
		// New: Check whether the queue has “shuffle” enabled.
		queue := ssn.Queues[jobInfo.Queue]
		if !queue.Actions.Has(shuffle.Name()) {
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

## 2.2、queue-level plugins
### 2.2.1、OpenSession

Since some users develop their own plugins based on the plugin, we cannot modify the plugin interface.
Therefore, we need to use plugin.OnSessionOpen(ssn) to first register the queue-level plugin functions into the session, and then save them to QueueInfo through an intermediate variable.

```go
func OpenSession(cache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration) *Session {
    ssn := openSession(cache)
    ssn.Tiers = tiers
    ssn.Configurations = configurations
    ssn.NodeMap = GenerateNodeMapAndSlice(ssn.Nodes)
    ssn.PodLister = NewPodLister(ssn)

    for queueID, QueueInfo := ssn.Queues {
        for _, tier := range QueueInfo.tiers {
            for _, plugin := range tier.Plugins {
                pb, _ := GetPluginBuilder(plugin.Name)
                plugin := pb(plugin.Arguments)
                QueueInfo.Plugins[plugin.Name()] = plugin
                plugin.OnSessionOpen(ssn)      // The queue-level plugin functions are registered into this temporary ssn.
            }       
        }
        QueueInfo.xxxFn = ssn.xxxFn
        ssn.xxxFn = nil
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

Since we cannot modify plugin.OnSessionOpen(ssn) and users don’t want to know whether their plugin.OnSessionOpen is registered at the global or plugin level, the only way is to copy the xxxFn registered in ssn to pluginInfo.

## 2.2.2、action

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

- **Job-level**: For example, `ssn.JobEnqueued(job)`, this function uses the `JobEnqueued` function registered in the session. Each queue can have a different `JobEnqueued`, and the job-level functions are influenced by the different plugins of the queue.

So, for job-level extension points, we need to determine whether the queue has enabled a queue-level scheduling strategy and, based on that, use the `xxxFn` from QueueInfo.

```go
func (ssn *Session) JobEnqueued(obj interface{}) {
    // If the queue is configured with a scheduling strategy,
    if len(ssn.Queues[obj.Queue].Tiers) != 0 {
        for _, tier := range ssn.Queues[obj.Queue].Tiers {
            for _, plugin := range tier.Plugins {
                if !isEnabled(plugin.EnabledJobEnqueued) {
                    continue
                }
                fn, found := ssn.Queues[obj.Queue].jobEnqueuedFns[plugin.Name]
                if !found {
                    continue
                }
				
                fn(obj)
            }       
        } 
        return
    }
	
    for _, tier := range ssn.Tiers {
        for _, plugin := range tier.Plugins {
            if !isEnabled(plugin.EnabledJobEnqueued) {
                continue
            }
            fn, found := ssn.jobEnqueuedFns[plugin.Name]
            if !found {
                continue
            }
			
            fn(obj)
        }
    }
}
```

