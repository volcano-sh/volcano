# Volcano Resource Reservation For Queue(v2)

@[Thor-wl](https://github.com/Thor-wl); Feb 28th, 2021

Inspired by design optimization of `target job resource reservation`, our goal is to ensure the SLA of jobs in target
queues. So just allocate resource for jobs by order SLA.

## Design
Queues with annotation `maxWaitingTime` indicate that this queue should ensure SLA. Jobs in the queue will be scheduled
first if its SLA is approach the requirement.

## Implementation
### API
```
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: q1
  annotations:
    # maxWaitingTime indicates the maximal pending time for jobs in the queue. 
    # If not scheduled after waiting for maxWaitTime, jobs will be scheduled first.
    maxWaitingTime: 30m
spec:
  reclaimable: true
  weight: 1
status:
  state: Open
```
### Detail
* Jobs in queue with annotation `maxWaitingTime` will be attached annotation `maxWaitingTime`.
* Jobs with annotation `maxWaitingTime` will be scheduled ahead of others.
* Reuse action `sla` to decide the scheduling order of jobs according the remaining waiting time.
  `remainingWaitingTime = maxWaitingTime - hasAlreadyWaitingTime`.
* Remaining waiting time will be recalculated again in every session.

