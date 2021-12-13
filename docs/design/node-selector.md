## Introduction

This feature allows volcano to schedule workloads based on the Nodes with specific label(not all nodes of k8s cluster). 

In my case, k8s cluster has 10 nodes (10 GPUs per node), 5 for training, 5 for serving, I want to use volcano schedule training job(tfjob/pytorchjob/vcjob) on training nodes. use default-scheduler schedule online pod on serving nodes. 

![](./images/node-selector-1.png)

if you just want to schedule workloads on training nodes, you can use nodeSelector or nodeAffinity on `Pod.Spec`. but it is not properly when considering volcano queue mechanism, because volcano think it can work on 10 node (and use all resources of 10 nodes), it has 100 GPUs. but in fact, it only can work on 5 nodes for training, it has 50 GPUs. 

if there are two queue: queue1 and queue2

||weight|reclaimable|deserved GPUs|
|---|---|---|---|
|queue1|1|true|50|
|queue2|1|true|50|

if queue1 already used 45 GPUs, then I submit a training job of queue2 using 10 GPUs, the job will be pending because there are not enough GPUs on training nodes, and queue1 is not overused in volcano's view, so it will not reclaim job of queue1 to release resource.   

so it is necessary to tell volcano scheduler that it can only work on training nodes(not all nodes in cluster), queue1 can only use 25 GPUs normally, it is overused for queue1 to use 45 GPUs.

![](./images/node-selector-2.png)

so I add nodeSelector as command args for volcano scheduler.

## Usage

in following example, volcano can only work on the node which has `nodeRole:training` or `nodeRole:dev` or `gpuModel: tesla` label. you can use any label name you like.

```yaml
...
spec:
  serviceAccount: volcano-scheduler
  containers:
    - name: volcano-scheduler
      image: xx
      args:
        - --node-selector=nodeRole:training
        - --node-selector=nodeRole:dev
        - --node-selector=gpuModel:tesla
        - --logtostderr
        - --scheduler-conf=/volcano.scheduler/volcano-scheduler.conf
        - -v=5
        - 2>&1
...
```

## Design

parse nodeSelector from command args and transfer the labels into `SchedulerCache.nodeSelectorLabel`
```go
// pkg/scheduler/cache/cache.go
type SchedulerCache struct {
	// added field
    nodeSelectorLabels    map[string]string
}

```
add 'filter logic' into node event handler.
```go
// pkg/scheduler/cache/cache.go
func newSchedulerCache(config *rest.Config, schedulerName string, defaultQueue string, nodeSelector []string) *SchedulerCache {
	...
    sc.nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
        cache.FilteringResourceEventHandler{
            FilterFunc: func(obj interface{}) bool {
                node, ok := obj.(*v1.Node)
                if !ok {
                    klog.Errorf("Cannot convert to *v1.Node: %v", obj)
                    return false
                }
                if !responsibleForNode(node.Name, mySchedulerPodName, c) {
                    return false
                }
                // add code ==========================>
                // filter node by nodeSelector labels
                if len(sc.nodeSelectorLabels) == 0 {
                    return true
                }
                for labelName, labelValue := range node.Labels {
                    key := labelName + ":" + labelValue
                    if _, ok := sc.nodeSelectorLabels[key]; ok {
                        return true
                    }
                }
                klog.Infof("node %s ignore add/update/delete into schedulerCache", node.Name)
                return false
                // add code ==========================>
            },
            Handler: cache.ResourceEventHandlerFuncs{
                AddFunc:    sc.AddNode,
                UpdateFunc: sc.UpdateNode,
                DeleteFunc: sc.DeleteNode,
                },
            },
            0,
        )
    }
...
```