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

log_info "Cleaning up test VCJobs..."
kubectl delete jobs.batch.volcano.sh --all -n default --ignore-not-found=true --grace-period=0 --force 2>/dev/null || true

log_info "Cleaning up pods created by VCJobs..."
kubectl delete pods -l volcano.sh/job-name -n default --ignore-not-found=true --grace-period=0 --force 2>/dev/null || true

log_info "Cleaning up PodGroups..."
kubectl delete podgroups.scheduling.volcano.sh --all -n default --ignore-not-found=true --grace-period=0 --force 2>/dev/null || true

log_info "Cleaning up KWOK Stages..."
kubectl delete stages.kwok.x-k8s.io --all --ignore-not-found=true 2>/dev/null || true

log_info "Cleaning up KWOK simulated nodes..."
kubectl delete node -l type=kwok --ignore-not-found=true 2>/dev/null || true

log_info "Cleaning up monitoring components..."
kubectl delete -f "${BENCHMARK_DIR}/manifests/monitoring/grafana.yaml" --ignore-not-found=true 2>/dev/null || true
kubectl delete configmap grafana-dashboards -n monitoring --ignore-not-found=true 2>/dev/null || true
kubectl delete -f "${BENCHMARK_DIR}/manifests/monitoring/prometheus.yaml" --ignore-not-found=true 2>/dev/null || true

log_info "Cleaning up Volcano scheduler config..."
kubectl delete configmap volcano-scheduler-configmap -n volcano-system --ignore-not-found=true 2>/dev/null || true

log_info "Cleaning up test queues..."
kubectl delete queues.scheduling.volcano.sh --all --ignore-not-found=true 2>/dev/null || true

log_info "Cleaning up Volcano..."
helm uninstall volcano -n volcano-system 2>/dev/null || true

log_info "Cleaning up residual Helm hook Jobs in volcano-system..."
kubectl delete job -n volcano-system --all --ignore-not-found=true 2>/dev/null || true

kubectl delete namespace volcano-system --ignore-not-found=true 2>/dev/null || true

# Clean up Volcano CRDs (left behind after helm uninstall)
log_info "Cleaning up Volcano CRDs..."
VOLCANO_CRDS=$(kubectl get crd -o name 2>/dev/null | grep 'volcano\.sh' || true)
if [[ -n "${VOLCANO_CRDS}" ]]; then
    echo "${VOLCANO_CRDS}" | while read -r crd; do
        kubectl delete "$crd" --ignore-not-found=true 2>/dev/null || true
    done
fi

# Clean up KWOK controller if deployed
kubectl delete deployment kwok-controller -n kube-system --ignore-not-found=true 2>/dev/null || true
kubectl delete clusterrolebinding kwok-controller --ignore-not-found=true 2>/dev/null || true
kubectl delete clusterrole kwok-controller --ignore-not-found=true 2>/dev/null || true
kubectl delete serviceaccount kwok-controller -n kube-system --ignore-not-found=true 2>/dev/null || true

# --- Clean up local files ---

log_info "Cleaning up local build artifacts..."
rm -rf "${BENCHMARK_DIR}/bin"
rm -rf "${BENCHMARK_DIR}/results"

if [[ "${DELETE_CLUSTER}" == "true" ]]; then
    require_cmd kind
    log_info "Deleting Kind cluster ${CLUSTER_NAME}..."
    kind delete cluster --name "${CLUSTER_NAME}"
    rm -f "${BENCHMARK_DIR}/kubeconfig"
    log_info "Cluster deleted"
else
    log_info "Cluster ${CLUSTER_NAME} retained (use --delete-cluster to remove)"
fi

log_info "Cleanup complete"
