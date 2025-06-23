#!/bin/bash

# Copyright 2019 The Volcano Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

VK_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
export RELEASE_FOLDER=${VK_ROOT}/${RELEASE_DIR}
export RELEASE_TAG=${TAG:-"latest"}

# Extract image tag from development yaml file
get_development_image_tag() {
	local dev_file="$1"
	grep -o 'volcanosh/[^:]*:[^[:space:]]*' "$dev_file" | head -1 | cut -d':' -f2
}

# Check if tag is a prerelease version (alpha/beta/rc)
is_prerelease_tag() {
	local tag="$1"
	[[ "$tag" =~ (alpha|beta|rc) ]]
}

# Compare yaml files with appropriate tag replacement
compare_yaml_files() {
	local dev_file="$1"
	local generated_file="$2"
	local file_type="$3"
	
	local dev_tag=$(get_development_image_tag "$dev_file")
	
	if [[ "$dev_tag" == "latest" ]] && is_prerelease_tag "$RELEASE_TAG"; then
		# Master branch prerelease scenario: development file uses 'latest', release tag is prerelease
		# Replace 'latest' with release tag in development file for comparison
		echo "Comparing $file_type ($RELEASE_TAG)"
		local temp_file=$(mktemp)
		sed "s|docker\.io/volcanosh/\([^:]*\):latest|docker.io/volcanosh/\1:$RELEASE_TAG|g" "$dev_file" > "$temp_file"
		
		if ! diff "$temp_file" "$generated_file"; then
			echo "ERROR: Generated $file_type differs from development file"
			echo "Please update templates and regenerate development file"
			rm -f "$temp_file"
			return 1
		fi
		rm -f "$temp_file"
		
	elif [[ "$dev_tag" == "$RELEASE_TAG" ]]; then
		# Release branch scenario: development file version matches release tag
		# Used for official releases and patch versions from release-x.xx branches
		echo "Comparing $file_type ($RELEASE_TAG)"
		
		if ! diff "$dev_file" "$generated_file"; then
			echo "ERROR: Generated $file_type differs from development file"
			echo "Run: make generate-yaml TAG=$RELEASE_TAG RELEASE_DIR=installer && mv installer/volcano-$RELEASE_TAG.yaml installer/volcano-development.yaml"
			return 1
		fi
	else
		echo "ERROR: Version mismatch in $file_type"
		echo "Development file: $dev_tag, Release tag: $RELEASE_TAG"
		return 1
	fi
}

# Check volcano main components
if ! compare_yaml_files \
	"${VK_ROOT}/installer/volcano-development.yaml" \
	"${RELEASE_FOLDER}/volcano-${RELEASE_TAG}.yaml" \
	"volcano yaml"; then
	exit 1
fi

# Check volcano agent
if ! compare_yaml_files \
	"${VK_ROOT}/installer/volcano-agent-development.yaml" \
	"${RELEASE_FOLDER}/volcano-agent-${RELEASE_TAG}.yaml" \
	"volcano-agent yaml"; then
	exit 1
fi

echo "All yaml files are consistent"