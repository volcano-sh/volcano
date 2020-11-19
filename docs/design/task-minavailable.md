# Support MinAvailable For Task Level

@[shinytang6](https://github.com/shinytang6); Nov 19th, 2020

## Motivation
As [issue 988](https://github.com/volcano-sh/volcano/issues/988) mentioned, Volcano should support set minAvailable in task level.

## Design

In order to implement this feature, I think it can be decomposed into the following two steps:

#### 1. Optimize job spec definition
 
The first step I think is also an existing problem in the current system, so we can optimize it first. Currently we use `int` in related spec definition of vcjob, like `MinAvailable`, `MaxRetry`. The problem with this is that we cannot distinguish between user behavior(set to 0) and the default int value of 0.
At the same time, due to this limitation, the current system does not inject default values ​​for these values ​​in admission webhook. Just take `MinAvailable` as an example, by default if the user does not specify it, we should set it to sum(task replicas), but currently we can’t do it.

All we need to do is change these `int` types to pointers, then we can set default values ​​for them and move forward to implement the second step of this feature.

The first step will **cause some compatibility issues**, users can submit yaml normally, but if users quote the definition of Job in their code, they need to modify the type of the corresponding fields.
**So users may need to update their code before upgrading to the latest version of Volcano.**

#### 2. Support minAvailable for task

This step needs to rely on the first step to proceed. Before talking about the specific implementation, l will first describe the purpose of `MinAvailable` field:
Currently `MinAvailable` field only supported at job level.
1. `MinAvailable` decides whether a vcjob can be scheduled during gang schedule. If sumof(valid tasks) >= job.minavailable, we take this job as valid(can be scheduled).
2. `MinAvailable` decides the final status of the vcjob. E.g. If the number of successful tasks in a finished job >= job.minavailable, we set the status of this job as `Completed`, or we set it as `Failed`.

So if we want to support minAvailable for task level, I think the changes involved in this feature are as follows:
1. We need to verify/set the default value in the webhook.
2. Since the scheduler does not aware jobs, it schedules based on `PodGroup`, we need to add a field to the `PodGroup` to describe the minMember corresponding to different tasks.
p.s: There is already a `minMember` field in `PodGroup`, which is equal to `MinAvailable` in corresponding job.
3. Modify the behavior of the current gang schedule. If the `minAvailable` field of the task is set, all tasks under the current job must meet the conditions of (valid pod of the task) >= job.task.minAvailable, then we can take the job as valid.
4. Modify the judgment of job status. Still take judging whether the job is completed as an example, only if the (successful pod of every task) >= job.task.minAvailable, then we can take the status of the job as `Completed`. This change may need another field in `JobStatus` to record the status of the pod corresponding to the task, currently `JobStatus` only records the number of pods in different states which we cannot distinguish which task belongs to. 

## Compatibility

As l described above, the first step will cause some compatibility issues, but I think the changes make senses.
If the user does not set `MinAvailable` in task level, it should be consistent with the original behavior.