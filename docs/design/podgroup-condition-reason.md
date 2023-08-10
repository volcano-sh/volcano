# podgroup conditions

## Backgrounds

Currently, there are only two podgroup condition type: `Unschedulable` and `Scheduled`. If job is not enqueued, it has not been scheduled. And the event reason is `Unschedulable` and podgroup condition reason is `NotEnoughResources`. These reasons is not corresponding with the real reason `job is not enqueued`

## Motivation

In order to classify the unenqueueable reason from other unscheduleable reasons

## Design

1. add `Unenqueueable` reason for podgroup events

```go
 // PodGroupUnenqueueable is Unenqueueable event type
 PodGroupUnenqueueable PodGroupConditionType = "Unenqueueable"
```

2. add `Unenqueueable` reason for podgroup conditions reason

```go
 // UnEnqueueableReason is probed if job is rejected to enqueue
 UnEnqueueableReason string = "Unenqueueable"
```
