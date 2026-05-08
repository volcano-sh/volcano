#!/usr/bin/env bash
# cleanup.sh — Clean up resources
# Usage:
#   ./scripts/cleanup.sh                   # Clean up resources only, keep the cluster
#   ./scripts/cleanup.sh --delete-cluster   # Clean up resources and delete the cluster
#
# Cleanup does NOT require SCENARIO — it removes all benchmark resources regardless of scenario.

source "$(dirname "$0")/common.sh"
require_cmd kubectl

DELETE_CLUSTER=false
for arg in "$@"; do
    case "${arg}" in
        --delete-cluster) DELETE_CLUSTER=true ;;
    esac
done

# --- Clean up K8s resources ---

# Test resources (VCJobs, bare pods, PodGroups)
log_info "Cleaning up test resources..."
kubectl delete jobs.batch.volcano.sh --all -n default --ignore-not-found --grace-period=0 --force 2>/dev/null || true
kubectl delete pods -l volcano.sh/benchmark=true -A --ignore-not-found --grace-period=0 --force 2>/dev/null || true
kubectl delete podgroups.scheduling.volcano.sh --all -n default --ignore-not-found --grace-period=0 --force 2>/dev/null || true

# Monitoring stack (audit-exporter + Prometheus + Grafana)
log_info "Cleaning up monitoring..."
kubectl delete -f "${BENCHMARK_DIR}/manifests/audit-exporter/daemonset.yaml" --ignore-not-found 2>/dev/null || true
kubectl delete configmap grafana-benchmark-dashboard -n volcano-monitoring --ignore-not-found 2>/dev/null || true
kubectl delete -f "${VOLCANO_ROOT}/installer/volcano-monitoring.yaml" --ignore-not-found 2>/dev/null || true

# KWOK nodes
log_info "Cleaning up KWOK nodes..."
"${BENCHMARK_DIR}/scripts/cleanup-kwok-nodes.sh" --all

# Volcano
log_info "Cleaning up Volcano..."
helm uninstall volcano -n volcano-system 2>/dev/null || true
kubectl delete job -n volcano-system --all --ignore-not-found 2>/dev/null || true
kubectl delete namespace volcano-system --ignore-not-found 2>/dev/null || true
kubectl get crd -o name 2>/dev/null | grep 'volcano\.sh' | xargs -r kubectl delete --ignore-not-found 2>/dev/null || true

# Local artifacts
log_info "Cleaning up local artifacts..."
rm -rf "${BENCHMARK_DIR}"/{bin,results,logs}
rm -f "${BENCHMARK_DIR}/config/.kind-config.rendered.yaml"

if [[ "${DELETE_CLUSTER}" == "true" ]]; then
    require_cmd kind
    log_info "Deleting Kind cluster ${CLUSTER_NAME}..."
    kind delete cluster --name "${CLUSTER_NAME}"
    rm -f "${BENCHMARK_DIR}/kubeconfig"
fi

log_info "Cleanup complete"
