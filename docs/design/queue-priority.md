# Queue Priority

@[bufan](https://github.com/TaiPark); Jul 16th, 2024

## Motivation

The current queue prioritization in Volcano is based on the principle that a lower share value indicates a higher priority for resource allocation during scheduling.

However, based on user feedback and practical scenarios, there is a strong preference for a more straightforward approach to establishing queue priorities. Explicitly assigning priorities provides users with clear and immediate control over the order in which their queues are serviced.

While Volcano's current method of using queue shares effectively determines priority, there is a clear demand for a more intuitive and user-friendly approach to setting and managing queue order. Enabling users to directly assign and adjust queue priorities in the specifications would simplify queue management significantly.

## Implementation

### API Change

Add a `priority` attribute to the spec of `queues.scheduling.volcano.sh`. The priority attribute controls the order of queues in `capacity` and `proportion` plugins that implement QueueOrderFn.
```
spec:
    ...
    priority:
        type: number
    ...
```
The `priority` value should range from 0 to the maximum limit of int32.

### Queue Ordering

Assuming the overused queues do not enter the scheduling process, the `QueueOrderFn` in the capacity and proportion plugins will follow this sorting logic:
- Prioritize by priority: queues with higher priorities will be positioned at the front of the PriorityQueue and processed first.
- If the priority values are identical, the current method of comparing queue shares will be applied.

### Reclaim

The `reclaim` action is implemented for the reclaim logic between queues. Queue priority should be considered when reclaiming across queues.
- The queue reclaiming should be orderly processed from the highest priority queue to the lowest, as guaranteed by `QueueOrderFn`.
- For the victim PriorityQueue construction, considering two victims with different job id, the one with the lower queue priority should be selected as the victim.
- If a queue has reclaimed all lower-priority reclaimable resources and still does not meet its resource request, it will proceed to reclaim resources from higher-priority queues.