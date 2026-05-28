#!/usr/bin/env bash
# create-cluster.sh — Create a Kind cluster
#
# Renders kind-config.yaml (replacing __VOLCANO_ROOT__ with the repo's
# absolute path, since kind requires absolute hostPaths) and creates the
# cluster. Pre-creates benchmark/logs/ so the apiserver audit log mount
# succeeds on first boot.

source "$(dirname "$0")/common.sh"
require_cmd kind docker kubectl

log_info "Checking if cluster ${CLUSTER_NAME} already exists..."
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    log_warn "Cluster ${CLUSTER_NAME} already exists, deleting first..."
    kind delete cluster --name "${CLUSTER_NAME}"
fi

AUDIT_LOG_DIR="${BENCHMARK_DIR}/logs"
mkdir -p "${AUDIT_LOG_DIR}"
log_info "Audit log host dir: ${AUDIT_LOG_DIR}"

RENDERED_CONFIG="${BENCHMARK_DIR}/config/.kind-config.rendered.yaml"
sed "s|__VOLCANO_ROOT__|${VOLCANO_ROOT}|g" \
    "${BENCHMARK_DIR}/config/kind-config.yaml" > "${RENDERED_CONFIG}"
log_info "Rendered kind config: ${RENDERED_CONFIG}"

log_info "Creating Kind cluster ${CLUSTER_NAME}..."
kind create cluster \
    --config "${RENDERED_CONFIG}" \
    --name "${CLUSTER_NAME}"

log_info "Exporting kubeconfig..."
kind get kubeconfig --name "${CLUSTER_NAME}" > "${BENCHMARK_DIR}/kubeconfig"
export KUBECONFIG="${BENCHMARK_DIR}/kubeconfig"

log_info "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready node --all --timeout=120s

log_info "Cluster ${CLUSTER_NAME} created successfully"
kubectl cluster-info
