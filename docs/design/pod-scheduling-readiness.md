# Pod Scheduling Readiness
## Motivation

Pod Scheduling Readiness is a beta feature in Kubernetes v1.27. Users expect Volcano to be aware of it. By specifying/removing a Pod's `.spec.schedulingGates`, which is an array of strings, users can control when a Pod is ready to be considered for scheduling. The scheduling gates are usually removed by external controllers. `kube-scheduler` will only schedule a gated pod once all scheduling gates of it are removed (i.e. the `.spec.schedulingGates` is an empty arry)\. Note that for K8S resources that use `PodTemplate` (Deployment, StatefulSet etc.), if the `.spec.schedulingGates` field of the template is emptied, the gates of all the pods created based on that template will also be removed. 

The following example illustrates why support for Scheduling Readiness is needed in Volcano. Suppose we have implemented an external quota manager responsible for reviewing all incoming pod requests for capacity/quota requirements. Only once these requests receive approval from the quota manager are they considered eligible for scheduling. The pods schedulingGates feature can be handy when implementing this funtionality\.
## Function Detail
To support Pod Scheduling Readiness, the modifications mainly happen in `scheduler/actions`. In each action, we want to skip jobs with pods that are scheduling gated. To achieve this, we propose adding a new state `PodGroupSchGated`. The diagram below shows how this new state is used: The transition to this state happens when a Job is in `Inqueue` state. When a Job is in `Inqueue` state, the Tasks (Pods) of this job have been created and we can judge whether the job is scheduling gated based on tasks. If one of the tasks is gated, then the Job is transitioned to `SchGated` state and will no longer be allocated to a node in the allocate action. In other actions (preempt/reclaim), we will also skip Jobs that are scheduling gated.
![Transition between states of PodGroup](./images/pod-scheduling-readiness.png)

## Feature Interactions
1. **API:** The state of a PodGroup, `PodGroupPhase` is defined in a separate api repo. Before making changes to the main repo, we need to first modify the api repo.
```
const (
//...

// PodGroupSchGated means the spec.SchedulingGates of at least one of the Pods of the job is not empty.
// PodGroupSchGated is transitioned from Inqueue and back to Inqueue once spec.SchedulingGates is empty.
PodGroupSchGated PodGroupPhase = "SchGated"

//...
)

```
2. **Plugins**: Scheduler Plugins such as proportion  will register functions to determine whether a PodGroup is enqueueable or allocatable. These functions calculate the resources already used in the cluster based on states of each PodGroup. For example, the proportion plugin determines whether a task is enqueuable when `sum(Inqueue)+sum(Running)+cur_job<TotalResource+elastic`. Since the scheduling gated jobs do not occupy resources in the cluster, by having the scheduling gated job in `SchGated` rather than `Inqueue`, they will not be summed up when calculating total used resources, which reflects the actual situation.
3. **Actions**: Transition to `SchGated` happens to and from `Inqueue` first in the allocation action. Then, other actions will skip the gated pods.
4. **Controllers**: One alternative design of state transition is to directly transition from `Pending` to `SchGated`. However, this is not possible because Controllers only create Pods for a job once it is inqueued (see [delayed-pod-creation](./delay-pod-creation.md)). We can only know a Pod is scheduling gated in the Inqueue state. The state transitions of a newly created Pod are the following: initially it is `Pending`; After there are enough resources for it to be scheduled, it will transition to `Inqueue` in enqueue action. Only for Jobs in `Inqueue` status, Pods of the job will be created in api-server by controller. Once the Pods of the Job are created, if some of the Pods have scheduling gates, it will be transitioned to `SchGated` state and transitioned back to `Inqueue` once all gates are removed.
5.  **Validation**: The job validation webhook(`webhooks/admission/jobs/validate`) will check if modifying a certain field in a Job is allowed, and currently it only allows modification of `minAvailable`, `tasks[*]`. We need to add permission for scheduling gates as well.
## Granularity of Pod Scheduling Readiness
Pod scheduling readiness is a field of the spec of a pod. However, Volcano Jobs(vcjob) consists of many tasks, each corresponding to a Pod created from possibly differnt Pod templates. It is possible that some of these pods are scheduling gated while others are not. To align the granularity of the Pod Scheduling Readiness and Volcano Job, we see scheduling gates as a property of a Job: as long as there is one gated pod, the job is scheduling gated. This is consistent with the workload of Volcano: most Volcano Jobs need to run as a whole and cannot be partially run. This constraint do not apply to normal Kubernetes resources, such as `Deployment`/`StatefulSet`, since unlike Volcano Jobs, they can only include one type of pod template.

[TODO]::
## Observability
K8S use `Recorder`s to report events related to resources. After a scheduling cycle is finished, the recorder in `SchedulerCache` is used to report whether and why each Job is successfully scheduled or not. After adding support for scheduling readiness, when a Job is blocked due to gates, we should report the reason as scheduling gated. We should also pay attention to whether the gated status of each Pod is correctly recorded (Modification is not necessary here as this is likely already handled by K8S). 