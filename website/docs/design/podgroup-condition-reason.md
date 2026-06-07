# podgroup conditions

## Backgrounds

Currently, there are only two podgroup condition type: `Unschedulable` and `Scheduled`. If job is not enqueued, it has not been scheduled. And the event reason is `Unschedulable` and podgroup condition reason is `NotEnoughResources`. These reasons is not coresponding with the real reason `job is not enqueued`

## Motivation

In order to classify the uninqueueable reason from other unscheduleable reasons

## Design

1. add `Uninqueueable` reason for podgroup events

```go
 // PodGroupUnInqueueable is Uninqueueable event type
 PodGroupUnInqueueable PodGroupConditionType = "Uninqueueable"
```

2. add `NotInqueueable` reason for podgroup conditions reason

```go
 // NotInqueueableReason if probed if job is rejected to enqueue
 NotInqueueableReason string = "NotInqueueable"
```
