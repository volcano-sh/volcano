# Volcano Resource Reservation For Big Jobs

@[Thor-wl](https://github.com/Thor-wl); Aug 19th, 2020

## Motivation
As [issue 13](https://github.com/volcano-sh/volcano/issues/13) / [issue 748](https://github.com/volcano-sh/volcano/issues/748) 
/ [issue 947](https://github.com/volcano-sh/volcano/issues/947) mentioned, current scheduler strategy may result in big 
jobs starvation. Consider two classical scenes:
* Suppose there is insufficient resource in cluster and both Job A and Job B are to be scheduled. Job A and Job B are in 
equal priority while Job A request more resources. Under current schedule strategy, there is high probability that Job B
can be scheduled first while Job A will be pending for a long time. If more jobs requesting less resource comes later, 
Job A will get a smaller chance to be scheduled.
* Suppose cluster resource is insufficient, Job A has higher priority and requests more resource while Job B has lower
priority but request less resource. As current schedule strategy works, volcano will schedule Job B first. What's worst,
Job A will keep waiting until enough resources are released by some low priority jobs. 

## Consideration
### How to recognise Big Jobs?
There are two ways to pick out Big Jobs:
#### request resources
Set standard of request resources. Jobs requesting more resources than standard will be regarded as Big Jobs. It may be
a good way for specific scenarios such as machine learning training/big data/scientific computing, etc. However, users 
need to be very experienced with his/her job requirements.
#### waiting time
Consider waiting time as Big Job standard is another solution. Jobs who waiting for longer time are more likely to be
Big Jobs. Different from setting standard line, order jobs by waiting time is a good idea.
### How to reserve resources for Big Jobs?
Following are the factors taking into consideration for resources reservation.
#### resource amount
Absolutely, jobs requiring resources more than cluster total amount cannot be satisfied. When choose nodes who need to
reserve resources for Big Jobs, the total amount idle resources of the selected nodes should as closer as the requirement
because only in this way can we need the least amount resources for jobs to be finished. 
#### selected nodes lock
Nodes who are chosen to reserve resources should be locked. That means these nodes cannot accept any other jobs until Big
Jobs are scheduled to it.
#### selected nodes numbers
Another problem is how many nodes can be selected as Reservation Node. In essence, it's a problem to balance scheduling
performance and reservation requirement.
#### the biggest challenge: unpredictable completion time of running jobs in selected nodes
Uncertainty of completion time of running jobs in selected nodes makes it difficult to find the optimal solution for 
meeting the requirement of Big Jobs. Though idle resources in selected nodes satisfied Big Jobs most, there's no guarantee 
that the waiting time for extra resource taken in running jobs is the shortest. In some cases, it may be a suboptimal 
solution.
### How to balance priority and waiting time?
priority is more important than waiting time.
* No matter how many resources high-priority jobs requests and how much time they have already waited for, they should be
scheduled first.
* When jobs are at same priority but waiting time differs, job who waits for the longest time should be scheduled first.

## Design
### Big Job Recognition
As volcano is a general platform, we tend to support both user defined mode and automatic election mode to recognize Big
Jobs.
#### user defined mode
Users can set **request resource** or **waiting time** as standard. Jobs who request resources more than settings or 
wait longer than standard line will be treated as potential Big Job. Volcano will choose the Big Job who has the highest 
priority and above the standard line most as the Big Job.
#### automatic election mode
If not config standard line, volcano will order jobs to be scheduled in session by priority and waiting time. The job 
with the highest priority and waiting for the longest time will be selected as the Big Job.                                                                                
volcano scheduler will check if there is a Big Job selected in each session. If no Big Job selected, volcano will select
a Big Job according to the strategy above.
### Reservation Node Selected
As job consists of some tasks and each task corresponds to a pod, scheduler will select a series of nodes who can satisfy
these pods. These nodes will be locked and no pod can be scheduled to them until the Big Job is scheduled. In order to 
reduce the influence of schedule performance, scheduler will try to select nodes as less as possible. 
As to the reservation strategy, there are two scenes:
* If field estimated_running_duration is not set for each task, namely we cannot evaluate the finish time of the running
pods in all nodes, volcano scheduler will order the nodes by idle resources. Then select some nodes at first until the 
total resource amount of these nodes is not less than requirement. 
* If field estimated_running_duration is set for each task, calculate the finish time of each job. Then calculate the 
time for each node that the last job running in it finishes. Order all nodes by the finish time and select some nodes at
first whose total amount of resource is not less than requirement. It should to be noted that if users config true to 
allow the Big Job preempt resource of running jobs whose running duration is out of date in Reservation Nodes. It is fit
for users who are very experienced with his/her tasks.
 
## Note
* The max resources amount of Reserved Node must be no less than Big Job's requirement.
* Implement this feature as a plugin.