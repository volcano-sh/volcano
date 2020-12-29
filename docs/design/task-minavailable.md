# Support MinAvailable For Task Level

@[shinytang6](https://github.com/shinytang6); Nov 19th, 2020

## Motivation
As [issue 988](https://github.com/volcano-sh/volcano/issues/988) mentioned, Volcano should support set minAvailable in task level.

## Design

#### Support minAvailable for task

Before talking about the specific implementation, l will first describe the purpose of `MinAvailable` field:
Currently `MinAvailable` field only supported at job level.
1. `MinAvailable` decides whether a vcjob can be scheduled during gang scheduling. If sumof(valid tasks) >= job.minavailable, we take this job as valid(can be scheduled).
2. `MinAvailable` decides the final status of the vcjob. E.g. If the number of successful tasks in a finished job >= job.minavailable, we set the status of this job as `Completed`, or we set it as `Failed`.

So if we want to support minAvailable for task level, I think the changes involved in this feature are as follows:
1. We need to define `minAvailable` field at the task level & verify/set the default value in the webhook.

    The new field is as follows：
    ```
    // TaskSpec specifies the task specification of Job.
    type TaskSpec struct {
        ...
        
        // The minimal available pods to run for this Task
        // Defaults to the task replicas
        // +optional
        MinAvailable *int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"`
    }
    ```

2. Since the scheduler does not aware jobs, it schedules based on `PodGroup`, we need to add a field to the `PodGroup` to describe the minMember corresponding to different tasks.

   The new field is as follows：
   ```
   // PodGroupSpec represents the template of a pod group.
   type PodGroupSpec struct {
        ...
       
        // MinTaskMember defines the minimal number of pods to run each task in the pod group;
        // if there's not enough resources to start each task, the scheduler
        // will not start anyone.
        MinTaskMember map[string]int32
   }
   ```
3. Add a new judgment logic to the current gang scheduling. The current logic is if sumof(valid tasks) >= job.minavailable, we take this job as valid(can be scheduled). We need to add a logic before that. The logic is that if the `minAvailable` field of the task is set, the task under current job must meet the conditions of (valid pod of the task) >= job.task.minAvailable, then we can take the job as valid.
4. Modify the judgment of job status. The current logic is that if the sumof(successful pods) >= job.minAvailable, then we can take the status of the job as `Completed`. This change may need another field in `JobStatus` to record the status of the pods under each task, currently `JobStatus` only records the number of pods in different states which we cannot distinguish which task pod belongs to.

    The newly added fields are as follows：
    
    ```
    // TaskState contains details for the current state of the task.
    type TaskState struct {
        // The phase of Task.
        // +optional
        Phase map[v1.PodPhase]int32 `json:"phase,omitempty" protobuf:"bytes,11,opt,name=phase"`
    } 
   
    // JobStatus represents the current status of a Job.
    type JobStatus struct {
        ...
        
        // The status of pods for each task
        // +optional
        TaskStatusCount map[string]TaskState `json:"taskStatusCount,omitempty" protobuf:"bytes,21,opt,name=taskStatusCount"`
    }
    ```