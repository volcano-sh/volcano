#!/bin/bash

# Copyright 2024 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# default parameters
NODE_COUNT=1
CPU="32"
MEMORY="256Gi"
PODS="110"
EXTENDED_RESOURCES=""
BASE_NODE_NAME="kwok-node"

# parse command line parameters
while getopts ":n:b:c:m:p:e:h" opt; do
  case $opt in
    n) NODE_COUNT="$OPTARG" ;;
    b) BASE_NODE_NAME="$OPTARG" ;;
    c) CPU="$OPTARG" ;;
    m) MEMORY="$OPTARG" ;;
    p) PODS="$OPTARG" ;;
    e) EXTENDED_RESOURCES="$OPTARG" ;;
    h)
       echo "Usage: $0 [options]"
       echo "   -n NODE_COUNT   Number of nodes to create (default: 1)"
       echo "   -b BASE_NODE_NAME Base name for nodes (default: kwok-node)"
       echo "   -c CPU          Amount of CPU resources that can be allocated (default: 32)"
       echo "   -m MEMORY       Amount of memory resources that can be allocated (default: 256Gi)"
       echo "   -p PODS         Number of pods can be allocated (default: 110)"
       echo "   -e EXTENDED_RESOURCES   Pairs of amount of extended resources that can be allocated, e.g., 'gpu=1,npu=2'"
       echo "   -h              Display this help message"
       exit 0
       ;;
    \?)
        echo "Invalid option: -$OPTARG" >&2
        exit 1
        ;;
  esac
done
    
# parse extended resources if have
parse_extended_resources(){
    local resources=$1
    local result=""
    if [[ -n "$resources" ]]; then
      IFS=',' read -ra PAIRS <<< "$resources"
      for pair in "${PAIRS[@]}"; do
        IFS='=' read -ra KV <<< "$pair"
        key="${KV[0]}"
        value="${KV[1]}"
        result="${result}
    $key: $value"
      done
    fi
    echo "$result"
}

EXTENDED_RESOURCES_YAML=$(parse_extended_resources "$EXTENDED_RESOURCES")

# create kwok fake nodes
for ((i=0; i<NODE_COUNT; i++)) 
do
    NODE_NAME="${BASE_NODE_NAME}-${i}"
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: $NODE_NAME
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok
  name: $NODE_NAME
spec:
  taints:
  - effect: NoSchedule
    key: kwok.x-k8s.io/node
    value: fake
status:
  allocatable:
    cpu: $CPU
    memory: $MEMORY
    pods: $PODS$EXTENDED_RESOURCES_YAML
  capacity:
    cpu: $CPU
    memory: $MEMORY
    pods: $PODS$EXTENDED_RESOURCES_YAML
  nodeInfo:
    architecture: amd64
    bootID: ""
    containerRuntimeVersion: ""
    kernelVersion: ""
    kubeProxyVersion: fake
    kubeletVersion: fake
    machineID: ""
    operatingSystem: linux
    osImage: ""
    systemUUID: ""
  phase: Running
EOF
done
