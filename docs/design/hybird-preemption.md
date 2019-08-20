# Hybird preemption

[@william-wang](http://github.com/william-wang); Aug 20, 2019

## Table of Contents

   * [Dynamic Plugins Configuration](#dynamic-plugins-configuration)
      * [Table of Contents](#table-of-contents)
      * [Motivation](#motivation)
      * [Function Detail](#function-detail)
     

## Motivation

Currently when Gang and Drf plugins are both configured and enable the preempt actions. The Drf and Gang preemption can not play together
well. The following issues have been found.
- When job’s `min.available` is configured to 1. This Job will be preempted endlessly.
- When the `min.available` of multiple jobs are configured to 1. These jobs will preempt each other endless.
- When most of tasks in job are completed, the jobs meet both preemptor and preemptee condition, so different jobs
  will preempt each other endlessly.

The PR404's target is to enhance the preemption to make make Gang and Drf work together more reasonable and resolve above problems.

## Function Detail

The enhancement complys with the tier design that if it fits plugins in high priority tier, the action will not go through the plugins
in lower priority tiers And in each tier, it's considered passed when all the plugins are fitted.

The basic preemption behavior is that the pending jobs preempt running jobs with Gang preemption. Once the preemptor jobs turn into 
running, the Drf preemption will be used to balance resource among running jobs according to the Drf share.

The detail is as below:

When the job of preemptor task is not ready, it will only preempt running tasks from other jobs which occupy more resource than it’s min.Avaiable firstly
and turns into running. Then the Drf preemption will be involved to preempt the resource among running jobs.
And the Drf will not preempt `min.available` part resource of job forever.


In `Preemptable(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {..}` function, The plugin function `pf` is redesigned.

>candidates, unpreemptableList := pf(preemptor, preemptees)
>
>candidates: 
```
Tasks that can be preemptable. 
If the preemptor job is not ready, it will get the candidates. 
If the preemptor job is running, It will not find candidates. It need Drf plugin to preempt resource for this running job.
```
>unpreemptableList:
```
Tasks that should not be preemptable. 
Gang plugin has the `min.Available` info. So it knows those tasks that should not be preemptable. The framework will filter
out these tasks and pass the left tasks to Drf preemption function. The targe is to avoid Drf preempt `min.available` part resource.

```

```
For example
1. candidates: [task1, task2], unpreemptableList:[]

Gang plugin will make decision and complete preemption on task1 and task2 alone.

2. Candidates:[], unpreemptableList[task1]

Gang plugin will not make decision, the Drf will complete preemption alone. The preemption will ignore the task1.
```

## Use Case
The cluster has 4 CPUs totally, each task in job-01 and job-02 requests 1 CPU.
- The Gang and Drf work together to complete preemption

    | Actions        | Job Name  | Task Num  | Min.Available | Task status |  
    | -------------- | --------- | --------- | --------------| ----------- |
    | Submit 1st Job | job-01    | 4         | 2             | 4 Running   |
    | Submit 2st Job | job-02    | 4         | 1             | 4 Pending   |
    | Result         | job-01    | 4         | 2             | 2 Running   |
    |                | job-02    | 4         | 1             | 2 Running   |
    
    The job-02 preempts one task from job-01 by Gang preemption. And preempts the second task from job-01 by Drf preemption. 
    
- The Drf preemption does not preempt `min.available` part resource of job

    | Actions        | Job Name  | Task Num  | Min.Available | Task status |  
    | -------------- | --------- | --------- | --------------| ----------- |
    | Submit 1st Job | job-01    | 4         | 3             | 4 Running   |
    | Submit 2st Job | job-02    | 4         | 1             | 4 Pending   |
    | Result         | job-01    | 4         | 3             | 3 Running   |
    |                | job-02    | 4         | 1             | 1 Running   |
    
    The job-02 preempts one task from job-01 by Gang preemption. And can not continue to preempt more tasks because all 
    remaining 3 tasks in job-01 are added into the unpreemptableList. These tasks will be ignored by framework before pass to 
    Drf preemption.

- If high tier plugin(Gang) can make decision, it will complete preemption alone.

    | Actions        | Job Name  | Task Num  | Min.Available | Task status |  
    | -------------- | --------- | --------- | --------------| ----------- |
    | Submit 1st Job | job-01    | 4         | 2             | 4 Running   |
    | Submit 2st Job | job-02    | 4         | 2             | 4 Pending   |
    | Result         | job-01    | 4         | 2             | 2 Running   |
    |                | job-02    | 4         | 2             | 2 Running   |
    
   The job-02 preempts two tasks from job-01 by Gang preemption.
  
 - When most tasks of job are completed, the jobs does not preempt each other endless any more. 
   The Drf will balance resource among jobs. 
   
    | Actions        | Job Name  | Task Num  | Min.Available | Task status                       |  
    | -------------- | --------- | --------- | --------------| ----------------------------------|
    | Submit 1st Job | job-01    | 10        | 3             | 4 Running  2 completed 4 Pending  |
    | Submit 2st Job | job-02    | 10        | 2             | 10 Pending 2 completed 4 Pending  |
    | Result         | job-01    | 10        | 3             | 2 Running  2 completed 6 Pending  |
    |                | job-02    | 10        | 2             | 2 Pending  8 pending              |
    
    The job-02 preempts two tasks from job-01 by Gang preemption and turns into running. And then Drf balance 
    resource between jobs according to the share.

