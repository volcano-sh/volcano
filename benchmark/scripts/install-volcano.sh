#!/usr/bin/env bash
# install-volcano.sh — Install Volcano
#
# Usage:
#   ./scripts/install-volcano.sh                    # Default: install from local source
#   ./scripts/install-volcano.sh --local             # Explicitly install from local source
#   ./scripts/install-volcano.sh --release v1.10.0   # Install a specific release version
#
# The scheduler config and queue are read from SCENARIO_DIR (set via SCENARIO env var).

source "$(dirname "$0")/common.sh"
require_cmd kubectl helm

# --- Parse arguments ---
INSTALL_MODE="local"
VOLCANO_VERSION=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --local)
            INSTALL_MODE="local"
            shift
            ;;
        --release)
            INSTALL_MODE="release"
            VOLCANO_VERSION="${2:?ERROR: --release requires a version argument (e.g. v1.10.0)}"
            shift 2
            ;;
        *)
            log_error "Unknown argument: $1"
            exit 1
            ;;
    esac
done

# --- Clean up any pre-existing Volcano installation ---

log_info "Cleaning up any pre-existing Volcano CRDs..."
VOLCANO_CRDS=$(kubectl get crd -o name 2>/dev/null | grep 'volcano\.sh' || true)
if [[ -n "${VOLCANO_CRDS}" ]]; then
    echo "${VOLCANO_CRDS}" | while read -r crd; do
        log_info "  Deleting $crd"
        kubectl delete "$crd" --ignore-not-found
    done
fi

if helm status volcano -n volcano-system &>/dev/null; then
    log_info "Removing previous Volcano Helm release..."
    helm uninstall volcano -n volcano-system --wait
fi

# --- Install Volcano ---

if [[ "${INSTALL_MODE}" == "release" ]]; then
    log_info "Installing Volcano release ${VOLCANO_VERSION} from official Helm repo..."

    helm repo add volcano-sh https://volcano-sh.github.io/volcano 2>/dev/null || true
    helm repo update volcano-sh

    helm install volcano volcano-sh/volcano \
        --version "${VOLCANO_VERSION}" \
        --namespace volcano-system \
        --create-namespace \
        --set basic.scheduler_config_file=volcano-scheduler-configmap \
        --set basic.image_pull_policy=IfNotPresent \
        --wait --timeout 180s
else
    log_info "Installing Volcano from local source..."

    helm install volcano "${VOLCANO_ROOT}/installer/helm/chart/volcano" \
        --namespace volcano-system \
        --create-namespace \
        --set basic.scheduler_config_file=volcano-scheduler-configmap \
        --set basic.image_pull_policy=IfNotPresent \
        --wait --timeout 120s
fi

# --- Post-install configuration (reads from scenario directory) ---

log_info "Applying scheduler configuration from ${SCENARIO_DIR}/manifests/volcano/scheduler-config.yaml..."
kubectl apply -f "${SCENARIO_DIR}/manifests/volcano/scheduler-config.yaml"

log_info "Restarting volcano-scheduler to load new configuration..."
kubectl rollout restart deployment/volcano-scheduler -n volcano-system
kubectl rollout status deployment/volcano-scheduler -n volcano-system --timeout=120s

log_info "Creating test queue from ${SCENARIO_DIR}/manifests/volcano/queue.yaml..."
kubectl apply -f "${SCENARIO_DIR}/manifests/volcano/queue.yaml"

log_info "Volcano installation complete (mode=${INSTALL_MODE}, scenario=${SCENARIO})"
kubectl get pods -n volcano-system
