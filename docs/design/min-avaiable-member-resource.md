# min available members and min resource

- [min available members and min resource](#min-available-members-and-min-resource)
  - [1. job's MinAvailable is zero](#1-jobs-minavailable-is-zero)
    - [1.1 task's MinAvailable is null](#11-tasks-minavailable-is-null)
    - [1.2. task's MinAvailable is not null](#12-tasks-minavailable-is-not-null)
  - [2. job's MinAvailable is not zero](#2-jobs-minavailable-is-not-zero)
    - [2.1 task's MinAvailable is null](#21-tasks-minavailable-is-null)
    - [2.2 task's MinAvailable is not null](#22-tasks-minavailable-is-not-null)

Nowadays there are min-members of both job and task.

If task's min-available is null, webhook will patch it as task's replicas;

And then, if job's min-available is null, webhook will patch job's min-available as sum of tasks' min-available

Relative issue is at [PG minresource has some problems after PR#1459 #2921](https://github.com/volcano-sh/volcano/issues/2921), and PR is at [fix calculations of podgroup min resource #3057](https://github.com/volcano-sh/volcano/pull/3057)

With this example yaml, master will have higher priority when sort them.

```yaml
spec:
  minAvailable: 2
  queue: default
  schedulerName: volcano
  tasks:
    - replicas: 2
      name: "master"
      template:
        spec:
          containers:
            - image: nginx
              name: nginx
              resources:
                requests:
                  cpu: "50m"
    - replicas: 2
      minAvailable: 1
      name: "work"
      template:
        spec:
          containers:
            - image: nginx
              name: nginx
              resources:
                requests:
                  cpu: "100m"
```

|job.minAvailable|master.minAvailable|work.minAvailable|current(minMember/CPU)|expect|explain|express|
|---|---|---|--------|------|----------------|-----|
| 0 | - | 1 | 3/200m |3/200m|2*master+worker| jobMinAvailable == sum(taskMinAvailable) |
| 0 | 1 | 1 | 2/100m |2/150m|master+worker | jobMinAvailable == sum(taskMinAvailable)|
| 3 | - | 1 | 3/200m |3/200m|2*master+worker| jobMinAvailable == sum(taskMinAvailable)|
| 3 | 1 | - | 3/200m |3/250m|master+2*worker| jobMinAvailable == sum(taskMinAvailable)|
| 3 | 1 | 1 | 3/200m |3/200m|master+worker+master | jobMinAvailable > sum(taskMinAvailable)|
| 2 | 1 | 1 | 2/100m |2/150m|master+worker |jobMinAvailable = sum(taskMinAvailable)|
| 2 | - | 1 | 2/100m |2/100m|2*master |jobMinAvailable < sum(taskMinAvailable)|
| 2 | 1 | - | 2/100m |2/100m|2*master |jobMinAvailable < sum(taskMinAvailable)|

## 1. job's MinAvailable is zero

### 1.1 task's MinAvailable is null

webhook will patch task's MinAvailable to Replicas via mutateSpec

- [x] jobMinAvailable equals sum of task's Replicas (done in patchDefaultMinAvailable)
- [x] minResource equals sum of resource among first jobMinAvailable

now, job.MinAvailable == sum(task.Replicas)

### 1.2. task's MinAvailable is not null

job.MinAvailable == sum(task.MinAvailable) <= sum(task.Replicas)

- [x] jobMinAvailable equals sum of task's MinAvailable (done in patchDefaultMinAvailable)
- [ ] minResource equals sum of resource among first jobMinAvailable by each task's MinAvailable(not task's Replicas)

## 2. job's MinAvailable is not zero

### 2.1 task's MinAvailable is null

now, job.MinAvailable <= sum(task.Replicas)

- [x] minResource equals sum of resource among first jobMinAvailable, sorted by priority

### 2.2 task's MinAvailable is not null

job.MinAvailable >= sum(task.MinAvailable)

- [ ] minResource equals sum of task.MinAvailable, then sum up resource of leftNumber(equals to job.MinAvailable - total(task.MinAvailable)) sorted by priority

job.MinAvailable < sum(task.MinAvailable)

- [ ] minResource equals total resource of first job.MinAvailable tasks (same as the current logical of CheckTaskReady/CheckTaskValid/CheckTaskPipelined, which treats job ready/valid/pipelined when job.MinAvailable < taskMinAvailableTotal)
