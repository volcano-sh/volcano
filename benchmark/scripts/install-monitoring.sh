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
log_info "Ensuring namespace '${MONITORING_NS}' exists..."
kubectl create namespace "${MONITORING_NS}" --dry-run=client -o yaml | kubectl apply -f -

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

# audit-exporter reads apiserver audit logs from the control-plane node.
# The image is not published to DockerHub, so users must build it first.
if [[ "${USE_EXISTING_CLUSTER}" == "true" ]]; then
    log_warn "=== audit-exporter setup required ==="
    log_warn "The image ${AUDIT_EXPORTER_IMAGE} is not available on DockerHub."
    log_warn "You need to build it and make it accessible to your cluster nodes."
    log_warn ""
    log_warn "Build the image:"
    log_warn "  make build-audit-exporter"
    log_warn ""
    # Check if this is a Kind cluster (even though USE_EXISTING_CLUSTER=true)
    if command -v kind &>/dev/null && kind get clusters 2>/dev/null | grep -q .; then
        log_warn "Detected Kind cluster. Load the image with:"
        DETECTED_CLUSTER=$(kind get clusters 2>/dev/null | head -1)
        log_warn "  kind load docker-image ${AUDIT_EXPORTER_IMAGE} --name ${DETECTED_CLUSTER}"
    else
        log_warn "Push the image to a registry accessible by your cluster:"
        log_warn "  docker tag ${AUDIT_EXPORTER_IMAGE} <your-registry>/audit-exporter:dev"
        log_warn "  docker push <your-registry>/audit-exporter:dev"
        log_warn "  Then re-run with: AUDIT_EXPORTER_IMAGE=<your-registry>/audit-exporter:dev make install-monitoring"
    fi
    log_warn ""
    log_warn "Apiserver audit logging must also be enabled:"
    log_warn "  --audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml"
    log_warn "  --audit-log-path=/var/log/kubernetes/kube-apiserver-audit.log"
    log_warn "The audit policy file is at: third_party/kube-apiserver-audit-exporter/audit-policy.yaml"
    log_warn "See README 'Enabling Apiserver Audit Logging' for details."
    log_warn ""
    log_warn "If you do not need audit-exporter for microsecond-precision latency,"
    log_warn "Prometheus and Grafana will still be deployed for scheduler metrics analysis."
    log_warn "Use 'DRY_RUN=true' + 'make collect-pod-latency' for latency collection instead."
    log_warn "==================================="
fi

log_info "Deploying kube-apiserver-audit-exporter DaemonSet..."
# Replace the default image with AUDIT_EXPORTER_IMAGE if customized
if [[ "${AUDIT_EXPORTER_IMAGE}" != "volcanosh/kube-apiserver-audit-exporter:dev" ]]; then
    log_info "Using custom audit-exporter image: ${AUDIT_EXPORTER_IMAGE}"
    sed "s|image: volcanosh/kube-apiserver-audit-exporter:dev|image: ${AUDIT_EXPORTER_IMAGE}|" \
        "${AUDIT_EXPORTER_MANIFEST}" | kubectl apply -f -
else
    kubectl apply -f "${AUDIT_EXPORTER_MANIFEST}"
fi

log_info "Waiting for Prometheus to be ready..."
wait_for_deployment prometheus-deployment "${MONITORING_NS}" 120

log_info "Waiting for kube-state-metrics to be ready..."
wait_for_deployment kube-state-metrics "${MONITORING_NS}" 120

log_info "Waiting for Grafana to be ready..."
wait_for_deployment grafana "${MONITORING_NS}" 300

log_info "Waiting for kube-apiserver-audit-exporter DaemonSet to be ready..."
if [[ "${USE_EXISTING_CLUSTER}" == "true" ]]; then
    # Don't block on audit-exporter for existing clusters; the image likely isn't loaded yet.
    if kubectl rollout status daemonset/kube-apiserver-audit-exporter \
        -n "${MONITORING_NS}" --timeout=10s 2>/dev/null; then
        log_info "audit-exporter DaemonSet is ready."
    else
        log_warn "audit-exporter DaemonSet is not ready yet (image may not be loaded)."
        log_warn "This is expected if you haven't built and loaded the image."
        log_warn "The DaemonSet has been created and will start automatically once the image is available."
        log_warn "Run 'make build-audit-exporter' and load/push the image, then check:"
        log_warn "  kubectl rollout status daemonset/kube-apiserver-audit-exporter -n ${MONITORING_NS}"
        log_warn "Or skip audit-exporter and use 'DRY_RUN=true' + 'make collect-pod-latency' instead."
    fi
else
    kubectl rollout status daemonset/kube-apiserver-audit-exporter \
        -n "${MONITORING_NS}" --timeout=120s || \
        log_warn "audit-exporter rollout not ready yet (image may still be loading)"
fi

log_info "Monitoring components installed successfully"
if [[ "${USE_EXISTING_CLUSTER}" == "true" ]]; then
    log_info "  Prometheus:  accessible via NodePort 30003 or kubectl port-forward"
    log_info "  Grafana:     accessible via NodePort 30004 or kubectl port-forward (admin/admin)"
    log_info "  Example: kubectl port-forward svc/prometheus-service -n ${MONITORING_NS} 30003:8080 &"
    log_info "  Then set PROM_URL=http://localhost:30003 when running tests"
else
    log_info "  Prometheus:    http://localhost:30003"
    log_info "  Grafana:       http://localhost:30004 (admin/admin)"
fi
log_info "  Audit metrics: scrape via Service kube-apiserver-audit-exporter.${MONITORING_NS}:8080/metrics"
