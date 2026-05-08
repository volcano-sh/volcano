#!/usr/bin/env bash
# build-images.sh — Build and load benchmark images into Kind
#
# By default only builds the audit-exporter (not available on DockerHub).
# Set BUILD_VOLCANO=true to also build Volcano from source.
#
# Usage:
#   ./scripts/build-images.sh                  # audit-exporter only
#   BUILD_VOLCANO=true ./scripts/build-images.sh  # audit-exporter + Volcano

source "$(dirname "$0")/common.sh"
require_cmd docker kind

AUDIT_EXPORTER_IMAGE="${AUDIT_EXPORTER_IMAGE:-volcanosh/kube-apiserver-audit-exporter:dev}"
BUILD_VOLCANO="${BUILD_VOLCANO:-false}"

# --- Volcano images (optional) ---
if [[ "${BUILD_VOLCANO}" == "true" ]]; then
    require_cmd make
    log_info "Building Volcano images from source..."
    cd "${VOLCANO_ROOT}"
    make images

    VOLCANO_IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' \
        | grep -E 'volcanosh/(vc-scheduler|vc-agent-scheduler|vc-controller-manager|vc-webhook-manager)')
    for img in ${VOLCANO_IMAGES}; do
        log_info "  Loading ${img}..."
        kind load docker-image "${img}" --name "${CLUSTER_NAME}"
    done
    log_info "Volcano images built and loaded"
else
    log_info "Skipping Volcano image build (using helm release). Set BUILD_VOLCANO=true to build from source."
fi

# --- Audit exporter (always) ---
log_info "Building kube-apiserver-audit-exporter image (${AUDIT_EXPORTER_IMAGE})..."
docker build \
    -f "${BENCHMARK_DIR}/manifests/audit-exporter/Dockerfile" \
    -t "${AUDIT_EXPORTER_IMAGE}" \
    "${VOLCANO_ROOT}"

log_info "Loading audit-exporter image into Kind cluster ${CLUSTER_NAME}..."
kind load docker-image "${AUDIT_EXPORTER_IMAGE}" --name "${CLUSTER_NAME}"

log_info "Done"
