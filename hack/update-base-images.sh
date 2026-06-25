#!/usr/bin/env bash

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

# This script finds Dockerfiles in the repository and updates specific base images 
# (e.g., alpine, openeuler) to their latest semantic version tag and corresponding digest.
# It resolves 'latest' to a semver tag to avoid using mutable 'latest' references.
# It requires `docker`, `curl`, and `jq` to be installed.

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${SCRIPT_ROOT}"

echo "Scanning for Dockerfiles to update pinned base images..."

# Helper function to get the semver tag that 'latest' currently points to
get_latest_semver_tag() {
    local api_repo=$1
    local tag_regex=$2
    local docker_repo=$3
    
    # Get index digest of latest tag
    local digest=$(docker buildx imagetools inspect "${docker_repo}:latest" | grep '^Digest:' | awk '{print $2}' || true)
    
    if [[ -z "$digest" ]]; then
        echo "Error: Could not fetch digest for ${docker_repo}:latest" >&2
        return 1
    fi
    
    # Query docker hub API to find tags pointing to this digest
    local tags=$(curl -s "https://registry.hub.docker.com/v2/repositories/${api_repo}/tags/?page_size=100" | jq -r ".results[] | select(.digest == \"$digest\") | .name")
    
    # Filter by regex and get the highest version
    local semver_tag=$(echo "$tags" | grep -E "$tag_regex" | sort -V | tail -n1)
    
    if [[ -z "$semver_tag" ]]; then
        echo "Error: Could not resolve a semver tag for ${docker_repo}:latest matching ${tag_regex}" >&2
        return 1
    fi
    
    echo "$semver_tag"
}

# 1. Update Alpine
echo "Resolving latest alpine tag..."
alpine_tag=$(get_latest_semver_tag "library/alpine" '^[0-9]+\.[0-9]+\.[0-9]+$' "alpine")
alpine_digest=$(docker buildx imagetools inspect "alpine:${alpine_tag}" | grep '^Digest:' | awk '{print $2}')
echo "Latest alpine is alpine:${alpine_tag}@${alpine_digest}"

# 2. Update OpenEuler
echo "Resolving latest openeuler tag..."
openeuler_tag=$(get_latest_semver_tag "openeuler/openeuler" '^[0-9]+\.[0-9]+-lts-sp[0-9]+$' "openeuler/openeuler")
openeuler_digest=$(docker buildx imagetools inspect "openeuler/openeuler:${openeuler_tag}" | grep '^Digest:' | awk '{print $2}')
echo "Latest openeuler is openeuler/openeuler:${openeuler_tag}@${openeuler_digest}"

# Update all Dockerfiles tracked by git
dockerfiles=$(git ls-files "*Dockerfile*")

for file in $dockerfiles; do
    echo "Updating ${file}..."
    
    # Replace alpine pinned images
    # Regex targets alpine:<any-tag>@sha256:<digest>
    sed -i -E "s|alpine:[a-zA-Z0-9.-]+@sha256:[a-f0-9]{64}|alpine:${alpine_tag}@${alpine_digest}|g" "$file"
    
    # Replace openeuler pinned images
    sed -i -E "s|openeuler/openeuler:[a-zA-Z0-9.-]+@sha256:[a-f0-9]{64}|openeuler/openeuler:${openeuler_tag}@${openeuler_digest}|g" "$file"
    
    # Replace OPEN_EULER_IMAGE_TAG argument if it exists
    sed -i -E "s|ARG OPEN_EULER_IMAGE_TAG=[a-zA-Z0-9.-]+@sha256:[a-f0-9]{64}|ARG OPEN_EULER_IMAGE_TAG=${openeuler_tag}@${openeuler_digest}|g" "$file"
done

echo "Done updating base images."
