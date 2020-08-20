# Volcano Resource Reservation For Big Job

@[Thor-wl](https://github.com/Thor-wl); Aug 19th, 2020

## Motivation
As [issue 13](https://github.com/volcano-sh/volcano/issues/13) / [issue 748](https://github.com/volcano-sh/volcano/issues/748) 
/ [issue 947](https://github.com/volcano-sh/volcano/issues/947) mentioned, current scheduler strategy may result in big 
job starvation. Consider two classical scenes:
* Suppose there is insufficient resource in cluster and both Job A and Job B are to be scheduled. Job A and Job B are in 
equal priority while Job A request more resources. Under current schedule strategy, there is high probability 
that Job B can be allocated resource first and turns to running state while Job A will be pending for a long time. If
more jobs requesting less resource comes later, Job A will get a smaller chance to be scheduled.
* Suppose cluster resource is insufficient, Job A has higher priority and requests more resource while Job B has lower
priority but request less resource. As current schedule strategy works, volcano will schedule Job B first. What's worst,
Job A will keep waiting until enough resources are released by some low priority job. 
## Consideration
### How to recognise Big Job?
There are two ways to pick out Big Jobs:
#### request resources
Setting standard of request resources. Jobs requesting more resources than standard will be regarded as Big Jobs. This 
way may not be so reasonable because the standard line is different in different scenes. On the other hand, This standard 
is set artificially which may be very imprecise.
#### waiting time
Considering waiting time as Big Job standard is another solution. Jobs who waiting for longer time are more likely to be
Big Jobs. Different from setting standard line, order jobs by waiting time is a good idea.
### How to balance priority and waiting time?
priority is more important than waiting time.
* No matter how many resources high-priority jobs requests and how much time they have already waited for, they should be
scheduled first.
* When jobs are at same priority but waiting time differs, job who waits for the longest time should be scheduled first.
## Design
* Add an attribute to every job to record the timestamp of reaching scheduler.
* In each session, if there has already a job who has been selected as the Big Job, continue to reserve resource in the 
Reserved Node until reserved resource satisfies its requirement. During the reserving process, do not bind any other jobs 
to the node.   
* In each session, if no Big Job was selected, select the job who has the highest priority and waits for the longest time
as Big Job. Choose a node whose idle resource is closest to Big Job's requirement as Reserved Node.
## Detail
* The max resources amount of Reserved Node must be no less than Big Job's requirement.
* Implement this feature as a plugin.