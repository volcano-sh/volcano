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

if ! diff ${VK_ROOT}/installer/volcano-development.yaml ${RELEASE_FOLDER}/volcano-latest.yaml ; then
	{
		echo
		echo "The Generated yaml is different from the one in installer/volcano-development.yaml"
		echo "please run 'make generate-yaml TAG=latest RELEASE_DIR=installer \
		&& mv ${VK_ROOT}/installer/volcano-latest.yaml ${VK_ROOT}/installer/volcano-development.yaml' to update"
		echo
	} >&2
	false
fi
