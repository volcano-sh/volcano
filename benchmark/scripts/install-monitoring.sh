#!/usr/bin/env bash
# install-monitoring.sh — Install monitoring components

source "$(dirname "$0")/common.sh"
require_cmd kubectl

log_info "Deploying Prometheus + kube-state-metrics..."
kubectl apply -f "${BENCHMARK_DIR}/manifests/monitoring/prometheus.yaml"

log_info "Creating Grafana dashboard ConfigMap from grafana-dashboard.json..."
kubectl create configmap grafana-dashboards \
    --from-file=volcano-benchmark.json="${BENCHMARK_DIR}/manifests/monitoring/grafana-dashboard.json" \
    -n monitoring --dry-run=client -o yaml | kubectl apply -f -

log_info "Deploying Grafana..."
kubectl apply -f "${BENCHMARK_DIR}/manifests/monitoring/grafana.yaml"

log_info "Waiting for Prometheus to be ready..."
wait_for_deployment prometheus monitoring 120

log_info "Waiting for kube-state-metrics to be ready..."
wait_for_deployment kube-state-metrics monitoring 120

log_info "Waiting for Grafana to be ready..."
wait_for_deployment grafana monitoring 300

log_info "Monitoring components installed successfully"
log_info "  Prometheus: http://localhost:30090"
log_info "  Grafana:    http://localhost:30080 (admin/admin)"
