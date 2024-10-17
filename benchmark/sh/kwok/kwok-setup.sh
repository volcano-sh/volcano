#!/bin/bash
# Copyright 2019 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

if [ $# -eq 0 ]; then
  echo "Error: Please provide the number of nodes to create."
  echo "Usage: $0 <number_of_nodes>"
  exit 1
fi

KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE=$(curl "https://api.github.com/repos/${KWOK_REPO}/releases/latest" | jq -r '.tag_name')
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"

for (( i=0;i<$1; i++))
do
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
      kubernetes.io/hostname: kwok-node-$i
      kubernetes.io/os: linux
      kubernetes.io/role: agent
      node-role.kubernetes.io/agent: ""
      type: kwok
    name: kwok-node-$i
  spec:
    taints: # Avoid scheduling actual running pods to fake Node
    - effect: NoSchedule
      key: kwok.x-k8s.io/node
      value: fake
  status:
    allocatable:
      cpu: 32
      memory: 256Gi
      pods: 110
    capacity:
      cpu: 32
      memory: 256Gi
      pods: 110
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

