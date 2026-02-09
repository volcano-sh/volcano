#!/usr/bin/env bash
# export-grafana-charts.sh — Export Grafana dashboard panels as PNG images
# Uses the Grafana Render API (/render/d-solo/) with the Image Renderer sidecar.
#
# Usage:
#   ./scripts/export-grafana-charts.sh [--from <epoch_ms>] [--to <epoch_ms>]
#
# If --from/--to are not specified, defaults to the last 10 minutes.

source "$(dirname "$0")/common.sh"
require_cmd curl

GRAFANA_URL="${GRAFANA_URL:-http://localhost:30080}"
RESULTS_DIR="${BENCHMARK_DIR}/results"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
CHARTS_DIR="${RESULTS_DIR}/charts-${TIMESTAMP}"
mkdir -p "${CHARTS_DIR}"

DASHBOARD_UID="volcano-benchmark"

# Default time range: last 10 minutes
DEFAULT_FROM=$(( $(date +%s) * 1000 - 600000 ))
DEFAULT_TO=$(( $(date +%s) * 1000 ))
TIME_FROM="${DEFAULT_FROM}"
TIME_TO="${DEFAULT_TO}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --from) TIME_FROM="$2"; shift 2 ;;
        --to)   TIME_TO="$2";   shift 2 ;;
        *)      shift ;;
    esac
done

# Panel definitions: id|filename|width|height
PANELS=(
    "1|pod-scheduling-progress|1200|500"
    "2|scheduling-latency|800|400"
    "3|job-e2e-duration|800|400"
)

render_dashboard() {
    local output_file="${CHARTS_DIR}/dashboard-full.png"
    local width=1600
    local height=1200
    local max_retries=3
    local retry=0

    local render_url="${GRAFANA_URL}/render/d/${DASHBOARD_UID}/volcano-benchmark"
    render_url="${render_url}?orgId=1&from=${TIME_FROM}&to=${TIME_TO}"
    render_url="${render_url}&width=${width}&height=${height}&tz=UTC&timeout=60"

    while [[ $retry -lt $max_retries ]]; do
        log_info "Rendering full dashboard attempt $((retry + 1))..."
        local http_code
        http_code=$(curl -s -o "${output_file}" -w "%{http_code}" \
            --max-time 90 \
            "${render_url}")

        if [[ "${http_code}" == "200" ]] && [[ -s "${output_file}" ]]; then
            local file_type
            file_type=$(file -b "${output_file}" 2>/dev/null || echo "unknown")
            if echo "${file_type}" | grep -qi "png\|image"; then
                log_info "  Saved: ${output_file} ($(du -h "${output_file}" | cut -f1))"
                return 0
            else
                log_warn "  Response is not a PNG image (${file_type}), retrying..."
            fi
        else
            log_warn "  HTTP ${http_code}, retrying..."
        fi

        retry=$((retry + 1))
        sleep 3
    done

    log_error "Failed to render full dashboard after ${max_retries} attempts"
    rm -f "${output_file}"
    return 1
}

wait_for_grafana() {
    local max_retries=30
    local i=0
    log_info "Waiting for Grafana to be ready..."
    while [[ $i -lt $max_retries ]]; do
        if curl -sf "${GRAFANA_URL}/api/health" > /dev/null 2>&1; then
            log_info "Grafana is ready"
            return 0
        fi
        sleep 2
        i=$((i + 1))
    done
    log_error "Grafana not ready after $((max_retries * 2))s"
    return 1
}

render_panel() {
    local panel_id="$1"
    local filename="$2"
    local width="$3"
    local height="$4"
    local output_file="${CHARTS_DIR}/${filename}.png"
    local max_retries=3
    local retry=0

    local render_url="${GRAFANA_URL}/render/d-solo/${DASHBOARD_UID}/volcano-benchmark"
    render_url="${render_url}?orgId=1&panelId=${panel_id}"
    render_url="${render_url}&from=${TIME_FROM}&to=${TIME_TO}"
    render_url="${render_url}&width=${width}&height=${height}"
    render_url="${render_url}&tz=UTC&timeout=30"

    while [[ $retry -lt $max_retries ]]; do
        log_info "Rendering panel ${panel_id} (${filename}) attempt $((retry + 1))..."
        local http_code
        http_code=$(curl -s -o "${output_file}" -w "%{http_code}" \
            --max-time 60 \
            "${render_url}")

        if [[ "${http_code}" == "200" ]] && [[ -s "${output_file}" ]]; then
            local file_type
            file_type=$(file -b "${output_file}" 2>/dev/null || echo "unknown")
            if echo "${file_type}" | grep -qi "png\|image"; then
                log_info "  Saved: ${output_file} ($(du -h "${output_file}" | cut -f1))"
                return 0
            else
                log_warn "  Response is not a PNG image (${file_type}), retrying..."
            fi
        else
            log_warn "  HTTP ${http_code}, retrying..."
        fi

        retry=$((retry + 1))
        sleep 5
    done

    log_error "Failed to render panel ${panel_id} after ${max_retries} attempts"
    rm -f "${output_file}"
    return 1
}

# Main
wait_for_grafana || exit 1

log_info "Exporting Grafana charts to ${CHARTS_DIR}/"
log_info "Time range: ${TIME_FROM} -> ${TIME_TO}"

SUCCESS=0
FAILED=0

# Render full dashboard screenshot
if render_dashboard; then
    SUCCESS=$((SUCCESS + 1))
else
    FAILED=$((FAILED + 1))
fi

# Render individual panels
for panel_def in "${PANELS[@]}"; do
    IFS='|' read -r panel_id filename width height <<< "${panel_def}"
    if render_panel "${panel_id}" "${filename}" "${width}" "${height}"; then
        SUCCESS=$((SUCCESS + 1))
    else
        FAILED=$((FAILED + 1))
    fi
done

log_info "Chart export complete: ${SUCCESS} succeeded, ${FAILED} failed"
log_info "Charts saved to: ${CHARTS_DIR}/"

if [[ ${FAILED} -gt 0 ]]; then
    log_warn "Some panels failed to render. Ensure the Grafana Image Renderer sidecar is running."
    log_warn "Check: kubectl logs -n monitoring deployment/grafana -c renderer"
fi
