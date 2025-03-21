Currently, Volcano’s scheduling strategy relies on global scheduler configurations, and the relevant functions in Action are at the Session level rather than the plugin level, so significant changes need to be made. This specifically involves two aspects: the definition of CRD and the scheduling logic.

# 1、Changes to the CRD

## 1.1、Extend the queue CRD

Add the `plugins` field in the `QueueSpec`, allowing the queue to define its own scheduling policy configuration.
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
	
    // add Plugins
    Plugins     []PluginConfig
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
  ai-training-queue: |
    plugins:
      - name: priority
        args:
          weight: 10
      - name: gang
        args:
          minAvailable: 5
  batch-queue: |
    plugins:
      - name: nodeorder
        args:
          resourceWeights:
            cpu: 1
            memory: 0.5
```

## 1.2、Add the PluginRegistry structure

Currently, the `session` struct contains many extension points, such as `xxxFn`. These functions currently represent global configurations in the session and will also represent plugin-specific configurations in the queue in the future. 

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


# 2、Changes to the scheduler

## 2.1、OpenSession

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

## 2.2、action

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

The purpose of these changes is to achieve the following effects:

- **Queue A**: Uses the priority plugin
- **Queue B**: Uses the gang plugin

![queue-level-scheduling-policy.png](images/queue-level-scheduling-policy.png)

