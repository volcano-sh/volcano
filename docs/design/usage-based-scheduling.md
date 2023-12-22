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
A separated goroutine is created in scheduler cache to talk with Metrics source(like prometheus, elasticsearch) which is used to collect and aggregate node usage metrics. The node usage data in cache is consumed by usage based scheduling plugin and other plugins like rescheduling plugin. The struct is as below. 
```
type NodeUsage struct {
    MetricsTime time.Time
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
        enablePredicate: false  # If the value is false, new pod scheduling is not disabled when the node load reaches the threshold. If the value is true or left blank, new pod scheduling is disabled.
        arguments:
          usage.weight: 5
          cpu.weight: 1
          memory.weight: 1
          thresholds:
            cpu: 80    # The actual CPU load of a node reaches 80%, and the node cannot schedule new pods.
            mem: 70    # The actual Memory load of a node reaches 70%, and the node cannot schedule new pods.
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                               # metrics server related configuration
  type: prometheus                     # Optional, The metrics source type, prometheus by default, support "prometheus", "prometheus_adapt" and "elasticsearch"
  address: http://192.168.0.10:9090    # Mandatory, The metrics source address
  interval: 30s                        # Optional, The scheduler pull metrics from Prometheus with this interval, 30s by default
  tls:                                 # Optional, The tls configuration
    insecureSkipVerify: "false"        # Optional, Skip the certificate verification, false by default
  elasticsearch:                       # Optional, The elasticsearch configuration
    index: "custom-index-name"         # Optional, The elasticsearch index name, "metricbeat-*" by default
    username: ""                       # Optional, The elasticsearch username
    password: ""                       # Optional, The elasticsearch password
    hostnameFieldName: "host.hostname" # Optional, The elasticsearch hostname field name, "host.hostname" by default
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

### Configuration and usage of different monitoring systems
The monitoring data of Volcano usage can be obtained from "Prometheus", "Custom Metrics API" and "Eleasticsearch", where the corresponding type of "Custom Metrics Api" is "prometheus_adapt".

**It is recommended to use the Custom Metrics API mode, and the monitoring indicators come from Prometheus Adapt.**

#### Custom Metrics API
Ensure that Prometheus Adaptor is properly installed in the cluster and the custom metrics API is available.
Set the user-defined indicator information. The rules to be added are as follows. For details, see [Metrics Discovery and Presentation Configuration](https://github.com/kubernetes-sigs/prometheus-adapter/blob/master/docs/config.md#metrics-discovery-and-presentation-configuration)
```
rules:
    - seriesQuery: '{__name__=~"node_cpu_seconds_total"}'
      resources:
        overrides:
          instance:
            resource: node
      name:
        matches: "node_cpu_seconds_total"
        as: "node_cpu_usage_avg"
      metricsQuery: avg_over_time((1 - avg (irate(<<.Series>>{mode="idle"}[5m])) by (instance))[10m:30s])
    - seriesQuery: '{__name__=~"node_memory_MemTotal_bytes"}'
      resources:
        overrides:
          instance:
            resource: node
      name:
        matches: "node_memory_MemTotal_bytes"
        as: "node_memory_usage_avg"
      metricsQuery: avg_over_time(((1-node_memory_MemAvailable_bytes/<<.Series>>))[10m:30s])
```
Scheduler Configuration:
```
actions: "enqueue, allocate, backfill"  
tiers:
  - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: usage  # usage based scheduling plugin
        enablePredicate: false  # If the value is false, new pod scheduling is not disabled when the node load reaches the threshold. If the value is true or left blank, new pod scheduling is disabled.
        arguments:
          usage.weight: 5
          cpu.weight: 1
          memory.weight: 1
          thresholds:
            cpu: 80    # The actual CPU load of a node reaches 80%, and the node cannot schedule new pods.
            mem: 70    # The actual Memory load of a node reaches 70%, and the node cannot schedule new pods.
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                               # metrics server related configuration
  type: prometheus_adaptor               # Optional, The metrics source type, prometheus by default, support "prometheus", "prometheus_adaptor" and "elasticsearch"
  interval: 30s                        # Optional, The scheduler pull metrics from Prometheus with this interval, 30s by default
  ```

#### Prometheus
Scheduler Configuration:
```
actions: "enqueue, allocate, backfill"  
tiers:
  - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: usage  # usage based scheduling plugin
        enablePredicate: false  # If the value is false, new pod scheduling is not disabled when the node load reaches the threshold. If the value is true or left blank, new pod scheduling is disabled.
        arguments:
          usage.weight: 5
          cpu.weight: 1
          memory.weight: 1
          thresholds:
            cpu: 80    # The actual CPU load of a node reaches 80%, and the node cannot schedule new pods.
            mem: 70    # The actual Memory load of a node reaches 70%, and the node cannot schedule new pods.
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                               # metrics server related configuration
  type: prometheus                     # Optional, The metrics source type, prometheus by default, support "prometheus", "prometheus_adaptor" and "elasticsearch"
  address: http://192.168.0.10:9090    # Mandatory, The metrics source address
  interval: 30s                        # Optional, The scheduler pull metrics from Prometheus with this interval, 30s by default
  ```

### Elesticsearch
Scheduler Configuration
```
actions: "enqueue, allocate, backfill"  
tiers:
  - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: usage  # usage based scheduling plugin
        enablePredicate: false  # If the value is false, new pod scheduling is not disabled when the node load reaches the threshold. If the value is true or left blank, new pod scheduling is disabled.
        arguments:
          usage.weight: 5
          cpu.weight: 1
          memory.weight: 1
          thresholds:
            cpu: 80    # The actual CPU load of a node reaches 80%, and the node cannot schedule new pods.
            mem: 70    # The actual Memory load of a node reaches 70%, and the node cannot schedule new pods.
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
metrics:                               # metrics server related configuration
  type: elasticsearch                  # Optional, The metrics source type, prometheus by default, support "prometheus", "prometheus_adaptor" and "elasticsearch"
  address: http://192.168.0.10:9090    # Mandatory, The metrics source address
  interval: 30s                        # Optional, The scheduler pull metrics from Prometheus with this interval, 30s by default
  tls:                                 # Optional, The tls configuration
    insecureSkipVerify: "false"        # Optional, Skip the certificate verification, false by default
  elasticsearch:                       # Optional, The elasticsearch configuration
    index: "custom-index-name"         # Optional, The elasticsearch index name, "metricbeat-*" by default
    username: ""                       # Optional, The elasticsearch username
    password: ""                       # Optional, The elasticsearch password
    hostnameFieldName: "host.hostname" # Optional, The elasticsearch hostname field name, "host.hostname" by default
  ```