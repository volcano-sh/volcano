## Scheduler Monitoring

## Introduction
Currently users can leverage controller logs and job events to monitor scheduler. While useful for debugging, none of these options is particularly practical for monitoring volcano behaviour over time. There's also requirement like to monitor volcano in one view to resolve critical performance issue in time [#427](https://github.com/kubernetes-sigs/kube-batch/issues/427).

This document describes metrics we want to add into volcano to better monitor performance.

## Metrics
In order to support metrics, volcano needs to expose a metrics endpoint which can provide golang process metrics like number of goroutines, gc duration, cpu and memory usage, etc as well as volcano custom metrics related to time taken by plugins or actions.

All the metrics are prefixed with `volcano_`.

### volcano execution
This metrics track execution of plugins and actions of volcano loop.

| **Metric Name**                           | **Metric Type** | **Labels**                                                                                | **Description**                                                                |
|-------------------------------------------|-----------------|-------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------|
| `e2e_scheduling_latency_milliseconds`     | Histogram       | None                                                                                      | End-to-end scheduling latency in milliseconds (scheduling algorithm + binding) |
| `e2e_job_scheduling_latency_milliseconds` | Histogram       | None                                                                                      | End-to-end job scheduling latency in milliseconds                              |
| `e2e_job_scheduling_duration`             | Gauge           | `job_name`=&lt;job_name&gt;, `queue`=&lt;queue&gt;, `job_namespace`=&lt;job_namespace&gt; | End-to-end job scheduling duration                                             |
| `e2e_job_scheduling_start_time`           | Gauge           | `job_name`=&lt;job_name&gt;, `queue`=&lt;queue&gt;, `job_namespace`=&lt;job_namespace&gt; | End-to-end job scheduling start time                                           |
| `plugin_scheduling_latency_milliseconds`  | Histogram       | `plugin`=&lt;plugin_name&gt;, `OnSession`=&lt;OnSession&gt;                               | Plugin scheduling latency in milliseconds                                      |
| `action_scheduling_latency_milliseconds`  | Histogram       | `action`=&lt;action_name&gt;                                                              | Action scheduling latency in milliseconds                                      |
| `task_scheduling_latency_milliseconds`    | Histogram       | None                                                                                      | Task scheduling latency in milliseconds (task create to task bound to a node)  |


### volcano operations
This metrics describe internal state of volcano.

| **Metric Name**                        | **Metric Type** | **Labels**                                                        | **Description**                               |
|----------------------------------------|-----------------|-------------------------------------------------------------------|-----------------------------------------------|
| `pod_preemption_victims`               | Gauge           | None                                                              | The number of selected preemption victims     |
| `total_preemption_attempts`            | Counter         | None                                                              | Total preemption attempts in the cluster      |
| `unschedule_task_count`                | Gauge           | `job_id`=&lt;job_id&gt;                                           | The number of tasks failed to schedule        |
| `unschedule_job_counts`                | Gauge           | None                                                              | The number of jobs could not be scheduled     |
| `queue_allocated_milli_cpu`            | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Allocated CPU count for one queue             |
| `queue_allocated_memory_bytes`         | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Allocated memory for one queue                |
| `queue_allocated_scalar_resources`     | Gauge           | `queue_name`=&lt;queue_name&gt;, `resource`=&lt;resource_name&gt; | Allocated scalar resource for one queue       |
| `queue_request_milli_cpu`              | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Requested CPU count for one queue             |
| `queue_request_memory_bytes`           | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Requested memory for one queue                |
| `queue_request_scalar_resources`       | Gauge           | `queue_name`=&lt;queue_name&gt;, `resource`=&lt;resource_name&gt; | Requested scalar resource for one queue       |
| `queue_deserved_milli_cpu`             | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Deserved CPU count for one queue              |
| `queue_deserved_memory_bytes`          | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Deserved memory for one queue                 |
| `queue_deserved_scalar_resources`      | Gauge           | `queue_name`=&lt;queue_name&gt;,                                  | Deserved scalar resource for one queue        |
| `queue_capacity_mill_cpu`              | Gauge           | `queue_name`=&lt;queue_name&gt;,                                  | CPU count capacity for one queue              |
| `queue_capacity_memory_bytes`          | Gauge           | `queue_name`=&lt;queue_name&gt;,                                  | memory capacity for one queue                 |
| `queue_capacity_scalar_resources`      | Gauge           | `queue_name`=&lt;queue_name&gt;, `resource`=&lt;resource_name&gt; | Scalar resource capacity for one queue        |
| `queue_real_capacity_mill_cpu`         | Gauge           | `queue_name`=&lt;queue_name&gt;,                                  | CPU count real capacity for one queue         |
| `queue_real_capacity_memory_bytes`     | Gauge           | `queue_name`=&lt;queue_name&gt;,                                  | Memory real capacity for one queue            |
| `queue_real_capacity_scalar_resources` | Gauge           | `queue_name`=&lt;queue_name&gt;, `resource`=&lt;resource_name&gt; | Scalar resource real capacity for one queue   |
| `queue_share`                          | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Share for one queue                           |
| `queue_weight`                         | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Weight for one queue                          |
| `queue_overused`                       | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | Whether one queue is overused                 |
| `queue_pod_group_inqueue_count`        | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | The number of Inqueue PodGroups in this queue |
| `queue_pod_group_pending_count`        | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | The number of Pending PodGroups in this queue |
| `queue_pod_group_running_count`        | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | The number of Running PodGroups in this queue |
| `queue_pod_group_unknown_count`        | Gauge           | `queue_name`=&lt;queue_name&gt;                                   | The number of Unknown PodGroups in this queue |
| `namespace_share`                      | Gauge           | `namespace_name`=&lt;namespace_name&gt;                           | Deserved CPU count for one namespace          |
| `namespace_weight`                     | Gauge           | `namespace_name`=&lt;namespace_name&gt;                           | Weight for one namespace                      |
| `job_share`                            | Gauge           | `job_id`=&lt;job_id&gt;, `job_ns`=&lt;job_ns&gt;                  | Share for one job                             |
| `job_retry_counts`                     | Counter         | `job_id`=&lt;job_id&gt;                                           | The number of retry counts for one job        |
| `job_completed_phase_count`            | Counter         | `job_name`=&lt;job_name&gt; `queue_name`=&lt;queue_name&gt;       | The number of job completed phase             |
| `job_failed_phase_count`               | Counter         | `job_name`=&lt;job_name&gt; `queue_name`=&lt;queue_name&gt;       | The number of job failed phase                |
| `task_pending_count`                   | Gauge           | None                                                              | The number of pending task in scheduler       |
| `task_allocated_count`                 | Gauge           | None                                                              | The number of allocated task in scheduler     |
| `task_pipelined_count`                 | Gauge           | None                                                              | The number of pipelined task in scheduler     |
| `task_binding_count`                   | Gauge           | None                                                              | The number of binding task in scheduler       |
| `task_bound_count`                     | Gauge           | None                                                              | The number of bound task in scheduler         |
| `task_running_count`                   | Gauge           | None                                                              | The number of running task in scheduler       |
| `task_releasing_count`                 | Gauge           | None                                                              | The number of releasing task in scheduler     |
| `task_succeeded_count`                 | Gauge           | None                                                              | The number of succeeded task in scheduler     |
| `task_failed_count`                    | Gauge           | None                                                              | The number of failed task in scheduler        |
| `task_unknown_count`                   | Gauge           | None                                                              | The number of unknown task in scheduler       |
| `task_operation_latency_milliseconds`  | Histogram       | `operation`=&lt;operation_name&gt; `result`=&lt;result&gt;        | Task task operation latency in milliseconds   |

### volcano Liveness
Healthcheck last time of volcano activity and timeout
