# Usage based scheduling
@william-wang Feb 16 2022

## Motivation
Currently the pod is scheduled based on the resource request and node allocatable resource other than the node usage. This leads to the unbalanced resource usage of compute nodes. Pod is scheduled to node with higher usage and lower allocation rate. This is not what users expect. Users expect the usage of each node to be balanced.

## Scope
### In scope
* Support node usaged based scheduling.
* Filter nodes whose usage is higher than usage threshold that user defined.
* Prioritize node with node usage and scheduling pod to node with low usage.

### Out of Scope
* The resource oversubscription is not considered in this project.
* Node GPU resource usage is out of scope.

## Design 

### Scheduler Cache
A separated goroutine is created in scheduler cache to talk with Prometheus which is used to collect and aggregate node usage metrics. The node usage data in cache is consumed by usage based scheduling plugin and other plugins like rescheduling plugin. The struct is as below. 
```
type NodeUsage struct {
    cpuUsageAvg map[string]float64
    memUsageAvg map[string]float64
}

type NodeInfo struct {
    …
    ResourceUsage NodeUsage
}
```

### Usage based scheduling plugin

* PredictFn()：Filter nodes whose usage is higher than usage threshold that user defined
* NodeOrder()：Prioritize node with node real-time usage
* Preemptable()：Pod whose node with lower usage is able to preempt pod whose nodes with higher usage

### Scheduler Configuration
```
actions: "enqueue, allocate, backfill"  
tiers:
  - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: usage  # usage based scheduling plugin
        arguments:
          thresholds:
            CPUUsageAvg.5m: 90 # The node whose average usage in 5 minute is higher than 90% will be filtered in predicating stage
            MEMUsageAvg.5m: 80 # The node whose average usage in 5 minute is higher than 80% will be filtered in predicating stage
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                             # metrics server related configuration
  address: http://192.168.0.10:9090  # mandatory, The Prometheus server address
  interval: 30s                      # Optional, The scheduler pull metrics from Prometheus with this interval, 5s by default
  ```

### How to predicate node
The plugins allow user to configure the cpu and memory average threshold within 5m.
Any node whose usage is higher than the value of `CpuUsageAvg.5m` or `MemUsageAvg.5m` is filtered. If no threshold is configured, the node gets into priority stage.
5m average usage is a typical value, more threshold can be added in the future if needed. The key format `CpuUsageAvg.<period>` such as `CpuUsageAvg.1h` . 

### How to prioritize node
There are several factors need to consider while evaluating which node is the best to allocate pod firstly. The first factor is the node average usage in a period of time such as 5m. The node with the lowest usage gets the highest score with this factor. 

The second factor is the node usage fluctuation curve in a period of time.
Suppose there are two nodes with similar usage, The usage of one node fluctuates over a wide range and the other one fluctuates over a narrow range like the `node1` in below tables. The `node1` has higher possibility to get a higher score than `node2`. This is useful to avoid the risk that node get overloaded in peak hours.

The third factor identified is the resource dimension. Take the below table as example. if there is pending pod which is a compute sensitive pod, it is more suitable to schedule it to `node2` with higher mem weight. DRF might be suitable to handle the case to calculate the cpu, mem, gpu share for pod and each node then make the best match.

Finally, there should a model to balance multiple factors with weight and calculate the final score for nodes. Only the cpu usage factor will be considered in the alpha version.

| factors                   | node1           | node2            |
| ----                      | ----            | ---              |
| usage                     | cpu 80%         | cpu 78%          |
| usage fluctuation curve   | 5               | 40               |
| resource dimension        | cpu 80%, mem 20%| cpu 20%, mem 80% |
| ...                       |   ...           |    ...           |
|                           |                 |                  |


### Prometheus rule configuration
The node-exporter is used to monitor the node real-time usage, from which the Prometheus collect the data and aggregate according to the rules. Following Prometheus rules are needed to configured as a example in order to get cpu_usage_avg_5m,cpu_usage_max_avg_1h,cpu_usage_max_avg_1d,mem_usage_avg_5m,mem_usage_max _avg_1h,mem_usage_max_avg_1d etc. 
```
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
    name: example-record
spec:
    groups:
      - name: cpu_mem_usage_active
        interval: 30s
        rules:
        - record: cpu_usage_active
          expr: 100 - (avg by (instance) (irate(node_cpu_seconds_total{mode="idle"}[30s])) * 100)
        - record: mem_usage_active
          expr: 100*(1-node_memory_MemAvailable_bytes/node_memory_MemTotal_bytes)
      - name: cpu-usage-1m
        interval: 1m
        rules:
        - record: cpu_usage_avg_5m
          expr: avg_over_time(cpu_usage_active[5m])
      - name: mem-usage-1m
        interval: 1m
        rules:
        - record: mem_usage_avg_5m
          expr: avg_over_time(mem_usage_active[5m])
```
