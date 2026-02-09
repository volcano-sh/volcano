#!/usr/bin/env bash
# create-cluster.sh — Create a Kind cluster

source "$(dirname "$0")/common.sh"
require_cmd kind docker kubectl

log_info "Checking if cluster ${CLUSTER_NAME} already exists..."
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    log_warn "Cluster ${CLUSTER_NAME} already exists, deleting first..."
    kind delete cluster --name "${CLUSTER_NAME}"
fi

log_info "Creating Kind cluster ${CLUSTER_NAME}..."
kind create cluster \
    --config "${BENCHMARK_DIR}/config/kind-config.yaml" \
    --name "${CLUSTER_NAME}"

log_info "Exporting kubeconfig..."
kind get kubeconfig --name "${CLUSTER_NAME}" > "${BENCHMARK_DIR}/kubeconfig"
export KUBECONFIG="${BENCHMARK_DIR}/kubeconfig"

log_info "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready node --all --timeout=120s

log_info "Cluster ${CLUSTER_NAME} created successfully"
kubectl cluster-info
