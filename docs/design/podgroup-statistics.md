# PodGroup Statistics

## Backgrounds

Each time when podgroups states changed, the controller will update the statistics of podgroup of each state in the queue's status. 
And at the end of each scheduling session, the volcano scheduler will also update the allocated filed in queue's status to recored 
the amount of the amount of resources allocated. Both components use `UpdateStatus` api to update the queue status, which will cause
conflict errors. When the controller encounter such an error, it will trigger `AddRateLimited` to push back the podgroup into work queue, 
resulting in accumulation of memory leak. See in issue #3597: https://github.com/volcano-sh/volcano/issues/3597.

## Alternative
Currently the statistics of podgroups of each state are only used for display by vcctl, there is no need to be persisted in queue's status. 
So when users need to use `vcctl queue get -n [name]` or `vcctl list` to display queues and each state of podgroups in queue, 
vcctl should calculate podgroup statistics in client side and then display them. And we can export these statistics of podgroups in each state as metrics.  

## Implementation
- In `syncQueue` of queue controller, counts of podgroups in each state will not be persisted in queue status anymore, 
instead these statistics will be recorded as metrics and then be exported directly: https://github.com/volcano-sh/volcano/blob/c9be5c4c934597d99a0a80c9b26a3e919bbf8877/pkg/controllers/queue/queue_controller_action.go#L41-L61. 
And `UpdateStatus` should not be used here: https://github.com/volcano-sh/volcano/blob/c9be5c4c934597d99a0a80c9b26a3e919bbf8877/pkg/controllers/queue/queue_controller_action.go#L84-L87, 
the `UpdateStatus` interface will verify the resourceVersion in the apiserver, which may cause concurrent update conflicts. 
Instead, `ApplyStatus` should be used here to avoid this situation, because we only need to update the status of the queue. 
It should be noted that the controller currently does not have patch queue status permissions, so we should add a patch queue/status permission to the clusterrole.
- `vcctl get -n [name]` and `vcctl list` display the statistics of podgroups in each state from queue's status directly, 
instead we should do one more step, query the podgroups owend in the queue, stat the counts of podgroups in each state at the `vcctl` side, and then display them. 
- Those metrics called `queue_pod_group_[state]_count`, which are recorded in proportion/capacity plugins in scheduler, 
now will be moved to controller to record. The metrics involved are as follows:
     - `queue_pod_group_inqueue_count`: The number of Inqueue PodGroup in this queue
     - `queue_pod_group_pending_count`: The number of Pending PodGroup in this queue
     - `queue_pod_group_running_count`: The number of Running PodGroup in this queue
     - `queue_pod_group_unknown_count`: The number of Unknown PodGroup in this queue
     - And a new metric is added to record the number of Completed PodGroup in the queue called `queue_pod_group_completed_count`

## Notice
- The statistical fields of podgroups in each state are still retained in queue status, but vc-controller no longer updates them. 
**If the plugin written by the user relies on these fields in the queue status, the logic of the plugin needs to be modified before 
upgrading to the latest version of volcano**. The user can refer to this code implementation to modify your plugin, detail of the code 
can be found in pkg/cli/queue/get.go. `PodGroupStatistics` records the number of podgroups in each state, which is the same as the 
previous fields in queue status. We need to check all podgroups first, query which podgroups are in the queue, and then record the statistics. 
*If the user is using k8s v1.30+, you can enable the `CustomResourceFieldSelectors` featuregate to directly filter the podgroups contained 
in the queue in kube-apiserver without having to query all podgroups*.
```go
pgList, err := queueClient.SchedulingV1beta1().PodGroups("").List(ctx, metav1.ListOptions{})
if err != nil {
    ...
}

pgStats := &PodGroupStatistics{}
for _, pg := range pgList.Items {
    if pg.Spec.Queue == queue.Name {
        pgStats.StatPodGroupCountsForQueue(&pg)
    }
}
```
- The queue controller in vc-controller still has podgroup cache, the cache is still necessary to use these cache data to check whether the queue can be closed, 
and to stat the number of podgroups in each state and export them as metrics.
- The metrics called `queue_pod_group_inqueue_count`, `queue_pod_group_pending_count`, `queue_pod_group_running_count`, `queue_pod_group_unknown_count` 
belonging to scheduler before now are moved to vc-controller instead. If the user still needs to query these metrics, 
vc-controller needs to be added as the scrape address, so that there is no need to modify PromQL.