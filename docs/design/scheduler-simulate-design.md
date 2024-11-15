# Volcano scheduler simulate
## background
* Consider such situation: users changed the parameter of nodeorder plugin and need to know the effect to the production enviroment. For example, after change the mostrequested.weight, if the average wait time of big task is shorter than before, etc.
* Currently, users must use real cluster to verify the effect, which is very time consuming and need a lot of node resources.
* Scheduler simulate is an effect solution of the problem above. It runs fast and needs much lesser resources than real cluster.
## design
* The simulator includes simulator command, simulate-data-input, simulate-result-output, time simulator, kube-apiserver simulator
### simulator command
* scheduler-simulate can be runned by command: vc-scheduler-simulate --running_time=24h --config_path=./config --output_path=./result
### simulate-data-input
* Simulate data includes nodes.csv, pods.csv, queues.csv. These files can be put in config_path directory.
* nodes.csv includes cpu_allocatable, memory_allocatable, label, maxPodNum
* pods.csv includes cpu_request, memory_request, runsec, cron, createtime, nodeSelector, priority, queueName
* queues.csv includes queueName, quota
#### pod
#### queue
### simulate-result-output
#### file
* output file includes nodes_detail.csv, queues_detail.csv, pods_detail.csv, pods_detail.csv. These files will be put in output_path directory.
* nodes_detail.csv includes ts, nodeName, cpuRequest, memoryRequest
* queues_detail.csv includes ts, queueName, cpuRequest, memoryRequest
* pods_detail.csv includes podName, createTs, scheduleTs, finishTs
#### metrics
* output metrics includes volcano_simulator_scheduler_wait_time, volcano_simulator_pod_run_time, volcano_simulator_node_cpu_request, volcano_simulator_node_memory_request which will help user to monitor the schduler simulate by prometheus.
### time simulator
* Time simulator is helpful to shorten simulate time because it will not get the time of real world, it will always get next timestamp of the min value between the create time of next pod and the finish time of next pod.
* The time related parameter should get from time simulator like pod create time, pod finish time, current time, etc...
### kube-apiserver simulator
* kube-apiserver simulator is the simulation of k8s component including apiserver, controller manager and etcd.
* The simulator is a http server just like the real kube-apiserver, which includes submit, modify and watch of different kinds of resources.
* To realize the kube-apiserver simulator, we should realize part of the function of apiserver. First, we should focus on the http url like /api/v1/namespaces/{namespace}/pods to enable the submit of different kinds of resources like pod, queue, podgroup and save them in memory; Second, we should focus on the http url like /api/v1/namespaces/{namespace}/pods?watch=true to enable the watch of different kinds of resources by channel which will support the informer of volcano-scheduler; Third, we should query the pods that are about to end in memory and notify the client-go informer by stream which is part of the function of controller-manager; Last, we should management the status of pod in memory.