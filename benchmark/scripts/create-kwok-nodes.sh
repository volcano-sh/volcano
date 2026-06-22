#!/usr/bin/env bash
# create-kwok-nodes.sh — Create KWOK simulated nodes with optional topology domain support
#
# Usage:
#   ./scripts/create-kwok-nodes.sh                                          # 100 flat nodes
#   KWOK_NODE_COUNT=200 ./scripts/create-kwok-nodes.sh                      # 200 flat nodes
#   ENABLE_TOPOLOGY=true ./scripts/create-kwok-nodes.sh                     # 100 nodes in 4 racks / 2 spines (labeled)
#   KWOK_NODE_LABELS="topology-rack=rack-5" ./scripts/create-kwok-nodes.sh  # 100 nodes with custom labels (for auto-discovery)
#
# Environment variables:
#   KWOK_VERSION      - The version of kwok controller to install (e.g. "v0.7.0")
#   KWOK_NODE_COUNT   - The number of simulated nodes to create (default: 100)
#   CPU_PER_NODE      - CPU capacity per node (default: "32")
#   MEMORY_PER_NODE   - Memory capacity per node (default: "256Gi")
#
# Topology variables (optional, only used when ENABLE_TOPOLOGY=true):
#   ENABLE_TOPOLOGY   - Set to "true" to distribute nodes across topology domains with labels
#   TOPOLOGY_RACKS    - Number of rack-level domains (tier 1). Default: 4
#   TOPOLOGY_SPINES   - Number of spine-level domains (tier 2). Default: 2
#   NODES_PER_RACK    - Nodes per rack. Default: KWOK_NODE_COUNT / TOPOLOGY_RACKS
#
# Custom labels (optional, can be combined with ENABLE_TOPOLOGY or used standalone):
#   KWOK_NODE_LABELS  - Comma-separated key=value pairs to add as extra labels on ALL created nodes.
#                       Useful when pairing with hypernode-controller auto-discovery: the controller
#                       watches node labels and automatically builds HyperNodes without manual CRD creation.
#                       Example: KWOK_NODE_LABELS="topology-rack=rack-0,topology-spine=spine-0,gpu=A100"
#   EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK
#                     - Patch common system DaemonSets so they do not run on KWOK nodes (default: true).
#
# Topology layout (when ENABLE_TOPOLOGY=true, TOPOLOGY_RACKS=4, TOPOLOGY_SPINES=2):
#   spine-0 (tier 2)
#   ├── rack-0 (tier 1) ─── kwok-node-0, kwok-node-1, ...
#   ├── rack-1 (tier 1) ─── kwok-node-N, kwok-node-N+1, ...
#   spine-1 (tier 2)
#   ├── rack-2 (tier 1) ─── ...
#   └── rack-3 (tier 1) ─── ...
#
# HyperNode creation:
#   This script only creates KWOK nodes with topology labels. HyperNode CRDs
#   must be created separately AFTER Volcano is installed (see create-hypernodes.sh).
#   Alternatively, use auto-discovery mode: the hypernode-controller watches
#   node labels and auto-builds HyperNodes via ConfigMap configuration.

source "$(dirname "$0")/common.sh"
require_cmd kubectl

# Topology defaults
ENABLE_TOPOLOGY="${ENABLE_TOPOLOGY:-false}"
TOPOLOGY_RACKS="${TOPOLOGY_RACKS:-4}"
TOPOLOGY_SPINES="${TOPOLOGY_SPINES:-2}"

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

# Compute nodes per rack when topology is enabled
function compute_nodes_per_rack() {
    if [[ -n "${NODES_PER_RACK:-}" ]]; then
        echo "${NODES_PER_RACK}"
    else
        echo $(( KWOK_NODE_COUNT / TOPOLOGY_RACKS ))
    fi
}

# Parse KWOK_NODE_LABELS into YAML label lines (cached, computed once)
KWOK_NODE_LABELS="${KWOK_NODE_LABELS:-}"
EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK="${EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK:-true}"
function _parse_custom_labels() {
    local result=""
    if [[ -n "${KWOK_NODE_LABELS}" ]]; then
        IFS=',' read -ra pairs <<< "${KWOK_NODE_LABELS}"
        for pair in "${pairs[@]}"; do
            local key="${pair%%=*}"
            local val="${pair#*=}"
            result="${result}
    ${key}: \"${val}\""
        done
    fi
    echo "${result}"
}
CUSTOM_LABEL_YAML="$(_parse_custom_labels)"

# Kind installs kindnet and kube-proxy as DaemonSets. They tolerate broad node
# taints, so the KWOK taint alone does not prevent one pod per simulated node.
function patch_daemonset_exclude_kwok_nodes() {
    local namespace="$1"
    local name="$2"

    if ! kubectl -n "${namespace}" get daemonset "${name}" >/dev/null 2>&1; then
        log_warn "DaemonSet ${namespace}/${name} not found, skipping KWOK exclusion patch"
        return
    fi

    log_info "Patching DaemonSet ${namespace}/${name} to exclude KWOK nodes..."
    kubectl -n "${namespace}" patch daemonset "${name}" --type=merge -p '{
      "spec": {
        "template": {
          "spec": {
            "affinity": {
              "nodeAffinity": {
                "requiredDuringSchedulingIgnoredDuringExecution": {
                  "nodeSelectorTerms": [
                    {
                      "matchExpressions": [
                        {
                          "key": "type",
                          "operator": "NotIn",
                          "values": ["kwok"]
                        }
                      ]
                    }
                  ]
                }
              }
            }
          }
        }
      }
    }' >/dev/null
}

function delete_daemonset_pods_on_kwok_nodes() {
    local namespace="$1"
    local label_selector="$2"
    local names

    names=$(kubectl -n "${namespace}" get pods -l "${label_selector}" \
        -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.nodeName}{"\n"}{end}' 2>/dev/null \
        | awk '$2 ~ /^kwok-node-/ {print $1}')

    if [[ -z "${names}" ]]; then
        return
    fi

    log_info "Deleting existing ${namespace} pods matching '${label_selector}' from KWOK nodes..."
    echo "${names}" | xargs -r -n 100 kubectl -n "${namespace}" delete pod --ignore-not-found --wait=false
}

function exclude_system_daemonsets_from_kwok_nodes() {
    if [[ "${EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK}" != "true" ]]; then
        log_info "Skipping system DaemonSet KWOK exclusion (EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK=false)"
        return
    fi

    patch_daemonset_exclude_kwok_nodes kube-system kindnet
    patch_daemonset_exclude_kwok_nodes kube-system kube-proxy
}

function cleanup_system_daemonset_pods_on_kwok_nodes() {
    if [[ "${EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK}" != "true" ]]; then
        return
    fi

    delete_daemonset_pods_on_kwok_nodes kube-system 'app=kindnet'
    delete_daemonset_pods_on_kwok_nodes kube-system 'k8s-app=kube-proxy'
}

function wait_for_kwok_nodes_ready() {
    local timeout="${1:-120}"
    local deadline=$((SECONDS + timeout))
    local total
    local ready

    log_info "Waiting for KWOK nodes to be ready (timeout ${timeout}s)..."
    while (( SECONDS < deadline )); do
        total=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | wc -l | tr -d ' ')
        ready=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | awk '$2 == "Ready" {count++} END {print count + 0}')

        if [[ "${total}" != "0" && "${ready}" == "${total}" ]]; then
            log_info "All KWOK nodes are ready (${ready}/${total})."
            return
        fi

        sleep 2
    done

    log_error "Timed out waiting for KWOK nodes to be ready (${ready:-0}/${total:-0})."
    return 1
}

# Create a single KWOK node with optional topology labels
# Args: $1=node_index, $2=rack_label (optional), $3=spine_label (optional)
function create_single_node() {
    local idx=$1
    local rack_label="${2:-}"
    local spine_label="${3:-}"

    local extra_labels=""
    if [[ -n "${rack_label}" ]]; then
        extra_labels="${extra_labels}
    topology-rack: \"${rack_label}\""
    fi
    if [[ -n "${spine_label}" ]]; then
        extra_labels="${extra_labels}
    topology-spine: \"${spine_label}\""
    fi
    # Append custom labels from KWOK_NODE_LABELS
    extra_labels="${extra_labels}${CUSTOM_LABEL_YAML}"

    cat <<EOF
apiVersion: v1
kind: Node
metadata:
  name: kwok-node-${idx}
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kwok-node-${idx}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok${extra_labels}
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
}

# Create KWOK nodes without topology
function create_kwok_nodes_flat() {
    local existing_nodes
    existing_nodes=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [ "$existing_nodes" -ge "$KWOK_NODE_COUNT" ]; then
        log_info "Found ${existing_nodes} KWOK nodes, which satisfies the requested ${KWOK_NODE_COUNT}. Skipping creation."
        return
    fi

    log_info "Creating ${KWOK_NODE_COUNT} KWOK simulated nodes (${CPU_PER_NODE} CPU, ${MEMORY_PER_NODE} Memory)..."
    for i in $(seq 0 $((KWOK_NODE_COUNT - 1))); do
        create_single_node "${i}"
        echo "---"
    done | kubectl apply -f -
}

# Create KWOK nodes distributed across topology domains
function create_kwok_nodes_with_topology() {
    local nodes_per_rack
    nodes_per_rack=$(compute_nodes_per_rack)
    local racks_per_spine=$(( TOPOLOGY_RACKS / TOPOLOGY_SPINES ))
    local total_nodes=$(( nodes_per_rack * TOPOLOGY_RACKS ))

    log_info "Creating ${total_nodes} KWOK nodes with topology: ${TOPOLOGY_SPINES} spines × ${racks_per_spine} racks/spine × ${nodes_per_rack} nodes/rack"

    local existing_nodes
    existing_nodes=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [ "$existing_nodes" -ge "$total_nodes" ]; then
        log_info "Found ${existing_nodes} KWOK nodes, which satisfies the requested ${total_nodes}. Skipping creation."
        return
    fi

    local node_idx=0
    for spine in $(seq 0 $((TOPOLOGY_SPINES - 1))); do
        for rack_in_spine in $(seq 0 $((racks_per_spine - 1))); do
            local rack_idx=$(( spine * racks_per_spine + rack_in_spine ))
            for _ in $(seq 1 "${nodes_per_rack}"); do
                create_single_node "${node_idx}" "rack-${rack_idx}" "spine-${spine}"
                echo "---"
                node_idx=$((node_idx + 1))
            done
        done
    done | kubectl apply -f -

    KWOK_NODE_COUNT=${total_nodes}
}

# Main execution
install_kwok_if_needed
exclude_system_daemonsets_from_kwok_nodes

if [[ "${ENABLE_TOPOLOGY}" == "true" ]]; then
    create_kwok_nodes_with_topology
    wait_for_kwok_nodes_ready 120
else
    create_kwok_nodes_flat
    wait_for_kwok_nodes_ready 120
fi

cleanup_system_daemonset_pods_on_kwok_nodes

local_node_count=$(kubectl get nodes -l type=kwok --no-headers | wc -l | tr -d ' ')
log_info "KWOK node creation complete, ${local_node_count} nodes total"
