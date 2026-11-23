#!/bin/bash

# Copyright 2026 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

platforms=$1
shift
authfile="${REGISTRY_AUTH_FILE:-${DOCKER_CONFIG:-${HOME}/.docker}/config.json}"

for component in "$@"; do
    source="oci-archive:${IMAGES_DIR}/vc-${component}-${TAG}.tar"
    destination="docker://${IMAGE_PREFIX}/vc-${component}:${TAG}"
    source_manifest=$(skopeo inspect --raw "${source}")

    for platform in ${platforms//,/ }; do
        os=${platform%%/*}
        arch=${platform#*/}
        arch=${arch%%/*}
        if ! jq -e --arg os "${os}" --arg arch "${arch}" \
            'any(.manifests[]; .platform.os == $os and .platform.architecture == $arch)' \
            <<< "${source_manifest}" >/dev/null; then
            echo "Missing platform ${platform} in ${source}" >&2
            exit 1
        fi
    done

    # Copy the entire index, including attestations, without changing its digests.
    skopeo copy --all --preserve-digests --authfile "${authfile}" "${source}" "${destination}"
    destination_manifest=$(skopeo inspect --raw --authfile "${authfile}" "${destination}")
    if [[ "${source_manifest}" != "${destination_manifest}" ]]; then
        echo "Published manifest does not match ${source}: ${destination}" >&2
        exit 1
    fi
done
