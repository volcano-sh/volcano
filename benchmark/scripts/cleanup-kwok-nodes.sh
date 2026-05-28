#!/usr/bin/env bash
# cleanup-kwok-nodes.sh — Clean up KWOK simulated nodes, HyperNode topology, and related resources
# 
# Usage:
#   ./scripts/cleanup-kwok-nodes.sh        # Delete KWOK nodes + HyperNodes
#   ./scripts/cleanup-kwok-nodes.sh --all  # Delete KWOK nodes + HyperNodes + uninstall KWOK controller
#
# Environment variables:
#   KWOK_VERSION - Used when '--all' is provided to cleanly uninstall KWOK controller manifests. 
#                  If not set, it falls back to deleting resources by name.

source "$(dirname "$0")/common.sh"
require_cmd kubectl

DELETE_ALL=false
for arg in "$@"; do
    case "${arg}" in
        --all) DELETE_ALL=true ;;
    esac
done

# Function to delete KWOK nodes
function delete_kwok_nodes() {
    log_info "Cleaning up KWOK simulated nodes..."
    kubectl delete node -l type=kwok --ignore-not-found=true 2>/dev/null || true
}

# Function to delete HyperNode topology CRDs
function delete_hypernodes() {
    # Check if HyperNode CRD exists
    if ! kubectl get crd hypernodes.topology.volcano.sh >/dev/null 2>&1; then
        return
    fi

    local count
    count=$(kubectl get hypernodes.topology.volcano.sh --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [[ "${count}" -eq 0 ]]; then
        return
    fi

    log_info "Cleaning up HyperNode topology resources (${count} found)..."
    # Delete rack-level HyperNodes (tier 1)
    kubectl delete hypernodes.topology.volcano.sh -l "volcano.sh/tier=rack" --ignore-not-found=true 2>/dev/null || true
    # Delete spine-level HyperNodes (tier 2)
    kubectl delete hypernodes.topology.volcano.sh -l "volcano.sh/tier=spine" --ignore-not-found=true 2>/dev/null || true
    # Fallback: delete by name pattern (for HyperNodes without labels)
    kubectl get hypernodes.topology.volcano.sh --no-headers 2>/dev/null | awk '{print $1}' | \
        grep -E '^(rack-|spine-)' | xargs -r kubectl delete hypernodes.topology.volcano.sh --ignore-not-found=true 2>/dev/null || true
}

# Function to delete KWOK controller and CRDs
function delete_kwok_controller() {
    log_info "Uninstalling KWOK CRD and controller (${KWOK_VERSION:-unknown version})..."
    if [[ -n "${KWOK_VERSION}" ]]; then
        kubectl delete -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/stage-fast.yaml" --ignore-not-found=true 2>/dev/null || true
        kubectl delete -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}/kwok.yaml" --ignore-not-found=true 2>/dev/null || true
    else
        log_info "KWOK_VERSION not set, deleting resources by name..."
        kubectl delete stages.kwok.x-k8s.io --all --ignore-not-found=true 2>/dev/null || true
        kubectl delete deployment kwok-controller -n kube-system --ignore-not-found=true 2>/dev/null || true
        kubectl delete clusterrolebinding kwok-controller --ignore-not-found=true 2>/dev/null || true
        kubectl delete clusterrole kwok-controller --ignore-not-found=true 2>/dev/null || true
        kubectl delete serviceaccount kwok-controller -n kube-system --ignore-not-found=true 2>/dev/null || true
    fi
}

delete_kwok_nodes
delete_hypernodes

if [[ "${DELETE_ALL}" == "true" ]]; then
    delete_kwok_controller
    log_info "KWOK nodes, HyperNodes, and controller cleanup complete"
else
    log_info "KWOK nodes and HyperNodes cleanup complete (controller retained. Use --all to remove)"
fi
