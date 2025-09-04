# Dequeue Strategies Example

This example demonstrates how to use different dequeue strategies in Volcano scheduler.

## Setup

1. Create queues with different dequeue strategies
2. Submit jobs with different resource requirements
3. Observe the scheduling behavior

## Files

- `queues.yaml`: Queue definitions with different dequeue strategies
- `jobs.yaml`: Sample jobs with varying resource requirements

## Running the Example

1. Apply the queue configurations:
   ```bash
   kubectl apply -f queues.yaml
   ```

2. Submit the sample jobs:
   ```bash
   kubectl apply -f jobs.yaml
   ```

3. Monitor the scheduling behavior:
   ```bash
   kubectl get pods -w
   kubectl get jobs
   ```

4. Check scheduler logs:
   ```bash
   kubectl logs -n volcano-system -l app=volcano-scheduler
   ```

## Expected Behavior

- **FIFO Queue**: Jobs will be scheduled in strict order, potentially blocking smaller jobs
- **Traverse Queue**: Smaller jobs may be scheduled before larger ones if resources are limited

## Cleanup

```bash
kubectl delete -f jobs.yaml
kubectl delete -f queues.yaml
```
