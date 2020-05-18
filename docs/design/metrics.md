## Scheduler Monitoring

## Introduction
Currently users can leverage controller logs and job events to monitor scheduler. While useful for debugging, none of this options is particularly practical for monitoring kube-batch behaviour over time. There's also requirement like to monitor kube-batch in one view to resolve critical performance issue in time [#427](https://github.com/kubernetes-sigs/kube-batch/issues/427).

This document describes metrics we want to add into kube-batch to better monitor performance.

## Metrics
In order to support metrics, kube-batch needs to expose a metrics endpoint which can provide golang process metrics like number of goroutines, gc duration, cpu and memory usage, etc as well as kube-batch custom metrics related to time taken by plugins or actions.

All the metrics are prefixed with `kube_batch_`.

### kube-batch execution
This metrics track execution of plugins and actions of kube-batch loop.

| Metric name | Metric type | Labels | Description |
| ----------- | ----------- | ------ | ----------- |
| e2e_scheduling_latency | histogram |  | E2e scheduling latency in seconds |
| plugin_latency | histogram | `plugin`=&lt;plugin_name&gt; | Schedule latency for plugin |
| action_latency | histogram | `action`=&lt;action_name&gt; | Schedule latency for action |
| task_latency | histogram | `job`=&lt;job_id&gt; `task`=&lt;task_id&gt; | Schedule latency for each task |


### kube-batch operations
This metrics describe internal state of kube-batch.

| Metric name | Metric type | Labels | Description |
| ----------- | ----------- | ------ | ----------- |
| pod_schedule_errors | Counter |  | The number of kube-batch failed due to an error |
| pod_schedule_successes | Counter | | The number of kube-batch success in scheduling a job |
| pod_preemption_victims | Counter | | Number of selected preemption victims |
| total_preemption_attempts | Counter |  | Total preemption attempts in the cluster till now |
| unschedule_task_count | Counter | `job`=&lt;job_id&gt; | The number of tasks failed to schedule |
| unschedule_job_counts | Counter | | The number of job failed to schedule in each iteration |
| job_retry_counts | Counter | `job`=&lt;job_id&gt; | The number of retry times of one job |


### kube-batch Liveness
Healthcheck last time of kube-batch activity and timeout
