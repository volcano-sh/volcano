#!/usr/bin/env bash
# run-tests.sh — Run benchmark tests
#
# Usage:
#   ./scripts/run-tests.sh <scenario> [options]
#   ./scripts/run-tests.sh <scenario>/<case>
#
# Examples:
#   # Gang scheduling with CLI parameters
#   ./scripts/run-tests.sh gang --jobs=20 --pods=50 --cpu=1 --memory=1Gi
#
#   # Run predefined test case
#   ./scripts/run-tests.sh gang/case_20x50
#
#   # Run all tests in a scenario
#   ./scripts/run-tests.sh gang
#
# Gang scenario options:
#   --jobs=N          Number of VCJobs to create
#   --pods=N          Number of pods per job
#   --cpu=N           CPU request per pod (default: 1)
#   --memory=SIZE     Memory request per pod (default: 1Gi)
#   --min-available=N Gang scheduling minAvailable (default: same as --pods)
#   --queue=NAME      Volcano queue name (default: benchmark-queue)

source "$(dirname "$0")/common.sh"
require_cmd go

# --- Parse arguments ---
if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <scenario> [options]"
    echo "       $0 <scenario>/<case>"
    echo ""
    echo "Examples:"
    echo "  $0 gang --jobs=20 --pods=50"
    echo "  $0 gang/case_20x50"
    exit 1
fi

FIRST_ARG="$1"
shift

# Determine if it's a predefined case (contains /) or scenario with params
if [[ "${FIRST_ARG}" == *"/"* ]]; then
    # Predefined test case mode: gang/case_20x50
    SCENE_DIR="${FIRST_ARG%%/*}"
    CASE_NAME="${FIRST_ARG##*/}"
    # case_20x50 -> TestGang20x50
    TEST_FUNC="TestGang${CASE_NAME#case_}"
    TEST_FUNC=$(echo "${TEST_FUNC}" | sed 's/_//g' | sed 's/x/x/g')
    CLI_MODE=false
    log_info "Predefined case mode: scenario=${SCENE_DIR}, case=${CASE_NAME}, func=${TEST_FUNC}"
else
    # Scenario with optional CLI parameters
    SCENE_DIR="${FIRST_ARG}"
    CLI_MODE=false
    TEST_FUNC=""
    
    # Default values
    JOBS=""
    PODS=""
    CPU="1"
    MEMORY="1Gi"
    MIN_AVAILABLE=""
    QUEUE="benchmark-queue"
    
    # Parse scenario-specific options
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --jobs=*)
                JOBS="${1#*=}"
                CLI_MODE=true
                shift
                ;;
            --pods=*)
                PODS="${1#*=}"
                shift
                ;;
            --cpu=*)
                CPU="${1#*=}"
                shift
                ;;
            --memory=*)
                MEMORY="${1#*=}"
                shift
                ;;
            --min-available=*)
                MIN_AVAILABLE="${1#*=}"
                shift
                ;;
            --queue=*)
                QUEUE="${1#*=}"
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
    
    if [[ "${CLI_MODE}" == "true" ]]; then
        # Validate required params for CLI mode
        if [[ -z "${JOBS}" || -z "${PODS}" ]]; then
            log_error "CLI mode requires --jobs and --pods"
            exit 1
        fi
        MIN_AVAILABLE="${MIN_AVAILABLE:-${PODS}}"
        TEST_FUNC="TestFromCLI"
        log_info "CLI params mode: scenario=${SCENE_DIR}, jobs=${JOBS}, pods=${PODS}, cpu=${CPU}, memory=${MEMORY}, minAvailable=${MIN_AVAILABLE}, queue=${QUEUE}"
    else
        log_info "Running all tests in scenario: ${SCENE_DIR}"
    fi
fi

# Update SCENARIO_DIR based on parsed scenario
export SCENARIO="${SCENE_DIR}"
export SCENARIO_DIR="${BENCHMARK_DIR}/testcases/${SCENE_DIR}"

# --- Compile test binary ---
log_info "Compiling test binary: testcases/${SCENE_DIR}..."
mkdir -p "${BENCHMARK_DIR}/bin"
mkdir -p "${BENCHMARK_DIR}/results"
cd "${VOLCANO_ROOT}"
go test -c -o "${BENCHMARK_DIR}/bin/test-${SCENE_DIR}" "./benchmark/testcases/${SCENE_DIR}/..."

# --- Run tests ---
log_info "Running tests..."
RUN_ARGS="-test.v -test.timeout 600s"
if [[ -n "${TEST_FUNC}" ]]; then
    RUN_ARGS="-test.run ${TEST_FUNC} ${RUN_ARGS}"
    log_info "  Test function: ${TEST_FUNC}"
fi

# Export env vars for Go test binary to read
export KUBECONFIG="${KUBECONFIG}"
export BENCHMARK_SCENARIO="${SCENARIO}"
export BENCHMARK_SCENARIO_DIR="${SCENARIO_DIR}"

if [[ "${CLI_MODE}" == "true" ]]; then
    export BENCHMARK_JOBS="${JOBS}"
    export BENCHMARK_PODS="${PODS}"
    export BENCHMARK_CPU="${CPU}"
    export BENCHMARK_MEMORY="${MEMORY}"
    export BENCHMARK_MIN_AVAILABLE="${MIN_AVAILABLE}"
    export BENCHMARK_QUEUE="${QUEUE}"
fi

RESULT_FILE="${BENCHMARK_DIR}/results/test-${SCENE_DIR}-$(date +%Y%m%d-%H%M%S).log"

# Record test start time (epoch milliseconds)
START_TIME_MS=$(date +%s%3N)

"${BENCHMARK_DIR}/bin/test-${SCENE_DIR}" ${RUN_ARGS} 2>&1 | tee "${RESULT_FILE}"

# Record test end time (epoch milliseconds)
END_TIME_MS=$(date +%s%3N)

log_info "Tests completed. Results saved to: ${RESULT_FILE}"

# Wait for Prometheus to scrape final metrics (scrape interval = 1s)
log_info "Waiting 5s for Prometheus to collect final metrics..."
sleep 5

# Auto-collect report with the exact test time window
log_info "Collecting report and exporting Grafana charts..."
bash "${SCRIPT_DIR}/collect-report.sh" --from "${START_TIME_MS}" --to "${END_TIME_MS}" \
    || log_warn "Report collection failed (monitoring may not be available)"
