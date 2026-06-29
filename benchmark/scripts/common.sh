#!/usr/bin/env bash
# common.sh — Shared utility functions

set -euo pipefail

# Environment variables
export CLUSTER_NAME="${CLUSTER_NAME:-volcano-benchmark}"
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"
export KWOK_NODE_COUNT="${KWOK_NODE_COUNT:-100}"
export CPU_PER_NODE="${CPU_PER_NODE:-32}"
export MEMORY_PER_NODE="${MEMORY_PER_NODE:-256Gi}"
export KWOK_VERSION="${KWOK_VERSION:-v0.7.0}"

# Existing cluster mode: skip Kind create/delete
export USE_EXISTING_CLUSTER="${USE_EXISTING_CLUSTER:-false}"
# Skip Volcano installation (use pre-installed Volcano on the cluster)
export SKIP_INSTALL_VOLCANO="${SKIP_INSTALL_VOLCANO:-false}"
# Skip monitoring stack installation (Prometheus, Grafana, audit-exporter already deployed)
export SKIP_INSTALL_MONITORING="${SKIP_INSTALL_MONITORING:-false}"
# Skip KWOK node creation (use real cluster nodes instead of simulated KWOK nodes)
export SKIP_KWOK="${SKIP_KWOK:-false}"
# Topology-aware scheduling: distribute nodes across rack/spine domains with HyperNode CRDs
export ENABLE_TOPOLOGY="${ENABLE_TOPOLOGY:-false}"
export TOPOLOGY_RACKS="${TOPOLOGY_RACKS:-4}"
export TOPOLOGY_SPINES="${TOPOLOGY_SPINES:-2}"
# Custom labels applied to all KWOK nodes (comma-separated key=value pairs)
export KWOK_NODE_LABELS="${KWOK_NODE_LABELS:-}"
# Keep common system DaemonSets such as kindnet and kube-proxy off KWOK nodes by default.
export EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK="${EXCLUDE_SYSTEM_DAEMONSETS_FROM_KWOK:-true}"
# Keep KWOK benchmark pods running instead of completing immediately.
export DISABLE_KWOK_POD_COMPLETE="${DISABLE_KWOK_POD_COMPLETE:-true}"
# Audit exporter image (configurable for private registries)
export AUDIT_EXPORTER_IMAGE="${AUDIT_EXPORTER_IMAGE:-volcanosh/kube-apiserver-audit-exporter:dev}"
# Volcano client QPS settings used when this benchmark installs Volcano.
export VOLCANO_SCHEDULER_KUBE_API_QPS="${VOLCANO_SCHEDULER_KUBE_API_QPS:-5000}"
export VOLCANO_SCHEDULER_KUBE_API_BURST="${VOLCANO_SCHEDULER_KUBE_API_BURST:-10000}"
export VOLCANO_CONTROLLER_KUBE_API_QPS="${VOLCANO_CONTROLLER_KUBE_API_QPS:-5000}"
export VOLCANO_CONTROLLER_KUBE_API_BURST="${VOLCANO_CONTROLLER_KUBE_API_BURST:-10000}"

# Project paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export BENCHMARK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export VOLCANO_ROOT="$(cd "${BENCHMARK_DIR}/.." && pwd)"

# Logging functions
log_info()  { echo "[INFO]  $(date '+%H:%M:%S') $*"; }
log_warn()  { echo "[WARN]  $(date '+%H:%M:%S') $*" >&2; }
log_error() { echo "[ERROR] $(date '+%H:%M:%S') $*" >&2; }

# Wait for a deployment to be ready
wait_for_deployment() {
    local name=$1 namespace=$2 timeout=${3:-120}
    log_info "Waiting for Deployment ${namespace}/${name} to be ready (timeout ${timeout}s)..."
    kubectl rollout status deployment/"${name}" -n "${namespace}" --timeout="${timeout}s"
}

# Wait for all pods to be ready
wait_for_pods_ready() {
    local namespace=$1 label=$2 timeout=${3:-120}
    log_info "Waiting for Pods (${label}) in ${namespace} to be ready..."
    kubectl wait --for=condition=Ready pod -l "${label}" -n "${namespace}" --timeout="${timeout}s"
}

# Check if required commands exist
require_cmd() {
    for cmd in "$@"; do
        if ! command -v "${cmd}" &>/dev/null; then
            log_error "Required command not found: ${cmd}"
            exit 1
        fi
    done
}
