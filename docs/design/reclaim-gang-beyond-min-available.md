# Support reclaim/preempt gang job which allocated resources equal or lesser than MinAvailable

## Introduction
Gang is a default scheduling policy in AI industry, since most AI frameworks don't support fail over.
In such situations, overused queue/lower priority job should return resources
when other queue deserve more resources/higher priority job coming.
In current behavior, gang job will not return resources if it's allocated resources equal or less than MinAvailable.

## Motivation
As [issue 2321](https://github.com/volcano-sh/volcano/issues/2321) mentioned, Gang job reclaim/preempt behavior should support configure even it's allocated resources equal or less than MinAvailable.

## Design
Add a job level new boolean flag `volcano.sh/disable-gang-min-available-preempt-check` to support bypass MinAvailable check during gang job preempt check.
The default value is `false`

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  annotations:
    volcano.sh/disable-gang-min-available-preempt-check: true
```

