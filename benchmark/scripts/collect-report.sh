#!/usr/bin/env bash
# collect-report.sh — Collect benchmark report from audit-exporter metrics
#
# Queries pod scheduling latency (created -> scheduled) from
# kube-apiserver-audit-exporter via Prometheus.
#
# Usage:
#   ./scripts/collect-report.sh --before <epoch_s> --after <epoch_s>
#   ./scripts/collect-report.sh  (fallback: uses increase() over 10m window)

source "$(dirname "$0")/common.sh"
require_cmd curl jq bc

PROM_URL="${PROM_URL:-http://localhost:30003}"
AUDIT_LOG_PATH="${AUDIT_LOG_PATH:-${BENCHMARK_DIR}/logs/kube-apiserver-audit.log}"
RESULTS_DIR="${BENCHMARK_DIR}/results"
mkdir -p "${RESULTS_DIR}"

# Parse arguments
TIME_BEFORE=""
TIME_AFTER=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --before) TIME_BEFORE="$2"; shift 2 ;;
        --after)  TIME_AFTER="$2";  shift 2 ;;
        *)        shift ;;
    esac
done

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
REPORT_FILE="${RESULTS_DIR}/report-${TIMESTAMP}.json"

log_info "Querying audit-exporter metrics from Prometheus..."

json_number_or_null() {
    if [[ "$1" == "N/A" || -z "$1" ]]; then
        echo "null"
    else
        echo "$1"
    fi
}

# Compute histogram percentiles by subtracting bucket values at two time points.
# This avoids increase() extrapolation issues with short time windows.
compute_hist_percentile() {
    local metric="$1"
    local quantile="$2"
    local ns_filter='namespace="default"'

    if [[ -n "${TIME_BEFORE}" && -n "${TIME_AFTER}" ]]; then
        BEFORE_BUCKETS=$(curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "time=${TIME_BEFORE}" \
            --data-urlencode "query=sum by (le) (${metric}{${ns_filter}})" \
            | jq -r '[.data.result[] | {le: .metric.le, val: (.value[1] | tonumber)}]')

        AFTER_BUCKETS=$(curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "time=${TIME_AFTER}" \
            --data-urlencode "query=sum by (le) (${metric}{${ns_filter}})" \
            | jq -r '[.data.result[] | {le: .metric.le, val: (.value[1] | tonumber)}]')

        echo "${BEFORE_BUCKETS}" | jq -r --argjson after "${AFTER_BUCKETS}" --arg q "${quantile}" '
            (reduce .[] as $b ({}; . + {($b.le): $b.val})) as $before |
            [$after[] | {le: .le, val: (.val - ($before[.le] // 0))}] |
            sort_by(if .le == "+Inf" then 1e308 else (.le | tonumber) end) as $sorted |
            ($sorted | last.val) as $total |
            if $total == 0 then "N/A"
            else
                (($q | tonumber) * $total) as $target |
                $sorted | reduce .[] as $bucket (
                    {prev_le: 0, prev_count: 0, result: null};
                    if .result == null then
                        ($bucket.le | if . == "+Inf" then 1e308 else tonumber end) as $upper |
                        if $bucket.val >= $target then
                            if ($bucket.val - .prev_count) == 0 then
                                .result = $upper
                            else
                                .result = (.prev_le + ($upper - .prev_le) * ($target - .prev_count) / ($bucket.val - .prev_count))
                            end
                        else
                            .prev_le = $upper |
                            .prev_count = $bucket.val
                        end
                    else . end
                ) | .result // "N/A"
            end
        '
    else
        curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "query=histogram_quantile(${quantile}, sum by (le) (increase(${metric}{${ns_filter}}[10m])))" \
            | jq -r '.data.result[0].value[1] // "N/A"'
    fi
}

# Compute counter delta between two time points
compute_count_delta() {
    local metric="$1"
    local ns_filter='namespace="default"'

    if [[ -n "${TIME_BEFORE}" && -n "${TIME_AFTER}" ]]; then
        BEFORE_VAL=$(curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "time=${TIME_BEFORE}" \
            --data-urlencode "query=sum(${metric}{${ns_filter}})" \
            | jq -r '.data.result[0].value[1] // "0"')
        AFTER_VAL=$(curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "time=${TIME_AFTER}" \
            --data-urlencode "query=sum(${metric}{${ns_filter}})" \
            | jq -r '.data.result[0].value[1] // "0"')
        echo "${AFTER_VAL} - ${BEFORE_VAL}" | bc | cut -d. -f1
    else
        curl -fsS "${PROM_URL}/api/v1/query" \
            --data-urlencode "query=sum(increase(${metric}{${ns_filter}}[10m]))" \
            | jq -r '.data.result[0].value[1] // "0"' | awk '{printf "%d", $1+0.5}'
    fi
}

# Compute pod scheduling throughput from successful pods/binding audit events.
# This is a final report value, not a Prometheus metric.
compute_pod_scheduling_throughput() {
    if [[ -z "${TIME_BEFORE}" || -z "${TIME_AFTER}" || ! -f "${AUDIT_LOG_PATH}" ]]; then
        jq -n '{count: 0, window_seconds: null, pods_per_second: null}'
        return
    fi

    jq -s \
        --argjson before "${TIME_BEFORE}" \
        --argjson after "${TIME_AFTER}" '
        def ts_epoch:
            capture("(?<Y>[0-9]{4})-(?<m>[0-9]{2})-(?<d>[0-9]{2})T(?<H>[0-9]{2}):(?<M>[0-9]{2}):(?<S>[0-9]{2})(?<frac>\\.[0-9]+)?Z") as $t |
            ([
                ($t.Y | tonumber),
                (($t.m | tonumber) - 1),
                ($t.d | tonumber),
                ($t.H | tonumber),
                ($t.M | tonumber),
                ($t.S | tonumber),
                0,
                0
            ] | mktime) + (($t.frac // "0") | tonumber);

        [
            .[] |
            select(.stage == "ResponseComplete") |
            select(.verb == "create") |
            select(.objectRef.resource == "pods") |
            select(.objectRef.subresource == "binding") |
            select(.objectRef.namespace == "default") |
            select((.responseStatus.code // 0) >= 200 and (.responseStatus.code // 0) < 300) |
            (.stageTimestamp | ts_epoch) as $ts |
            select($ts >= $before and $ts <= $after) |
            $ts
        ] | sort as $events |
        ($events | length) as $count |
        if $count < 2 then
            {count: $count, window_seconds: null, pods_per_second: null}
        else
            (($events[-1] - $events[0]) as $window |
            {
                count: $count,
                window_seconds: (($window * 1000 | round) / 1000),
                pods_per_second: (($count / $window * 100 | round) / 100)
            })
        end
    ' "${AUDIT_LOG_PATH}"
}

# Pod scheduling latency
POD_SCHED_P50=$(compute_hist_percentile "pod_scheduling_latency_seconds_bucket" "0.5")
POD_SCHED_P90=$(compute_hist_percentile "pod_scheduling_latency_seconds_bucket" "0.9")
POD_SCHED_P99=$(compute_hist_percentile "pod_scheduling_latency_seconds_bucket" "0.99")
POD_SCHED_COUNT=$(compute_count_delta "pod_scheduling_latency_seconds_count")
POD_SCHED_THROUGHPUT_STATS=$(compute_pod_scheduling_throughput)
POD_SCHED_THROUGHPUT=$(echo "${POD_SCHED_THROUGHPUT_STATS}" | jq -r '.pods_per_second // "N/A"')
POD_SCHED_THROUGHPUT_WINDOW=$(echo "${POD_SCHED_THROUGHPUT_STATS}" | jq -r '.window_seconds // "N/A"')
POD_SCHED_BINDING_COUNT=$(echo "${POD_SCHED_THROUGHPUT_STATS}" | jq -r '.count')

TIME_WINDOW="N/A"
if [[ -n "${TIME_BEFORE}" && -n "${TIME_AFTER}" ]]; then
    TIME_WINDOW=$(echo "${TIME_AFTER} - ${TIME_BEFORE}" | bc)
fi

log_info "Generating report..."

fmt_ms() {
    echo "$1" | awk '{if ($1 == "N/A") print "N/A"; else printf "%.2f", $1*1000}'
}

cat > "${REPORT_FILE}" <<EOF
{
  "timestamp": "${TIMESTAMP}",
  "time_window": {
    "before": "${TIME_BEFORE:-unknown}",
    "after": "${TIME_AFTER:-unknown}",
    "seconds": $(json_number_or_null "${TIME_WINDOW}")
  },
  "pod_scheduling_latency_seconds": {
    "p50": $(json_number_or_null "${POD_SCHED_P50}"),
    "p90": $(json_number_or_null "${POD_SCHED_P90}"),
    "p99": $(json_number_or_null "${POD_SCHED_P99}"),
    "count": ${POD_SCHED_COUNT}
  },
  "grafana_url": "http://localhost:30004/d/volcano-benchmark",
  "prometheus_url": "${PROM_URL}"
}
EOF

log_info "Report saved to: ${REPORT_FILE}"
log_info ""
log_info "=== Pod Scheduling Latency (created -> scheduled) ==="
log_info "  P50:  $(fmt_ms ${POD_SCHED_P50}) ms"
log_info "  P90:  $(fmt_ms ${POD_SCHED_P90}) ms"
log_info "  P99:  $(fmt_ms ${POD_SCHED_P99}) ms"
log_info "  Total scheduled: ${POD_SCHED_COUNT} pods"
log_info "  Pod scheduling throughput: ${POD_SCHED_THROUGHPUT} pods/sec"
log_info "  Throughput window: ${POD_SCHED_THROUGHPUT_WINDOW} sec (${POD_SCHED_BINDING_COUNT} binding events)"
log_info ""
log_info "Grafana: http://localhost:30004/d/volcano-benchmark"
