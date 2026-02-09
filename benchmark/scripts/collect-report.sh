#!/usr/bin/env bash
# collect-report.sh — Collect test report
#
# Usage:
#   ./scripts/collect-report.sh [--from <epoch_ms>] [--to <epoch_ms>]

source "$(dirname "$0")/common.sh"
require_cmd curl jq

PROM_URL="${PROM_URL:-http://localhost:30090}"
RESULTS_DIR="${BENCHMARK_DIR}/results"
mkdir -p "${RESULTS_DIR}"

# Parse --from/--to for time range
TIME_FROM=""
TIME_TO=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --from) TIME_FROM="$2"; shift 2 ;;
        --to)   TIME_TO="$2";   shift 2 ;;
        *)      shift ;;
    esac
done

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
REPORT_FILE="${RESULTS_DIR}/report-${TIMESTAMP}.json"

log_info "Querying metrics from Prometheus..."

# Query Job E2E scheduling duration
JOB_DURATION=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=volcano_e2e_job_scheduling_duration{job_namespace="default"}' | jq '.')

# Query P50/P99 scheduling latency
P50_LATENCY=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=histogram_quantile(0.5, rate(volcano_e2e_scheduling_latency_milliseconds_bucket[5m]))' | jq '.data.result[0].value[1] // "N/A"')

P99_LATENCY=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=histogram_quantile(0.99, rate(volcano_e2e_scheduling_latency_milliseconds_bucket[5m]))' | jq '.data.result[0].value[1] // "N/A"')

# Query scheduling attempt count
SCHEDULE_ATTEMPTS=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=volcano_scheduler_schedule_attempts_total' | jq '.')

# Query Pod status statistics
POD_CREATED=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=count(kube_pod_info{namespace="default"})' | jq '.data.result[0].value[1] // "0"')

POD_SCHEDULED=$(curl -s "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=count(kube_pod_status_scheduled{namespace="default",condition="true"})' | jq '.data.result[0].value[1] // "0"')

log_info "Generating test report..."
cat > "${REPORT_FILE}" <<EOF
{
  "timestamp": "${TIMESTAMP}",
  "metrics": {
    "scheduling_latency_p50_ms": ${P50_LATENCY},
    "scheduling_latency_p99_ms": ${P99_LATENCY},
    "pods_created": ${POD_CREATED},
    "pods_scheduled": ${POD_SCHEDULED},
    "job_e2e_duration": ${JOB_DURATION},
    "schedule_attempts": ${SCHEDULE_ATTEMPTS}
  },
  "grafana_url": "http://localhost:30080/d/volcano-benchmark",
  "prometheus_url": "${PROM_URL}"
}
EOF

log_info "Report saved to: ${REPORT_FILE}"
log_info "Key metrics:"
log_info "  Scheduling latency P50: ${P50_LATENCY} ms"
log_info "  Scheduling latency P99: ${P99_LATENCY} ms"
log_info "  Pods created: ${POD_CREATED}"
log_info "  Pods scheduled: ${POD_SCHEDULED}"
log_info ""
log_info "Grafana dashboard: http://localhost:30080/d/volcano-benchmark"

# Export Grafana charts as PNG images, passing time range if available
CHART_ARGS=""
if [[ -n "${TIME_FROM}" && -n "${TIME_TO}" ]]; then
    CHART_ARGS="--from ${TIME_FROM} --to ${TIME_TO}"
fi
log_info "Exporting Grafana dashboard charts..."
bash "$(dirname "$0")/export-grafana-charts.sh" ${CHART_ARGS} || log_warn "Chart export failed (Image Renderer may not be available)"
