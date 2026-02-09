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
export SCENARIO="${SCENARIO:-default}"

# Project paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export BENCHMARK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export VOLCANO_ROOT="$(cd "${BENCHMARK_DIR}/.." && pwd)"
export SCENARIO_DIR="${BENCHMARK_DIR}/testcases/${SCENARIO}"

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
