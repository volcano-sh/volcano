# Customize Scheduling Algorithms (Plugins) per Queue

## Motivation

The primary use scenario is that different users may need different scheduling algorithms, even in the same clusters. 
The ideal scenario would be the user being able to specify different actions and plugins per Queue. 
However, that will introduce considerable changes in the core logic and might not be backward compatible. 
This project will consist of customizable plugins per Queue as the first step towards that goal.
Actions will be the same throughout the cluster. However, users can specify different plugins for different Queues. 
The proposed feature will be a very beneficial feature for Volcano users.

Reference Issue : [Issue 1035](https://github.com/volcano-sh/volcano/issues/1035)

## Solution

* As of now, users can specify actions and plugins through config-map, providing the same scheduling algorithms across the cluster. 
The project aims to provide an option for users to specify different plugins for different queues. 
Actions will remain the same as the default configuration, but the user can customize different plugins for different queues. 
* Currently, the scheduling configuration is loaded initially, and it's only updated when the underlying scheduling config is changed. 
In a scheduling session, the scheduler traverses through actions ( which were loaded from the scheduling config in the beginning ). 
In each action, it goes through all jobs ( from all Queues ) that are yet to be scheduled and perform actions. 
The proposed feature won't affect this workflow. However, which plugins are executed for jobs belonging to a particular Queue will depend on whether any plugins are specified in the Queue YAML or not.
* If plugins are specified with a Queue, they will be loaded and invoked. Otherwise, default plugins specified in the config-map will be used.

## Implementation

1. Introduce an optional field to specify plugins within Queue CRD spec
2. Scheduler core logic won't change. The scheduler will load default actions and plugins in the beginning. After every 'SchedulePeriod', the scheduler will traverse through default actions. 
3. Each of the execute (Actions) functions traverse through jobs from each Queue. For each of these Queues, we can check whether plugins were specified in the YAML. If yes, then we load those plugins and pass them to functions in `session_plugins.go`.
4. The functions in `session_plugins.go` will have an optional parameter to pass plugins. If plugins are given as a parameter, then they will be used for the corresponding task. Otherwise, it will invoke the default plugins stored in the session object (as it currently does).

### Interaction with existing features

The proposed feature introduces an optional workflow; hence it won't affect existing features.
As of now, queues don't provide any info regarding scheduling config. When the proposed system is implemented, queue YAML will consist of an optional field to specify plugins. Even if the user doesn't specify plugins in the YAML file, it will use the default plugins specified in the config-map passed through the command-line. Hence the proposed feature will be backward compatible. 