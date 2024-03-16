# Priority Plugin

## Introduction

When users apply jobs to volcano, they may need to support different priority to different kind of jobs. For example, important serving jobs can have a higher priority when allocating resources, or preempting other jobs in same queue. Even in the same job, the important roles may have a higher priority than others roles, so that it can finished running without interruption.

## Solution

1. `priority` plugin return 5 callback functions: `taskOrderFn`, `jobOrderFn`, `preemptableFn`, `reclaimableFn`, and `jobStarvingFn`:

   1. `taskOrderFn` adjusts the order of task in a job. The higher priority a task has, the higher scored of this task in `taskOrderFn` of `priority` plugin, so that task would have larger probability to be front in priority queue.

   2. `JobOrderFn` adjusts the order of this job. The higher priority (set in podgroup spec) a job has, the higher scored of this job in `JobOrderFn` of `priority` plugin, so that job would have larger probability to be front int priority queue.

   3. `preemptableFn` compares the priority of preemptor and victims. If preemptor task and victim task belong to different job, then compares their job's priority and job with higher priority can preempt job with lower priority. Otherwise, compares preemptor task's priority and victim task's priority. Task with higher priority can preempt task with lower priority in same job.

   4. `reclaimableFn` compares the priority of preemptor job and victim job. Job with higher priority can reclaim job with lower priority.

**Note**: `reclaimableFn` usually is used in `reclaim` action to reclaim a queue's deserved resource when cluster has not enough resource to allocate new coming tasks in this queue. So it should be carefully used in `priority` plugin, in case that a queue's resource owned by high priority jobs can not be released.

## Feature Interaction

At first, `reclaimableFn` is not supported in priority plugin. Now it is added as [issue2262](https://github.com/volcano-sh/volcano/issues/2262) required.  `enableReclaimable` is disabled by default for compatibility. Please be carefully to set `enableReclaimable` to `true` in `priority` plugin, because it may cause a queue's overused resource can not be reclaimed if the overused resource is own by jobs with high priority.

```yaml
    actions: "enqueue, allocate, preempt, reclaim"
    tiers:
    - plugins:
      - name: priority
        enableReclaimable: false
      - name: gang
        enablePreemptable: false
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
```
