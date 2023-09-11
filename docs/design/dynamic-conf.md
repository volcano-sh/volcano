## 1 Introduction

### 1.1 Project Background

Volcano consists of three components: Scheduler, Webhook, and Controller. The Scheduler component schedules jobs by performing a series of actions and plugins to find the most suitable node for them. The Controller Manager manages the lifecycle of CRD (Custom Resource Definition) resources and is mainly composed of Queue ControllerManager, PodGroupControllerManager, and VCJob ControllerManager. The Admission component is responsible for validating CRD API resources.
Currently, the startup configuration information for Volcano components is static and cannot be updated at runtime. For example, the log level of the Scheduler poses a significant burden on administrators during operation and maintenance. It is urgent to add the capability of dynamic and hot configuration updates to improve the efficiency of problem troubleshooting in production environments.

### 1.2 dynamic configuration candidates

The startup configuration information for Volcano components is currently static, and we need to select appropriate configurations and make them dynamic. I will identify all the static parameters used when launching Volcano and choose the configurations that are necessary to be dynamically adjustable under specific scenarios.

```
-v, --v: The level of verbosity for log levels.
```

#### Possible actual use scenarios

1. `-v `The inability to update the log level of Volcano's Scheduler at runtime poses a significant burden on administrators during operation and maintenance. It is crucial to add the capability of dynamic configuration hot updates to enhance the efficiency of problem identification in production environments.

### 1.3 Project scope

Only focus on the static parameters that start the Schedule component

## 2 Implementation Plan

The vc-scheduler pod has an internal socket service to monitor changes in the klog.scok file. Because the socket service in the pod cannot be directly accessed in the k8s Node, the klog.sock file needs to be mounted from the pod to the cluster. Use The curl command modifies the klog.sock file.
[![](https://camo.githubusercontent.com/a5a8fabc657ba344e04992e0844d634dca711dae88954c0a80df6edf38eeeb3e/68747470733a2f2f702e697069632e7669702f3178343533372e706e67#from=url&id=gptPv&originHeight=720&originWidth=1074&originalType=binary&ratio=2&rotation=0&showTitle=false&status=done&style=none&title=)](https://camo.githubusercontent.com/a5a8fabc657ba344e04992e0844d634dca711dae88954c0a80df6edf38eeeb3e/68747470733a2f2f702e697069632e7669702f3178343533372e706e67)
example curl command example

```
sudo curl --unix-socket /tmp/socks/klog.sock "http://localhost/setlevel?level=5&duration=60s"
```

## 3、Why not change klog level throuth change configmap
Volcano is a commercial project. Volcano is deployed in a customer cluster. The configmap cannot be modified easily, but curl can be executed. I thought of using curl to pass in the klog value that you want to modify through the scoket method.