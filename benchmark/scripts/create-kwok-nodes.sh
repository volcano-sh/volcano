#!/usr/bin/env bash
# create-kwok-nodes.sh — Create KWOK simulated nodes
#
# Environment variables required:
#   KWOK_VERSION      - The version of kwok controller to install (e.g. "v0.5.2")
#   KWOK_NODE_COUNT   - The number of simulated nodes to create
#   CPU_PER_NODE      - CPU capacity per node (e.g. "32")
#   MEMORY_PER_NODE   - Memory capacity per node (e.g. "128Gi")

source "$(dirname "$0")/common.sh"
require_cmd kubectl

# Check if KWOK is already installed
function check_kwok_installed() {
    log_info "Checking if KWOK controller is already installed..."
    if kubectl get deployment kwok-controller -n kube-system >/dev/null 2>&1; then
        return 0 # Installed
    else
        return 1 # Not installed
    fi
}

# Install KWOK if not present
function install_kwok_if_needed() {
    if check_kwok_installed; then
        log_info "KWOK controller is already installed, skipping installation."
        return
    fi

    log_info "Installing KWOK CRD and controller (${KWOK_VERSION})..."
    kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/kwok.yaml"
    kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/stage-fast.yaml"

    log_info "Waiting for KWOK controller to be ready..."
    kubectl wait --for=condition=Available deployment/kwok-controller -n kube-system --timeout=120s
}

# Create KWOK nodes
function create_kwok_nodes() {
    # Check if there are already some kwok nodes to avoid recreation issues if needed
    local existing_nodes
    existing_nodes=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [ "$existing_nodes" -ge "$KWOK_NODE_COUNT" ]; then
        log_info "Found ${existing_nodes} KWOK nodes, which satisfies the requested ${KWOK_NODE_COUNT}. Skipping creation."
        return
    fi

    log_info "Creating ${KWOK_NODE_COUNT} KWOK simulated nodes (${CPU_PER_NODE} CPU, ${MEMORY_PER_NODE} Memory)..."
    for i in $(seq 0 $((KWOK_NODE_COUNT - 1))); do
        cat <<EOF
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
        echo "---"
    done | kubectl apply -f -

    log_info "Waiting for all KWOK nodes to be ready..."
    kubectl wait --for=condition=Ready node -l type=kwok --timeout=120s

    local NODE_COUNT
    NODE_COUNT=$(kubectl get nodes -l type=kwok --no-headers | wc -l)
    log_info "KWOK node creation complete, ${NODE_COUNT} nodes total"
}

install_kwok_if_needed
create_kwok_nodes
