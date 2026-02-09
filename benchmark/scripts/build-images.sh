#!/usr/bin/env bash
# build-images.sh — Build Volcano images

source "$(dirname "$0")/common.sh"
require_cmd docker kind make

log_info "Building Volcano images..."
cd "${VOLCANO_ROOT}"
make images

log_info "Listing Volcano images..."
IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep -E 'volcanosh/(vc-scheduler|vc-controller-manager|vc-webhook-manager)')

log_info "Loading images into Kind cluster ${CLUSTER_NAME}..."
for img in ${IMAGES}; do
    log_info "  Loading ${img}..."
    kind load docker-image "${img}" --name "${CLUSTER_NAME}"
done

log_info "Volcano images built and loaded successfully"
