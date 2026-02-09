#!/usr/bin/env bash
# create-kwok-nodes.sh — Create KWOK simulated nodes
# Reads KWOK Stage configs from the scenario's manifests directory.

source "$(dirname "$0")/common.sh"
require_cmd kubectl

log_info "Installing KWOK CRD and controller (${KWOK_VERSION})..."
kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/kwok.yaml"
kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/stage-fast.yaml"

log_info "Waiting for KWOK controller to be ready..."
kubectl wait --for=condition=Available deployment/kwok-controller -n kube-system --timeout=120s

log_info "Applying KWOK Stage configurations from ${SCENARIO_DIR}/manifests/kwok/..."
kubectl apply -f "${SCENARIO_DIR}/manifests/kwok/node-heartbeat.yaml"
kubectl apply -f "${SCENARIO_DIR}/manifests/kwok/pod-complete.yaml"
kubectl apply -f "${SCENARIO_DIR}/manifests/kwok/pod-delete.yaml"

log_info "Creating ${KWOK_NODE_COUNT} KWOK simulated nodes (${CPU_PER_NODE} CPU, ${MEMORY_PER_NODE} Memory)..."
for i in $(seq 0 $((KWOK_NODE_COUNT - 1))); do
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Node
metadata:
  name: kwok-node-${i}
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kwok-node-${i}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok
spec:
  taints:
    - key: kwok.x-k8s.io/node
      value: fake
      effect: NoSchedule
status:
  allocatable:
    cpu: "${CPU_PER_NODE}"
    memory: "${MEMORY_PER_NODE}"
    pods: "110"
  capacity:
    cpu: "${CPU_PER_NODE}"
    memory: "${MEMORY_PER_NODE}"
    pods: "110"
  conditions:
    - type: Ready
      status: "True"
      reason: KubeletReady
      message: "kubelet is posting ready status"
      lastHeartbeatTime: "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      lastTransitionTime: "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  phase: Running
EOF
done

log_info "Waiting for all KWOK nodes to be ready..."
kubectl wait --for=condition=Ready node -l type=kwok --timeout=120s

NODE_COUNT=$(kubectl get nodes -l type=kwok --no-headers | wc -l)
log_info "KWOK node creation complete, ${NODE_COUNT} nodes total"
