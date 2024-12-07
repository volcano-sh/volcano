#!/bin/bash
#
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

show_help() {
  cat << EOF
Invalid option: -$OPTARG
Usage: $0 [-d] [-v] [-i <interval>] <deployment_count> <replicas_count>

Options:
  -d, --delete               Delete the specified number of deployments.
  -v, --volcano              use volcano scheduler.
  -a, --affinity             test affinity and non-affinity.
  -i, --interval <interval>  Set the interval between deployments in seconds.

Arguments:
  <deployment_count>         Number of deployments to create or delete (required).
  <replicas_count>           Number of replicas for each deployment (required).
EOF
}

deploy_queue() {
  kubectl apply -f - <<EOF
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: test
spec:
  weight: 1
  reclaimable: false
  capability:
    cpu: "32"
    memory: "128Gi"
EOF
}

deploy_deployments() {
  for (( i=0; i<deployment_count; i++ )); do
    # 设置调度器名称
    if [ "$volcano" = true ]; then
      schedulerName="volcano"
    else
      schedulerName="default-scheduler"
    fi
    echo "Scheduler Name: $schedulerName"

    kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-deployment-$i
  labels:
    app: sleep
    applicationId: "sleep-deployment-$i"
spec:
  replicas: $replicas_count
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
        applicationId: "sleep-deployment-$i"
#      annotations:
#        scheduling.volcano.sh/queue-name: test
    spec:
      schedulerName: $schedulerName
      containers:
        - name: sleep300
          image: "alpine:latest"
      tolerations:
        - key: "kwok.x-k8s.io/node"
          operator: "Exists"
          effect: "NoSchedule"
EOF
    sleep "$interval"
  done
}

deploy_deployments_withAff() {
  for (( i=0; i<deployment_count; i++ )); do
    # 计算当前副本的百分比
    percent=$(( 100 * i / deployment_count ))

    # 随机分配一个节点和应用ID
    randHost=$((RANDOM % 1000))  # 1000个节点
    randAppID=$((RANDOM % deployment_count))

    # 设置调度器名称
    if [ "$volcano" = true ]; then
      schedulerName="volcano"
    else
      schedulerName="default-scheduler"
    fi
    echo "Scheduler Name: $schedulerName"

    # 根据百分比来设置亲和性策略
    if [ $percent -lt 25 ]; then
      # 前 25% 副本：Preferred-In
      affinity_type="preferredDuringSchedulingIgnoredDuringExecution"
      operator="In"
    elif [ $percent -lt 50 ]; then
      # 接着 25% 副本：Preferred-NotIn
      affinity_type="preferredDuringSchedulingIgnoredDuringExecution"
      operator="NotIn"
    fi

    if [ $percent -lt 50 ]; then
      kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-deployment-$i
  labels:
    app: sleep
    applicationId: "sleep-deployment-$i"
spec:
  replicas: $replicas_count
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
        applicationId: "sleep-deployment-$i"
    spec:
      schedulerName: $schedulerName
      containers:
        - name: sleep300
          image: "alpine:latest"
      tolerations:
        - key: "kwok.x-k8s.io/node"
          operator: "Exists"
          effect: "NoSchedule"
      affinity:
        nodeAffinity:
          $affinity_type:
            - weight: 100
              preference:
                matchExpressions:
                  - key: kubernetes.io/hostname
                    operator: $operator
                    values:
                      - kwok-node-$randHost
EOF
    fi

    if [ $percent -lt 75 ]; then
      # 接着 25% 副本：Required-In
      affinity_type="requiredDuringSchedulingIgnoredDuringExecution"
      operator="In"
    else
      # 最后 25% 副本：Required-NotIn
      affinity_type="requiredDuringSchedulingIgnoredDuringExecution"
      operator="NotIn"
    fi

    if [ $percent -ge 50 ]; then
      kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-deployment-$i
  labels:
    app: sleep
    applicationId: "sleep-deployment-$i"
spec:
  replicas: $replicas_count
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
        applicationId: "sleep-deployment-$i"
        queue: root.default
    spec:
      schedulerName: $schedulerName
      containers:
        - name: sleep300
          image: "alpine:latest"
      tolerations:
        - key: "kwok.x-k8s.io/node"
          operator: "Exists"
          effect: "NoSchedule"
      affinity:
        nodeAffinity:
          $affinity_type:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: kubernetes.io/hostname
                    operator: $operator
                    values:
                      - kwok-node-$randHost
EOF
    fi

    sleep "$interval"
  done
}


delete_deployments(){
    for (( i=0; i<deployment_count; i++ )); do
        kubectl delete deploy/sleep-deployment-"$i"
    done
}

# Default values
volcano=false
delete=false
aff=false
interval=0

# Process command-line options
while getopts ":dvai:" opt; do
  case $opt in
    d)
      delete=true
      ;;
    v)
      volcano=true
      ;;
    a)
      aff=true
      ;;
    i)
      interval=$OPTARG
      ;;
    \?)
      show_help
      exit 1
      ;;
    :)
      show_help
      exit 1
      ;;
  esac
done

# Shift the processed options out of the command-line arguments
shift $((OPTIND-1))

# Check if deployment count and replicas count are provided
if [ $# -ne 2 ]; then
  show_help
  exit 1
fi

# Assign provided values to variables
deployment_count=$1
replicas_count=$2

# Check if delete flag is set
if [ "$delete" = true ]; then
  echo "Deleting $deployment_count deployments with $replicas_count replicas each."
  delete_deployments
else
  if [ "$volcano" = true ]; then
    deploy_queue
  fi
  if [ "$aff" = true ]; then
    echo "Deploying $deployment_count deployments with $replicas_count replicas each, with affinity."
    deploy_deployments_withAff
  else
    echo "Deploying $deployment_count deployments with $replicas_count replicas each"
    deploy_deployments
  fi
fi

exit 0