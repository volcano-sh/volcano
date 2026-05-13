#!/usr/bin/env bash
# run-tests.sh — Run benchmark tests
#
# Usage:
#   ./scripts/run-tests.sh <scenario> --config=<profile.yaml>
#
# Note:
#   For a complete list of supported YAML parameters, please refer to:
#   testcases/<scenario>/profiles/comprehensive.yaml
#
# Examples:
#   # Gang scheduling with YAML config
#   ./scripts/run-tests.sh gang --config=profiles/basic-gang.yaml

source "$(dirname "$0")/common.sh"
require_cmd go kubectl curl jq tee

usage() {
    echo "Usage: $0 <scenario> --config=<profile.yaml>"
    echo ""
    echo "Examples:"
    echo "  $0 gang --config=profiles/basic-gang.yaml"
}

# --- Parse arguments ---
if [[ $# -lt 1 ]]; then
    usage
    exit 1
fi

SCENE_DIR="$1"
shift

CONFIG_FILE=""
SCENARIO_DIR_PATH="${BENCHMARK_DIR}/testcases/${SCENE_DIR}"

if [[ ! -d "${SCENARIO_DIR_PATH}" ]]; then
    log_error "Scenario not found: ${SCENE_DIR}"
    exit 1
fi

# Parse scenario-specific options
while [[ $# -gt 0 ]]; do
    case "$1" in
        --config=*)
            CONFIG_FILE="${1#*=}"
            shift
            ;;
        --template=*)
            require_cmd envsubst
            TEMPLATE_FILE="${1#*=}"
            
            # Resolve template path
            if [[ ! -f "${TEMPLATE_FILE}" ]] && [[ -f "${SCENARIO_DIR_PATH}/${TEMPLATE_FILE}" ]]; then
                TEMPLATE_FILE="${SCENARIO_DIR_PATH}/${TEMPLATE_FILE}"
            fi
            
            CONFIG_FILE_TMP="${TEMPLATE_FILE%.yaml}-temp.yaml"
            log_info "Rendering config ${CONFIG_FILE_TMP} from template ${TEMPLATE_FILE} using envsubst"
            
            export JOBS MIN_AVAILABLE REPLICAS PODS SCHEDULER_NAME
            envsubst '$JOBS,$MIN_AVAILABLE,$REPLICAS,$PODS,$SCHEDULER_NAME' < "${TEMPLATE_FILE}" > "${CONFIG_FILE_TMP}"
            # Clean up temp file on exit
            trap "rm -f '${CONFIG_FILE_TMP}'" EXIT
            # Make CONFIG_FILE point to the temporary rendered file
            CONFIG_FILE="${CONFIG_FILE_TMP}"
            shift
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [[ -z "${CONFIG_FILE}" ]]; then
    log_error "Missing required option: --config=<profile.yaml>"
    usage
    exit 1
fi

# Resolve config path (absolute > existing relative > scenario-relative)
if [[ "${CONFIG_FILE}" = /* ]]; then
    RESOLVED_CONFIG="${CONFIG_FILE}"
elif [[ -f "${CONFIG_FILE}" ]]; then
    RESOLVED_CONFIG="$(cd "$(dirname "${CONFIG_FILE}")" && pwd)/$(basename "${CONFIG_FILE}")"
else
    RESOLVED_CONFIG="${SCENARIO_DIR_PATH}/${CONFIG_FILE}"
fi

if [[ ! -f "${RESOLVED_CONFIG}" ]]; then
    log_error "Config file not found: ${RESOLVED_CONFIG}"
    exit 1
fi

log_info "Config mode: scenario=${SCENE_DIR}, config=${RESOLVED_CONFIG}"
export BENCHMARK_CONFIG="${RESOLVED_CONFIG}"

export SCENARIO="${SCENE_DIR}"
export SCENARIO_DIR="${SCENARIO_DIR_PATH}"
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

# --- Run tests ---

mkdir -p "${BENCHMARK_DIR}/results"
RESULT_FILE="${BENCHMARK_DIR}/results/test-${SCENE_DIR}-$(date +%Y%m%d-%H%M%S).log"

log_info "Running tests for scenario '${SCENE_DIR}' with config '${RESOLVED_CONFIG}'..."

PROM_URL="${PROM_URL:-http://localhost:30003}"

# Try to record Prometheus timestamp before test (may fail if Prometheus is not available)
PROM_AVAILABLE=true
TIME_BEFORE=$(curl -s --connect-timeout 3 "${PROM_URL}/api/v1/query" \
    --data-urlencode 'query=time()' 2>/dev/null | jq -r '.data.result[0].value[1] // empty' 2>/dev/null) || true
if [[ -z "${TIME_BEFORE}" ]]; then
    PROM_AVAILABLE=false
    log_warn "Prometheus not reachable at ${PROM_URL}, audit-exporter report will be skipped"
fi

cd "${VOLCANO_ROOT}"
set +e
go test -count=1 -v -timeout 1800s "./benchmark/testcases/${SCENE_DIR}/..." -run TestFromConfig 2>&1 | tee "${RESULT_FILE}"
TEST_EXIT_CODE=${PIPESTATUS[0]}
set -e

if [[ ${TEST_EXIT_CODE} -eq 0 ]]; then
    log_info "Tests completed. Results saved to: ${RESULT_FILE}"
else
    log_warn "Tests failed (exit code: ${TEST_EXIT_CODE}). Results saved to: ${RESULT_FILE}"
fi

# Collect audit-exporter report from Prometheus (only if Prometheus is available)
if [[ "${PROM_AVAILABLE}" == "true" ]]; then
    log_info "Waiting 10s for Prometheus to scrape audit-exporter metrics..."
    sleep 10

    TIME_AFTER=$(curl -s "${PROM_URL}/api/v1/query" \
        --data-urlencode 'query=time()' | jq -r '.data.result[0].value[1] // empty')

    if [[ -n "${TIME_AFTER}" ]]; then
        log_info "Collecting scheduling latency report from audit-exporter..."
        bash "${SCRIPT_DIR}/collect-report.sh" --before "${TIME_BEFORE}" --after "${TIME_AFTER}" \
            || log_warn "Report collection failed (monitoring may not be available)"
    else
        log_warn "Failed to read Prometheus timestamp after test, audit-exporter report will be skipped"
    fi
fi

# audit-exporter may produce empty results if apiserver audit logging is not
# enabled, even when Prometheus itself is reachable.
log_info ""
log_info "If audit-exporter metrics are empty (apiserver audit logging not enabled),"
log_info "run with DRY_RUN=true and then 'make collect-pod-latency' to collect latency from pod timestamps."
log_info "For full precision, enable apiserver audit logging (see README)."

exit "${TEST_EXIT_CODE}"
