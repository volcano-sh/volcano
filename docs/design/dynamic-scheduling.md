# Dynamic scheduling
@william-wang Feb 16 2022

## Motivation
Currently the pod is scheduled based on the resource request and node allocatable resource other than the node usage. This leads to the unbalanced resource usage of compute nodes. Pod is scheduled to node with higher usage and lower allocation rate. This is not what users expect. Users expect the usage of each node to be balanced.

## Scope
### In scope
* Support dynamic scheduling based on the node usage.
* Filter nodes whose usage is higher than usage threshold that user defined.
* Prioritize node with node usage and scheduling pod to node with low usage.

### Out of Scope
* The resource oversubscription is not considered in this project.
* Node GPU resource usage is out of scope.

## Design 

### Scheduler Cache
A separated goroutine is created in scheduler cache to talk with Prometheus which is used to collect and aggregate node usage metrics. The node usage data in cache is consumed by dynamic scheduler plugin and other plugins like rescheduling plugin. The struct is as below. 
```
type NodeUsage struct {
    cpuUsageAvg5m    float64  
    cpuUsageMaxAvg1h float64
    cpuUsageMaxAvg1d float64
    memUsageAvg5m    float64
    memUsageMaxAvg1h float64
    memUsageMaxAvg1d float64
}

type NodeInfo struct {
    …
    ResourceUsage NodeUsage
}
```

### Dynamic scheduler plugin

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
      - name: dynamicScheduling  # dynamic scheduling plugin
        arguments:
          threshholds:
            cpuUsageAvg5m: 80%
            cpuUsageMaxAvg1h: 90%
            memUsageAvg5m: 80%
            memUsageMaxAvg1h: 90%
          weight:
           	cpuUsageAvg5m: 0.3
          	cpuUsageMaxAvg1h：0.1
            cpuUsageMaxAvg1d：0.1
          	memUsageAvg5m：0.3
            memUsageMaxAvg1h：0.1
            memUsageMaxAvg1d：0.1         
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                # metrics server related configuration
  address: 192.168.0.10 # mandatory, The Prometheus server address
  port: 9090            # mandatory, The Prometheus server port
  interval: 5s          # Optional, The scheduler pull metrics from Prometheus with this inteval, 5s by default
  ```

### Prometheus rule configuration
The node-exporter is used to monitor the node real-time usage, from which the Prometheus collect the data and aggregate accoring to the rules. Following Prometheus rules are needed to configured as a example in order to get cpu_usage_avg_5m,cpu_usage_max_avg_1h,cpu_usage_max_avg_1d,mem_usage_avg_5m,mem_usage_max _avg_1h,mem_usage_max_avg_1d etc. 
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
      - name: cpu-usage-5m
        interval: 5m
        rules:
        - record: cpu_usage_max_avg_1h
          expr: max_over_time(cpu_usage_avg_5m[1h])
        - record: cpu_usage_max_avg_1d
          expr: max_over_time(cpu_usage_avg_5m[1d])
      - name: cpu-usage-1m
        interval: 1m
        rules:
        - record: cpu_usage_avg_5m
          expr: avg_over_time(cpu_usage_active[5m])
      - name: mem-usage-5m
        interval: 5m
        rules:
        - record: mem_usage_max_avg_1h
          expr: max_over_time(mem_usage_avg_5m[1h])
        - record: mem_usage_max_avg_1d
          expr: max_over_time(mem_usage_avg_5m[1d])
      - name: mem-usage-1m
        interval: 1m
        rules:
        - record: mem_usage_avg_5m
          expr: avg_over_time(mem_usage_active[5m])
```
