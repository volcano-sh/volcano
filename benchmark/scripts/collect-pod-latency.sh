#!/usr/bin/env bash
# collect-pod-latency.sh - Collect scheduling latency from pod timestamps
#
# Computes per-pod scheduling latency from CreationTimestamp to the
# PodScheduled condition's LastTransitionTime. Works on any cluster
# without apiserver audit logging, but only has second-level precision
# (metav1.Time).
#
# Prerequisites:
#   - Benchmark pods must still exist (use DRY_RUN=true when running tests)
#   - kubectl configured to access the cluster
#
# Usage:
#   ./scripts/collect-pod-latency.sh
#   ./scripts/collect-pod-latency.sh --namespace my-ns
#   ./scripts/collect-pod-latency.sh --label-selector volcano.sh/benchmark=true

source "$(dirname "$0")/common.sh"
require_cmd kubectl jq

NAMESPACE="default"
LABEL_SELECTOR="${BenchmarkLabel:-volcano.sh/benchmark}=true"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --namespace|-n)  NAMESPACE="$2"; shift 2 ;;
        --label-selector|-l) LABEL_SELECTOR="$2"; shift 2 ;;
        *) log_error "Unknown option: $1"; exit 1 ;;
    esac
done

RESULTS_DIR="${BENCHMARK_DIR}/results"
mkdir -p "${RESULTS_DIR}"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
REPORT_FILE="${RESULTS_DIR}/pod-latency-${TIMESTAMP}.json"

log_info "Collecting pod scheduling latency from pod timestamps..."
log_info "  Namespace: ${NAMESPACE}"
log_info "  Label selector: ${LABEL_SELECTOR}"
log_info "  Note: precision is ~1 second (metav1.Time)"

# Fetch all benchmark pods with their timestamps
PODS_JSON=$(kubectl get pods -n "${NAMESPACE}" -l "${LABEL_SELECTOR}" \
    -o json 2>/dev/null)

POD_COUNT=$(echo "${PODS_JSON}" | jq '.items | length')
if [[ "${POD_COUNT}" -eq 0 ]]; then
    log_error "No pods found matching label '${LABEL_SELECTOR}' in namespace '${NAMESPACE}'"
    log_error "Make sure you ran the test with DRY_RUN=true so pods are not cleaned up"
    exit 1
fi

# Extract per-pod latency: PodScheduled.LastTransitionTime - CreationTimestamp
# Output: sorted array of latencies in milliseconds
LATENCIES=$(echo "${PODS_JSON}" | jq -r '
    [.items[] |
        (.metadata.creationTimestamp | fromdateiso8601) as $created |
        ([.status.conditions[]? | select(.type == "PodScheduled" and .status == "True") |
            .lastTransitionTime | fromdateiso8601] | first) as $scheduled |
        if $scheduled then
            {
                name: .metadata.name,
                created: $created,
                scheduled: $scheduled,
                latency_ms: (($scheduled - $created) * 1000)
            }
        else empty end
    ] | sort_by(.latency_ms)')

SCHEDULED_COUNT=$(echo "${LATENCIES}" | jq 'length')
if [[ "${SCHEDULED_COUNT}" -eq 0 ]]; then
    log_error "No pods have been scheduled yet (no PodScheduled=True condition found)"
    exit 1
fi

UNSCHEDULED=$((POD_COUNT - SCHEDULED_COUNT))
if [[ "${UNSCHEDULED}" -gt 0 ]]; then
    log_warn "${UNSCHEDULED} pods are not yet scheduled (skipped)"
fi

# Compute statistics
STATS=$(echo "${LATENCIES}" | jq --argjson total "${POD_COUNT}" '
    (map(.latency_ms) | sort) as $sorted |
    ($sorted | length) as $n |

    # percentile helper (linear interpolation)
    def pct(p):
        (p * ($n - 1)) as $idx |
        ($idx | floor) as $lower |
        ($lower + 1) as $upper |
        if $upper >= $n then $sorted[$n - 1]
        elif $lower < 0 then $sorted[0]
        else
            ($idx - $lower) as $frac |
            ($sorted[$lower] * (1 - $frac) + $sorted[$upper] * $frac)
        end;

    ($sorted | add / $n) as $avg |
    (map(.created) | min) as $first_created |
    (map(.scheduled) | max) as $last_scheduled |
    ($last_scheduled - $first_created) as $total_sec |

    {
        timestamp: (now | todate),
        source: "pod-timestamps (second precision)",
        total_pods: $total,
        scheduled_pods: $n,
        scheduling_latency_ms: {
            p50: (pct(0.50) | . * 100 | round / 100),
            p90: (pct(0.90) | . * 100 | round / 100),
            p99: (pct(0.99) | . * 100 | round / 100),
            min: $sorted[0],
            max: $sorted[$n - 1],
            avg: ($avg | . * 100 | round / 100)
        },
        total_scheduling_time_ms: ($total_sec * 1000),
        throughput_pods_per_second: (if $total_sec > 0 then ($n / $total_sec | . * 100 | round / 100) else 0 end)
    }')

# Save report
echo "${STATS}" | jq '.' > "${REPORT_FILE}"

# Print summary
P50=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.p50')
P90=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.p90')
P99=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.p99')
MIN=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.min')
MAX=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.max')
AVG=$(echo "${STATS}" | jq -r '.scheduling_latency_ms.avg')
TOTAL_TIME=$(echo "${STATS}" | jq -r '.total_scheduling_time_ms')
THROUGHPUT=$(echo "${STATS}" | jq -r '.throughput_pods_per_second')

log_info ""
log_info "=== Pod Scheduling Latency (from pod timestamps, ~1s precision) ==="
log_info "  Pods: ${SCHEDULED_COUNT} scheduled / ${POD_COUNT} total"
log_info "  P50:  ${P50} ms"
log_info "  P90:  ${P90} ms"
log_info "  P99:  ${P99} ms"
log_info "  Min:  ${MIN} ms"
log_info "  Max:  ${MAX} ms"
log_info "  Avg:  ${AVG} ms"
log_info "  Total scheduling time: ${TOTAL_TIME} ms"
log_info "  Throughput: ${THROUGHPUT} pods/s"
log_info ""
log_info "Report saved to: ${REPORT_FILE}"
log_info ""
log_info "Note: For microsecond-precision metrics, enable apiserver audit logging"
log_info "and use the audit-exporter + Prometheus pipeline (see README)."
