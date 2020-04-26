# Volcano Job scale up and down

@hzxuzhonghu; April 24, 2020

## Motivation

Currently, Volcano does not support Job update. It is not allowed to update the `Job.Spec` on the fly.
However, many users show appeal to run ML training jobs in a elastic manner. For example ModelArts want to dynamically adjust Job's replicas according to the cluster idle capacity 
in order to achieve most high efficiency on GPU card.

I propose to support volcano job dynamical scale up/down before more intelligent elasticity in the first step.

## Design

Before this design, let's recall the current Job's initialization

### Job Initialization

When a Volcano job is created, the job controller does the following to run/manage all of its tasks.

1. all the plugins execute OnJobAdd callbacks

2. create pvc for the job

3. create PodGroup for the job

4. create pods equals the replicas of the job

All above steps are run in `syncJob`, which is called when external events happen, for this it happens when Job is newly created.

### Volcano Job Scale Up/Down

The Job's scale up and down correlates to reconciling of the resources the job owns, like PVC/PodGroup/Service/HostFile ConfigMap
so the procedure is kind of similar to the [Job Initialization](#Job Initialization).

The differences are:

1. job plugins' callbacks：only the `svc` plugin should update the configmap including the job tasks

2. create pods when scale up

3. delete pods when scale down

However, only when the job is not started, the initialization is run. 
So we need a way to know whether it is a scale up/down event that triggered this round of sync.

The way I propose is to add a new event `JobUpdatedEvent` to indicate that the job is updated(here only cares about the scale up/down).
And accordingly add a new action `UpdateJobAction` to run `UpdateJob` function. And the overall workflow is:
![workflow](images/Job-scale-up-down.PNG)

To scale up/down on the fly, Volcano should be responsible to notify the original pods the current status, including the hosts of all the pods.
This is done by plugins, so to distinguish from the initialization phase, a new `OnJobUpdate` is introduced. 
It is to reconcile all the associated configs of the job. Currently, the `svc` plugin should update the configmap of all the hosts.

**NOTE**: Users should watch the `/etc/volcano` to get the up-to-date hosts files if they want to be aware of the training workers.

```
type PluginInterface interface {
	// The unique name of Plugin.
	Name() string

	// for all pod when createJobPod
	OnPodCreate(pod *v1.Pod, job *vcbatch.Job) error

	// do once when syncJob
	OnJobAdd(job *vcbatch.Job) error

	// do once when killJob
	OnJobDelete(job *vcbatch.Job) error

	OnJobUpdate(job *vcbatch.Job) error
}
```


### Admission webhook

Should prevent invalid mutating Job Spec on the fly. In this proposal, we only allow replicas update. Any other spec changes will be prohibited.
It is also not allowed if the number of total replicas is less than the `minAvailable`.




