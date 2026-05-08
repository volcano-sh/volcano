#!/usr/bin/env bash
# install-monitoring.sh — Install monitoring components
#
# Deploys:
#   1. Prometheus + kube-state-metrics + Grafana from
#      installer/volcano-monitoring.yaml.
#   2. The benchmark Grafana dashboard.
#   3. The kube-apiserver-audit-exporter DaemonSet. Prometheus discovers it
#      via the kubernetes-service-endpoints scrape job already present in
#      installer/volcano-monitoring.yaml, so no Prometheus config change is
#      needed here.

source "$(dirname "$0")/common.sh"
require_cmd kubectl

MONITORING_NS="volcano-monitoring"
MONITORING_YAML="${VOLCANO_ROOT}/installer/volcano-monitoring.yaml"
AUDIT_EXPORTER_MANIFEST="${BENCHMARK_DIR}/manifests/audit-exporter/daemonset.yaml"

if [[ ! -f "${MONITORING_YAML}" ]]; then
    log_error "volcano-monitoring.yaml not found at ${MONITORING_YAML}"
    log_error "Run 'make generate-yaml' in the project root first, or ensure installer/volcano-monitoring.yaml exists."
    exit 1
fi

# Ensure the monitoring namespace exists before applying resources
if ! kubectl get namespace "${MONITORING_NS}" &>/dev/null; then
    log_info "Namespace '${MONITORING_NS}' not found, creating..."
    kubectl create namespace "${MONITORING_NS}"
fi

log_info "Deploying monitoring stack from installer/volcano-monitoring.yaml..."
kubectl apply -f "${MONITORING_YAML}"

log_info "Loading benchmark Grafana dashboard..."
kubectl create configmap grafana-benchmark-dashboard \
    --from-file=volcano-benchmark.json="${BENCHMARK_DIR}/manifests/monitoring/grafana-dashboard.json" \
    -n "${MONITORING_NS}" --dry-run=client -o yaml | kubectl apply -f -

# Patch Grafana deployment to mount the benchmark dashboard alongside existing dashboards
PATCH=$(cat <<'EOF'
{"spec":{"template":{"spec":{
  "volumes":[{"name":"grafana-benchmark-dashboard","configMap":{"name":"grafana-benchmark-dashboard"}}],
  "containers":[{"name":"grafana","volumeMounts":[{"name":"grafana-benchmark-dashboard","mountPath":"/var/lib/grafana/dashboards/benchmark"}]}]
}}}}
EOF
)
log_info "Patching Grafana to mount benchmark dashboard..."
kubectl patch deployment grafana -n "${MONITORING_NS}" --type=strategic -p "${PATCH}" 2>/dev/null || true

log_info "Deploying kube-apiserver-audit-exporter DaemonSet..."
kubectl apply -f "${AUDIT_EXPORTER_MANIFEST}"

log_info "Waiting for Prometheus to be ready..."
wait_for_deployment prometheus-deployment "${MONITORING_NS}" 120

log_info "Waiting for kube-state-metrics to be ready..."
wait_for_deployment kube-state-metrics "${MONITORING_NS}" 120

log_info "Waiting for Grafana to be ready..."
wait_for_deployment grafana "${MONITORING_NS}" 300

log_info "Waiting for kube-apiserver-audit-exporter DaemonSet to be ready..."
kubectl rollout status daemonset/kube-apiserver-audit-exporter \
    -n "${MONITORING_NS}" --timeout=120s || \
    log_warn "audit-exporter rollout not ready yet (image may still be loading)"

log_info "Monitoring components installed successfully"
log_info "  Prometheus:    http://localhost:30003"
log_info "  Grafana:       http://localhost:30004 (admin/admin)"
log_info "  Audit metrics: scrape via Service kube-apiserver-audit-exporter.${MONITORING_NS}:8080/metrics"
