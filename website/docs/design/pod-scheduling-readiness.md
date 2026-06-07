# Pod Scheduling Readiness
## Motivation

Pod Scheduling Readiness is a beta feature in Kubernetes v1.27. Users expect Volcano to be aware of it. By specifying/removing a Pod's `.spec.schedulingGates`, which is an array of strings, users can control when a Pod is ready to be considered for scheduling. The scheduling gates are usually removed by external controllers. `kube-scheduler` will only schedule a gated pod once all scheduling gates of it are removed (i.e. the `.spec.schedulingGates` is an empty arry)\. Note that for K8S resources that use `PodTemplate` (Deployment, StatefulSet etc.), if the `.spec.schedulingGates` field of the template is emptied, the gates of all the pods created based on that template will also be removed. 

This feature is often used to implement more performant Quota Managers. K8S have a native ResourceQuota resource, which rejects a pod if it exceed the quota. However, if a quota manager is implemented using ResourceQuota,  one downside is user need to keep polling the api-server until there are sufficient resources the pod is schedulable. To solve this problem, scheduling gates enable users to first create pods that are not yet schedulable, and by removing the gates, it signals kube-scheduler that the pod is ready to be allocated to a node, thus no longer require polling.


## Function Detail
To support Pod Scheduling Readiness, the modifications mainly happen in `scheduler/actions`. In each actions except Enqueue such as Allocation, Preempte, Backfill, the scheduler will skip the tasks of a job whose Pods are scheduling gated, ensuring that a scheduling gated pod will not be bound to a node.




## Feature Interactions
1. **Actions**: Jobs with scheduling gates will first be enqueued by the Enqueue action and transit to the Inqueue state. In the following allocate, preempt actions, tasks with scheduling gated Pod will be skipped, which might result in gang-unschedulable job.
2. **Plugins**: Since scheduling gated pods are not ready to be allocated, we don't want scheduling gated tasks to consume inqueue resources, making other potentially schedulable jobs uninqueuable. Therefore, proportion, capacity and overcommit plugins have to be changed. Since scheduling gated pods will not be allocated, we only need to deduct it from inqueue resources.
3.  **Events and Conditions**: As shown below, when listing Pod with Kubectl, the pods that are shown below will have `SchedulingGated` status, which is the Reason of the condition in pod status. To align with K8S, we need to skip scheduling gated tasks when updating pods conditions after each scheduling cycle and show the original condition created by K8S.
4. **Controllers**: K8S native resources like Deployment support template level removal of scheduling gates. In other words, if a Deployment has scheduling gates in its pod template, by patching the Deployment and remove the scheduling gates, all its pods will be deleted and recreated without gates. However, for Vcjob, currently the job controller cannot detect changes in PodTemplate and cannot support this feature. Despite this, it is uncommon to remove scheduling gates from PodTemplate and the Pod Scheduling Gates feature is usually used at pod level. What's more, scheduling gates are often added by webhooks instead of in the Job template (more details in K8S KEP). Therefore, we choose to not align this behavior with K8S.

## Limitations
1. **Vcjob does not support removing scheduling gates in template**: As mentioned above, currently, if we remove the scheduling gates field in a Vcjob, the gates of its pods are not removed. This behavior of Vcjob is different from native K8S resources. 

2. **Inaccurate message for scheduling gated podgroups**: Currently, if a Job is gang-unschedulable due to gates, its condition is Unschedulable with reason `NotEnoughResources`, which comes from gang plugin. A better message should notify user about the scheduling gates. Moreover, if a K8S resources like Deployment is Pending due to scheduling gates, there is no event. However, for vcjob, we get a gang scheduling failed event each scheduling cycle, which might be a burden of performence.
   
## References
1. K8S Pod Scheduling Readiness Documentation: https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/
2. Pod Scheduling Readiness KEP: https://github.com/kubernetes/enhancements/blob/master/keps/sig-scheduling/3521-pod-scheduling-readiness/README.md
3. K8S Pod Scheduling Readiness Blog: https://kubernetes.io/blog/2022/12/26/pod-scheduling-readiness-alpha/
