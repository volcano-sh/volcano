# A easy way to deploy multi-volcano-scheduler without using selector

## Background

Currently the single scheduler can not satisfy the high throughput requirement in some scenarios. Besides the performance optimization against the single scheduler, another choice is to deploy multiple volcano schedulers to improve the overall scheduling throughput.

## Introduction
Previously we use label to divide the cluster nodes to multiple sections and each volcano scheduler is responsible for one section and then specify the schedulerName in the Pod Spec and submit it. It is inconvenient in some conditions especially for large clusters. This doc privides a another option for user to deploy multiple scheduler which needs less modification for workload and nodes.
A statefulset is used to deploy the volcano scheduler. The Job and Node are assigned to scheduler automatically based on the hash algorithm. 

！[multi-scheduler-deployment](images/multi-volcano-schedulers-without-using-selector.png) 

## How to deployment

### 1. Prepare the volcano scheduler yaml file. Here is a example for your reference.
```
kind: StatefulSet
apiVersion: apps/v1
metadata:
  name: volcano-scheduler
  namespace: volcano-system
  labels:
    app: volcano-scheduler
spec:
  replicas: 3
  selector:
    matchLabels:
      app: volcano-scheduler
  serviceName: "volcano-scheduler"
  template:
    metadata:
      labels:
        app: volcano-scheduler
    spec:
      serviceAccount: volcano-scheduler
      containers:
        - name: volcano-scheduler
          image: volcanosh/vc-scheduler:ae78900d21dce8522eb04b6817aac66c9abd01e2
          args:
            - --logtostderr
            - --scheduler-conf=/volcano.scheduler/volcano-scheduler.conf
            - -v=3
            - 2>&1
          imagePullPolicy: "IfNotPresent"
          env:
            - name: MULTI_SCHEDULER_ENABLE
              value: "true"
            - name: SCHEDULER_NUM
              value: "3"
            - name: SCHEDULER_POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
          volumeMounts:
            - name: scheduler-config
              mountPath: /volcano.scheduler
      volumes:
        - name: scheduler-config
          configMap:
            name: volcano-scheduler-configmap
apiVersion: v1
kind: Service
metadata:
  name: volcano-scheduler
  labels:
    app: volcano-scheduler
spec:
  ports:
  - port: 80
    name: volcano-scheduler
  clusterIP: None
  selector:
    app: volcano-scheduler
```            

Notes:
1. MULTI_SCHEDULER_ENABLE env is used to enable or disable  multi-scheduler.
2. SCHEDULER_NUM indicates the numbers of volcano schedulers which you are planning to launch.

### 2. Deploy the statefulset
```
kubectl apply -f <volcano-statefulset.yaml>
```

