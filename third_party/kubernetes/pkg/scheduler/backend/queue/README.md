# Scheduling Queue for Volcano Agent Scheduler

This directory contains the scheduling queue implementation copied from Kubernetes kube-scheduler.

## Purpose

Volcano agent scheduler requires a fast and reliable pod queue mechanism. We deeply respect and appreciate the work of Kubernetes kube-scheduler and all contributors who have built this sophisticated scheduling queue over the years. Their proven, battle-tested implementation serves as an excellent foundation.

Rather than reinventing the wheel, Volcano adopts this mature queue logic and continues to iterate upon it with Volcano-specific enhancements. This approach honors the upstream work while enabling us to build features tailored to volcano agent scheduler.

## Source

- **Repository**: `https://github.com/kubernetes/kubernetes`
- **Version**: `v1.34`
- **Source Package**: `pkg/scheduler/backend/queue`
- **Copied Files**:
    - `scheduling_queue.go` - Main scheduling queue implementation
    - `active_queue.go` - Active queue for ready-to-schedule pods
    - `backoff_queue.go` - Backoff queue for retry with exponential backoff
    - `unschedulable_pods.go` - Unschedulable pods management
    - `*_test.go` - Corresponding unit tests

## Modifications

- Removed all `NominatedNode` related code and pod nomination logic.

## Upgrade

Since this is a manual copy, upgrading requires manual intervention.

### Option 1: Apply Diff

For small version bumps:

```bash
# In kubernetes repo
git diff v1.34.0..v1.35.0 -- pkg/scheduler/backend/queue
```

Then manually apply changes.

### Option 2: Replace Files

For major version changes:

1. Delete all files in this directory
2. Copy new files from upstream `pkg/scheduler/backend/queue`
3. Re-apply modifications (remove `NominatedNode` code)

Remember to update the version number in this README.

## License

Apache License 2.0 (same as Kubernetes project).
